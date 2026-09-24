package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Return an upstream validation error after receiving the final body. This tests
// the production request pipeline and log metadata without charging a real user.
func TestReasoningEffortRequestPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	saved := *settings
	t.Cleanup(func() { *settings = saved })
	deleteEffort := func(path string) map[string]any {
		return map[string]any{"operations": []any{map[string]any{"path": path, "mode": "delete"}}}
	}
	for _, tc := range []struct {
		name, model, body, field, want string
		channel                        int
		format                         types.RelayFormat
		override                       map[string]any
		passthrough, viaResponses      bool
		retry                          bool
	}{
		{name: "grok chat", channel: constant.ChannelTypeXai, model: "grok-4.6", body: `{"reasoning_effort":"xhigh"}`, field: "reasoning_effort", want: "xhigh", retry: true},
		{name: "grok search", channel: constant.ChannelTypeXai, model: "grok-4-search", body: `{"reasoning_effort":"high"}`, field: "reasoning_effort", want: "high"},
		{name: "grok suffix", channel: constant.ChannelTypeXai, model: "grok-3-mini-high", body: `{"reasoning_effort":"low"}`, field: "reasoning_effort", want: "high"},
		{name: "openai compatible grok", channel: constant.ChannelTypeOpenAI, model: "grok-4.6", body: `{"reasoning_effort":"low"}`, field: "reasoning_effort", want: "low"},
		{name: "override after suffix", channel: constant.ChannelTypeOpenAI, model: "gpt-5.2-high", body: `{"reasoning_effort":"low"}`, override: map[string]any{"reasoning_effort": "xhigh"}, field: "reasoning_effort", want: "xhigh"},
		{name: "delete adapter effort", channel: constant.ChannelTypeOpenAI, model: "gpt-5", body: `{"reasoning_effort":"low"}`, override: deleteEffort("reasoning_effort"), field: "reasoning_effort"},
		{name: "openrouter conversion", channel: constant.ChannelTypeOpenRouter, model: "openai/gpt-5", body: `{"reasoning_effort":"high"}`, field: "reasoning.effort", want: "high"},
		{name: "grok responses", channel: constant.ChannelTypeXai, model: "grok-4.6", format: types.RelayFormatOpenAIResponses, body: `{"reasoning":{"effort":"xhigh"}}`, field: "reasoning.effort", want: "xhigh"},
		{name: "responses override", channel: constant.ChannelTypeOpenAI, model: "gpt-5.2", format: types.RelayFormatOpenAIResponses, body: `{"reasoning":{"effort":"low"}}`, override: map[string]any{"reasoning": map[string]any{"effort": "xhigh"}}, field: "reasoning.effort", want: "xhigh"},
		{name: "responses delete", channel: constant.ChannelTypeOpenAI, model: "gpt-5", format: types.RelayFormatOpenAIResponses, body: `{"reasoning":{"effort":"low"}}`, override: deleteEffort("reasoning.effort"), field: "reasoning.effort"},
		{name: "chat passthrough", channel: constant.ChannelTypeXai, model: "grok-4.6", body: `{"reasoning_effort":"none","unknown":false}`, passthrough: true, override: map[string]any{"reasoning_effort": "high"}, field: "reasoning_effort", want: "none"},
		{name: "responses passthrough", channel: constant.ChannelTypeXai, model: "grok-4.6", format: types.RelayFormatOpenAIResponses, body: `{"reasoning":{"effort":"custom-level"},"unknown":false}`, passthrough: true, field: "reasoning.effort", want: "custom-level"},
		{name: "chat to responses", channel: constant.ChannelTypeOpenAI, model: "gpt-5.2", body: `{"reasoning_effort":"low"}`, viaResponses: true, override: map[string]any{"reasoning_effort": "xhigh"}, field: "reasoning.effort", want: "xhigh"},
		{name: "claude", channel: constant.ChannelTypeAnthropic, model: "claude-opus-4-6", format: types.RelayFormatClaude, body: `{"max_tokens":64,"output_config":{"effort":"max"}}`, field: "output_config.effort", want: "max"},
		{name: "claude passthrough", channel: constant.ChannelTypeAnthropic, model: "claude-opus-4-6", format: types.RelayFormatClaude, body: `{"max_tokens":64,"output_config":{"effort":"low"}}`, passthrough: true, field: "output_config.effort", want: "low"},
		{name: "gemini", channel: constant.ChannelTypeGemini, model: "gemini-3-pro-preview", format: types.RelayFormatGemini, body: `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high"}}}`, field: "generationConfig.thinkingConfig.thinkingLevel", want: "high"},
		{name: "gemini snake passthrough", channel: constant.ChannelTypeGemini, model: "gemini-3-pro-preview", format: types.RelayFormatGemini, body: `{"generationConfig":{"thinking_config":{"thinking_level":"low"}}}`, passthrough: true, field: "generationConfig.thinking_config.thinking_level", want: "low"},
	} {
		for _, stream := range []bool{false, true} {
			name := tc.name + "/nonstream"
			if stream {
				name = tc.name + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				settings.PassThroughRequestEnabled = false
				settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: tc.viaResponses, AllChannels: true, ModelPatterns: []string{".*"}}
				format := tc.format
				if format == "" {
					format = types.RelayFormatOpenAI
				}
				var payload map[string]any
				require.NoError(t, common.UnmarshalJsonStr(tc.body, &payload))
				payload["model"], payload["stream"] = tc.model, stream
				path := "/v1/chat/completions"
				switch format {
				case types.RelayFormatOpenAIResponses:
					path, payload["input"] = "/v1/responses", "hi"
				case types.RelayFormatGemini:
					path = "/v1beta/models/" + tc.model + ":generateContent"
					payload["contents"] = []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}}}
				default:
					payload["messages"] = []any{map[string]any{"role": "user", "content": "hi"}}
					if format == types.RelayFormatClaude {
						path = "/v1/messages"
					}
				}
				body, err := common.Marshal(payload)
				require.NoError(t, err)
				captured := make(chan []byte, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					data, _ := io.ReadAll(r.Body)
					captured <- data
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"mock validation"}}`)
				}))
				defer server.Close()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Params = gin.Params{{Key: "model", Value: tc.model + ":generateContent"}}
				common.SetContextKey(c, constant.ContextKeyChannelType, tc.channel)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
				common.SetContextKey(c, constant.ContextKeyChannelKey, "mock-key")
				common.SetContextKey(c, constant.ContextKeyOriginalModel, tc.model)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
				common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{RemoveGifImagesEnabled: common.GetPointer(false)})
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, tc.override)
				request, err := helper.GetAndValidateRequest(c, format)
				require.NoError(t, err)
				info := &relaycommon.RelayInfo{Request: request, OriginModelName: tc.model, RelayFormat: format, RelayMode: relayconstant.Path2RelayMode(path), RequestURLPath: path, StartTime: time.Now(), DisablePing: true, IsStream: stream, ReasoningEffort: "stale"}
				run := func() *types.NewAPIError {
					switch format {
					case types.RelayFormatClaude:
						return ClaudeHelper(c, info)
					case types.RelayFormatGemini:
						return GeminiHelper(c, info)
					case types.RelayFormatOpenAIResponses:
						return ResponsesHelper(c, info)
					default:
						return TextHelper(c, info)
					}
				}
				apiErr := run()
				require.NotNil(t, apiErr)
				require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				select {
				case upstream := <-captured:
					require.Equal(t, tc.want, gjson.GetBytes(upstream, tc.field).String())
					if tc.passthrough {
						require.Equal(t, string(body), string(upstream))
					}
				default:
					t.Fatalf("request did not reach upstream: %v", apiErr)
				}
				require.Equal(t, tc.want, info.ReasoningEffort)
				other := service.GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 0, 0, 0)
				if tc.want == "" {
					require.NotContains(t, other, "reasoning_effort")
				} else {
					require.Equal(t, tc.want, other["reasoning_effort"])
				}
				if tc.retry {
					common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
					common.SetContextKey(c, constant.ContextKeyChannelParamOverride, deleteEffort("reasoning_effort"))
					require.NotNil(t, run())
					require.Empty(t, info.ReasoningEffort)
					select {
					case upstream := <-captured:
						require.False(t, gjson.GetBytes(upstream, "reasoning_effort").Exists())
					default:
						t.Fatal("retry did not reach upstream")
					}
				}
			})
		}
	}
}
