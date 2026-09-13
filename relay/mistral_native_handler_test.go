package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestMistralNativeGatewayPipeline(t *testing.T) {
	db := setupMistralPipeline(t)
	received := make(chan map[string]any, 4)
	response := `{"pages":[{"markdown":"document","custom":false}],"usage_info":{"pages_processed":2,"doc_size_bytes":120},"unknown":0}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = common.DecodeJson(r.Body, &body)
		received <- body
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/ocr" {
			_, _ = io.WriteString(w, response)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8},"native_extra":false}`)
	}))
	defer upstream.Close()
	for _, tc := range []struct{ path, body, model, field string }{
		{"/v1/ocr", `{"model":"test-alias","document":{"type":"document_url","document_url":"https://example.com/test.pdf"},"include_image_base64":false}`, "mistral-ocr-latest", "model"},
		{"/v1/fim/completions", `{"model":"test-alias","prompt":"def f():","suffix":"return 1","min_tokens":0}`, "codestral-latest", "model"},
		{"/v1/agents/completions", `{"agent_id":"test-alias","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search"}],"parallel_tool_calls":false}`, "ag:example", "agent_id"},
	} {
		w, _, apiErr := mistralGatewayRequest(t, tc.path, "application/json", []byte(tc.body), upstream.URL+"/v1/", "test-key", tc.model, dto.ChannelSettings{ForceFormat: true, SystemPrompt: "must not be injected"}, nil)
		require.Nil(t, apiErr)
		captured := <-received
		require.Equal(t, tc.model, captured[tc.field])
		if tc.field == "agent_id" {
			require.NotContains(t, captured, "model")
			require.Equal(t, false, captured["parallel_tool_calls"])
		}
		if tc.path == "/v1/ocr" {
			require.Equal(t, response, w.Body.String())
		} else {
			require.Contains(t, w.Body.String(), `"native_extra":false`)
		}
	}
	var logs []model.Log
	require.NoError(t, db.Order("id").Find(&logs).Error)
	require.Len(t, logs, 3)
	require.Equal(t, 0, logs[0].PromptTokens)
	require.Equal(t, 0, logs[0].CompletionTokens)
	require.Equal(t, int(0.001*common.QuotaPerUnit), logs[0].Quota, "successful OCR must honor per-call price without fake tokens")
	require.Contains(t, logs[0].Content, "pages_processed=2")
}
