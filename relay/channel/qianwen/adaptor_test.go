package qianwen

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenURLs(t *testing.T) {
	a := &Adaptor{}
	for _, base := range []string{"https://maas.qianwenaiapi.com", "https://proxy.example/prefix"} {
		for _, suffix := range []string{"", "/compatible-mode/v1", "/apps/anthropic", "/apps/anthropic/v1", "/compatible-mode/v1/models", "/compatible-mode/v1/chat/completions", "/compatible-mode/v1/responses", "/apps/anthropic/v1/messages"} {
			for _, tc := range []struct {
				format types.RelayFormat
				mode   int
				path   string
			}{
				{types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, "/compatible-mode/v1/chat/completions"},
				{types.RelayFormatClaude, relayconstant.RelayModeUnknown, "/apps/anthropic/v1/messages"},
				{types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, "/compatible-mode/v1/responses"},
			} {
				info := &relaycommon.RelayInfo{RelayFormat: tc.format, RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: " " + base + suffix + "/ "}}
				got, err := a.GetRequestURL(info)
				require.NoError(t, err)
				require.Equal(t, base+tc.path, got)
			}
		}
	}
	require.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeQianwenAI], NormalizeBaseURL(""))
	for _, base := range []string{"file:///tmp/qwen", "relative/path", "https://host?url=x", "https://host#fragment", "https://user:password@host"} {
		require.Error(t, ValidateBaseURL(base))
	}
	for _, mode := range []int{relayconstant.RelayModeCompletions, relayconstant.RelayModeResponsesCompact, relayconstant.RelayModeAudioTranscription} {
		_, err := a.GetRequestURL(&relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: mode})
		require.ErrorContains(t, err, "does not support")
	}
}

func TestQianwenNativeRequestRoundTrip(t *testing.T) {
	a := &Adaptor{}
	for _, body := range []string{
		`{"model":"qwen3.7-plus","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"glm-5.3","stream":false,"temperature":0,"top_p":0,"max_tokens":0,"max_completion_tokens":0,"enable_thinking":false,"preserve_thinking":false,"thinking_budget":0,"tool_stream":false,"search_options":{"forced_search":false},"parallel_tool_calls":false,"messages":[{"role":"assistant","content":"","reasoning_content":"keep this","tool_calls":[{"id":"c1","type":"function","function":{"name":"echo","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","content":"OK"}],"tools":[{"type":"function","function":{"name":"echo","parameters":{"type":"object"}}}],"response_format":{"type":"json_object"}}`,
	} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(body, &req))
		got, err := a.ConvertOpenAIRequest(nil, nil, &req)
		require.NoError(t, err)
		data, err := common.Marshal(got)
		require.NoError(t, err)
		require.JSONEq(t, body, string(data))
	}
	body := `{"model":"kimi-k3","max_tokens":0,"temperature":0,"stream":false,"thinking":{"type":"disabled"},"output_config":{"effort":"low","format":{"type":"json_schema","schema":{"type":"object"}}},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"keep","signature":""},{"type":"tool_use","id":"c1","name":"echo","input":{"value":0}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"OK","is_error":false}]}]}`
	var claude dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &claude))
	got, err := a.ConvertClaudeRequest(nil, nil, &claude)
	require.NoError(t, err)
	data, err := common.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, body, string(data))
	body = `{"model":"qwen3.8-max","input":[{"type":"function_call_output","call_id":"c1","output":"OK"}],"previous_response_id":"resp1","store":false,"stream":false,"background":false,"temperature":0,"max_output_tokens":0,"reasoning":{"effort":"low"},"parallel_tool_calls":false}`
	var responses dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &responses))
	got, err = a.ConvertOpenAIResponsesRequest(nil, nil, responses)
	require.NoError(t, err)
	data, err = common.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, body, string(data))
}

func TestQianwenRejectsBackgroundAfterOverrides(t *testing.T) {
	a := &Adaptor{}
	for _, body := range []string{`{"background":true}`, `{"background":"true"}`} {
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{}}
		_, err := a.DoRequest(nil, info, strings.NewReader(body))
		require.Error(t, err)
		var apiErr *types.NewAPIError
		require.ErrorAs(t, err, &apiErr)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	}
}

func TestQianwenAuthHeaders(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "client-key")
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		h := http.Header{}
		info := &relaycommon.RelayInfo{RelayFormat: format, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "upstream-key"}}
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &h, info))
		if format == types.RelayFormatClaude {
			require.Equal(t, "upstream-key", h.Get("x-api-key"))
			require.Empty(t, h.Get("Authorization"))
			require.Equal(t, "2023-06-01", h.Get("anthropic-version"))
		} else {
			require.Equal(t, "Bearer upstream-key", h.Get("Authorization"))
			require.Empty(t, h.Get("x-api-key"))
		}
	}
}
