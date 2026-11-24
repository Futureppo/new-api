package gemini

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiDecisionsURL(t *testing.T) {
	a := &Adaptor{}
	for _, suffix := range []string{"", "/", "/v1beta", "/v1beta/"} {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.test/proxy" + suffix}, RelayMode: constant.RelayModeDecisions, Request: &dto.GeminiDecisionsRequest{Model: "gemini-decision-1"}}
		url, err := a.GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, "https://example.test/proxy/v1beta/decisions", url)
		info.IsStream = true
		_, err = a.GetRequestURL(info)
		require.ErrorContains(t, err, "synchronous")
		info.IsStream = false
		info.Request = &dto.OpenAIDecisionsRequest{}
		_, err = a.GetRequestURL(info)
		require.ErrorContains(t, err, "named questions")
	}
}

func TestGeminiDecisionsResponseValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body, contentType string
		wantTokens              int
		bad                     bool
	}{
		{"success", `{"answers":{"q":{"type":"noul","noul":0,"confidence":1}},"usage":{"total_input_tokens":42,"total_output_tokens":0},"future":false}`, "application/json", 42, false},
		{"blocked", `{"answers":{},"prompt_feedback":{"block_reason":"SAFETY","future":false},"usage":{"total_input_tokens":42,"total_output_tokens":0,"total_tokens":42}}`, "application/json", 42, false},
		{"zero", `{"answers":{"q":{}},"usage":{"total_input_tokens":0,"total_output_tokens":0,"total_tokens":0}}`, "application/json", 0, false},
		{"blocked without usage", `{"prompt_feedback":{"block_reason":"SAFETY"}}`, "application/json", 0, false},
		{"empty answers", `{"answers":{},"usage":{"total_input_tokens":42,"total_output_tokens":0}}`, "application/json", 0, true},
		{"missing input", `{"answers":{"q":{}},"usage":{"total_output_tokens":0}}`, "application/json", 0, true},
		{"missing output", `{"answers":{"q":{}},"usage":{"total_input_tokens":42}}`, "application/json", 0, true},
		{"fractional input", `{"answers":{"q":{}},"usage":{"total_input_tokens":1.5,"total_output_tokens":0}}`, "application/json", 0, true},
		{"negative input", `{"answers":{"q":{}},"usage":{"total_input_tokens":-1,"total_output_tokens":0}}`, "application/json", 0, true},
		{"string input", `{"answers":{"q":{}},"usage":{"total_input_tokens":"42","total_output_tokens":0}}`, "application/json", 0, true},
		{"overflow input", `{"answers":{"q":{}},"usage":{"total_input_tokens":9999999999999999999999,"total_output_tokens":0}}`, "application/json", 0, true},
		{"nonzero output", `{"answers":{"q":{}},"usage":{"total_input_tokens":42,"total_output_tokens":1}}`, "application/json", 0, true},
		{"wrong total", `{"answers":{"q":{}},"usage":{"total_input_tokens":42,"total_output_tokens":0,"total_tokens":43}}`, "application/json", 0, true},
		{"error payload", `{"error":{"message":"busy"}}`, "application/json", 0, true},
		{"malformed", `<html>`, "text/html", 0, true},
		{"SSE", `data: {}`, "text/event-stream", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/decisions", nil)
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			usage, err := GeminiDecisionsHandler(c, resp, &relaycommon.RelayInfo{})
			if tc.bad {
				require.NotNil(t, err)
				require.Equal(t, 502, err.StatusCode)
				require.Empty(t, w.Body.String())
				return
			}
			require.Nil(t, err)
			require.Equal(t, tc.body, w.Body.String())
			require.Equal(t, tc.wantTokens, usage.PromptTokens)
			require.Equal(t, tc.wantTokens, usage.TotalTokens)
			require.Zero(t, usage.CompletionTokens)
			require.Equal(t, "upstream", usage.UsageSource)
		})
	}
}
