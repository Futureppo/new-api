package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestMistralModerationGateway(t *testing.T) {
	db := setupMistralPipeline(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		var request map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &request))
		require.Equal(t, "mistral-moderation-latest", request["model"])
		require.Equal(t, []any{"safe", "threat"}, request["input"])
		require.Equal(t, false, request["metadata"].(map[string]any)["enabled"])
		require.NotContains(t, request, "messages")
		require.NotContains(t, request, "temperature")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"mod-test","model":"mistral-moderation-latest","results":[{"categories":{"violence_and_threats":false},"category_scores":{"violence_and_threats":0}},{"categories":{"violence_and_threats":true},"category_scores":{"violence_and_threats":0.99}}],"usage":{"prompt_tokens":81,"completion_tokens":0,"total_tokens":142}}`)
	}))
	defer upstream.Close()
	w, _, apiErr := mistralGatewayRequest(t, "/v1/moderations", "application/json", []byte(`{"model":"test-alias","input":"original","stream":false,"temperature":0}`), upstream.URL+"/v1/", "test-key", "mistral-moderation-latest", dto.ChannelSettings{SystemPrompt: "must not be sent", ForceFormat: true}, map[string]any{"input": []string{"safe", "threat"}, "metadata": map[string]any{"enabled": false}})
	require.Nil(t, apiErr)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"flagged":true`)
	require.Contains(t, w.Body.String(), `"flagged":false`)
	require.NotContains(t, w.Body.String(), "choices")
	var logs []model.Log
	require.NoError(t, db.Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, 81, logs[0].PromptTokens)
	require.Zero(t, logs[0].CompletionTokens)
	require.Positive(t, logs[0].Quota)
}

func TestMistralModerationLiveGateway(t *testing.T) {
	if os.Getenv("MISTRAL_LIVE_TEST") != "1" {
		t.Skip("set MISTRAL_LIVE_TEST=1 and MISTRAL_API_KEY to run")
	}
	key := os.Getenv("MISTRAL_API_KEY")
	require.NotEmpty(t, key)
	setupMistralPipeline(t)
	for _, tc := range []struct {
		name  string
		input any
		flags []bool
	}{
		{"safe", "Have a nice day.", []bool{false}},
		{"threat", "I will kill you.", []bool{true}},
		{"batch", []string{"Have a nice day.", "I will kill you."}, []bool{false, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"model": "test-alias", "input": tc.input})
			require.NoError(t, err)
			w, _, apiErr := mistralGatewayRequest(t, "/v1/moderations", "application/json", body, "https://api.mistral.ai/v1/", key, "mistral-moderation-latest", dto.ChannelSettings{ForceFormat: true}, nil)
			require.Nil(t, apiErr)
			var result struct {
				ID      string `json:"id"`
				Results []struct {
					Flagged    bool            `json:"flagged"`
					Categories map[string]bool `json:"categories"`
				} `json:"results"`
				Usage *dto.Usage `json:"usage"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
			require.NotEmpty(t, result.ID)
			require.Len(t, result.Results, len(tc.flags))
			for i, expected := range tc.flags {
				require.Equal(t, expected, result.Results[i].Flagged)
				require.NotEmpty(t, result.Results[i].Categories)
			}
			if result.Usage != nil {
				require.Positive(t, result.Usage.PromptTokens)
			}
		})
	}
}
