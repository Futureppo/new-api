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
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
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

func TestOpenAICompatibilityRequestPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	saved := *settings
	t.Cleanup(func() { *settings = saved })
	tools := `,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]`
	appendOnce := map[string]any{"operations": []any{map[string]any{"path": "metadata.note", "mode": "append", "value": "-once"}}}
	for _, tc := range []struct {
		name, model, fields, mapping, wantPath, errorText, messages string
		responses, passthrough, globalPassthrough                   bool
		channel                                                     int
		override                                                    map[string]any
		want                                                        map[string]string
		absent                                                      []string
	}{
		{name: "astra token and sampling", model: "gpt-6-astra", fields: `,"max_tokens":64,"max_completion_tokens":0,"temperature":0,"top_p":0,"top_logprobs":0`, want: map[string]string{"max_completion_tokens": "0"}, absent: []string{"max_tokens", "temperature", "top_p", "top_logprobs"}},
		{name: "astra tools", model: "gpt-6-astra-max", fields: tools + `,"max_tokens":64,"metadata":{"note":"start"}`, override: appendOnce, wantPath: "/v1/responses", want: map[string]string{"model": `"gpt-6-astra"`, "reasoning.effort": `"max"`, "max_output_tokens": "64", "metadata.note": `"start-once"`}, absent: []string{"max_tokens", "max_completion_tokens", "messages"}},
		{name: "sol default tools", model: "gpt-6-sol", fields: tools, wantPath: "/v1/responses"},
		{name: "luna none tools", model: "gpt-6-luna", fields: tools + `,"reasoning_effort":"none","temperature":0,"top_p":0,"logprobs":false,"top_logprobs":0`, want: map[string]string{"temperature": "0", "top_p": "0", "logprobs": "false", "top_logprobs": "0"}},
		{name: "mapped alias", model: "my-model", mapping: `{"my-model":"gpt-6-astra-max"}`, fields: tools, wantPath: "/v1/responses", want: map[string]string{"model": `"gpt-6-astra"`, "reasoning.effort": `"max"`}},
		{name: "real codex model", model: "gpt-5.1-codex-max", wantPath: "/v1/responses", want: map[string]string{"model": `"gpt-5.1-codex-max"`}, absent: []string{"reasoning.effort"}},
		{name: "override restores sampling", model: "gpt-5.2-high", fields: `,"temperature":0,"top_p":0`, override: map[string]any{"reasoning_effort": "none"}, want: map[string]string{"reasoning_effort": `"none"`, "temperature": "0", "top_p": "0"}},
		{name: "override selects responses", model: "gpt-5.2", fields: tools, override: map[string]any{"model": "gpt-6-astra"}, wantPath: "/v1/responses", want: map[string]string{"model": `"gpt-6-astra"`}},
		{name: "override removes restriction", model: "gpt-6-astra", fields: `,"max_tokens":64,"temperature":0`, override: map[string]any{"model": "gpt-4o"}, want: map[string]string{"model": `"gpt-4o"`, "max_tokens": "64", "temperature": "0"}},
		{name: "override fixes effort", model: "gpt-6-astra-none", override: map[string]any{"reasoning_effort": "low"}, want: map[string]string{"reasoning_effort": `"low"`}},
		{name: "invalid none", model: "gpt-6-astra", fields: `,"reasoning_effort":"none"`, errorText: "reasoning_effort"},
		{name: "invalid minimal", model: "gpt-6-sol-minimal", errorText: "supported values"},
		{name: "invalid override", model: "gpt-6-astra", override: map[string]any{"reasoning_effort": "none"}, errorText: "reasoning_effort"},
		{name: "conversion rejects n", model: "gpt-6-astra", fields: tools + `,"n":2`, errorText: "n>1"},
		{name: "conversion rejects legacy functions", model: "gpt-6-astra", fields: `,"functions":[{"name":"lookup"}]`, errorText: "legacy functions"},
		{name: "conversion rejects malformed tool calls", model: "gpt-6-astra", messages: `[{"role":"assistant","tool_calls":{"id":"call_1"}}]`, errorText: "messages[0].tool_calls"},
		{name: "conversion rejects misplaced tool calls", model: "gpt-6-astra", messages: `[{"role":"user","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]`, errorText: "requires role=assistant"},
		{name: "conversion rejects tool output without id", model: "gpt-6-astra", messages: `[{"role":"tool","content":"result"}]`, errorText: "tool_call_id is required"},
		{name: "conversion rejects unknown tool output content", model: "gpt-6-astra", messages: `[{"role":"tool","tool_call_id":"call_1","content":[{"type":"unknown","data":"result"}]}]`, errorText: "messages[0].content"},
		{name: "conversion rejects malformed tool output content", model: "gpt-6-astra", messages: `[{"role":"tool","tool_call_id":"call_1","content":{"result":"found"}}]`, errorText: "messages[0].content"},
		{name: "conversion rejects legacy tool output", model: "gpt-6-astra", messages: `[{"role":"function","tool_call_id":"call_1","content":"result"}]`, errorText: "role=function"},
		{name: "conversion rejects null custom definition", model: "gpt-6-astra", fields: `,"tools":[{"type":"custom","custom":null}]`, errorText: "tools[0].custom"},
		{name: "conversion rejects null custom call", model: "gpt-6-astra", messages: `[{"role":"assistant","tool_calls":[{"id":"call_1","type":"custom","custom":null}]}]`, errorText: "tool_calls[0].custom"},
		{name: "conversion rejects unknown tool definition", model: "gpt-6-astra", fields: `,"tools":[{"type":"unknown","unknown":{"name":"lookup"}}]`, errorText: "tools[0].type"},
		{name: "conversion rejects unsupported tool selection", model: "gpt-6-astra", fields: tools + `,"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":"auto","tools":[{"type":"function","function":{"name":"lookup"}}]}}`, errorText: "tool_choice"},
		{name: "native responses", model: "gpt-6-astra", responses: true, fields: `,"max_output_tokens":0,"temperature":0,"top_p":0,"top_logprobs":0,"include":["message.output_text.logprobs","reasoning.encrypted_content"]`, want: map[string]string{"max_output_tokens": "0", "include": `["reasoning.encrypted_content"]`}, absent: []string{"temperature", "top_p", "top_logprobs"}},
		{name: "responses override restores sampling", model: "gpt-5.2-high", responses: true, fields: `,"temperature":0,"top_p":0`, override: map[string]any{"reasoning": map[string]any{"effort": "none"}}, want: map[string]string{"temperature": "0", "top_p": "0", "reasoning.effort": `"none"`}},
		{name: "responses invalid override", model: "gpt-6-astra", responses: true, override: map[string]any{"reasoning": map[string]any{"effort": "none"}}, errorText: "reasoning.effort"},
		{name: "unknown o name", model: "orcarouter/model", fields: `,"max_tokens":0,"temperature":0`, want: map[string]string{"max_tokens": "0", "temperature": "0"}},
		{name: "unknown gpt name", model: "gpt-5-unknown-high", fields: `,"max_tokens":0,"temperature":0`, want: map[string]string{"model": `"gpt-5-unknown-high"`, "max_tokens": "0", "temperature": "0"}},
		{name: "channel passthrough", model: "gpt-6-astra-none", fields: tools + `,"max_tokens":0,"unknown":false`, passthrough: true, override: appendOnce},
		{name: "global passthrough", model: "gpt-6-astra-none", fields: tools + `,"max_tokens":0`, globalPassthrough: true},
		{name: "responses passthrough", model: "gpt-6-astra", responses: true, fields: `,"reasoning":{"effort":"none"},"temperature":0`, passthrough: true},
		{name: "third party unchanged", model: "gpt-6-astra", channel: constant.ChannelTypeCustom, fields: tools + `,"max_tokens":0,"temperature":0`, want: map[string]string{"max_tokens": "0", "temperature": "0"}},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/json", true: "/sse"}[stream], func(t *testing.T) {
				settings.PassThroughRequestEnabled = tc.globalPassthrough
				settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
				path, format, input := "/v1/chat/completions", types.RelayFormatOpenAI, `,"messages":[{"role":"system","content":"system"},{"role":"user","content":"hi"}]`
				if tc.messages != "" {
					input = `,"messages":` + tc.messages
				}
				if tc.responses {
					path, format, input = "/v1/responses", types.RelayFormatOpenAIResponses, `,"input":"hi"`
				}
				body := `{"model":"` + tc.model + `","stream":` + map[bool]string{false: "false", true: "true"}[stream] + input + tc.fields + `}`
				type capturedRequest struct {
					path string
					body []byte
				}
				captured := make(chan capturedRequest, 2)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					captured <- capturedRequest{r.URL.Path, b}
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"mock upstream"}}`)
				}))
				defer server.Close()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				channel := tc.channel
				if channel == 0 {
					channel = constant.ChannelTypeOpenAI
				}
				common.SetContextKey(c, constant.ContextKeyChannelType, channel)
				baseURL := server.URL
				if channel == constant.ChannelTypeCustom {
					baseURL += "/v1/chat/completions"
				}
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
				common.SetContextKey(c, constant.ContextKeyChannelKey, "mock-key")
				common.SetContextKey(c, constant.ContextKeyOriginalModel, tc.model)
				common.SetContextKey(c, constant.ContextKeyChannelModelMapping, tc.mapping)
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
				common.SetContextKey(c, constant.ContextKeyChannelParamOverride, tc.override)
				request, err := helper.GetAndValidateRequest(c, format)
				require.NoError(t, err)
				info := &relaycommon.RelayInfo{Request: request, OriginModelName: tc.model, RelayFormat: format, RelayMode: relayconstant.Path2RelayMode(path), RequestURLPath: path, StartTime: time.Now(), DisablePing: true, IsStream: stream}
				var apiErr *types.NewAPIError
				if tc.responses {
					apiErr = ResponsesHelper(c, info)
				} else {
					apiErr = TextHelper(c, info)
				}
				require.NotNil(t, apiErr)
				require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				if tc.errorText != "" {
					require.Contains(t, apiErr.Error(), tc.errorText)
					require.True(t, types.IsSkipRetryError(apiErr))
					require.Empty(t, captured)
					return
				}
				select {
				case got := <-captured:
					wantPath := tc.wantPath
					if wantPath == "" {
						wantPath = path
					}
					require.Equal(t, wantPath, got.path)
					if tc.passthrough || tc.globalPassthrough {
						require.Equal(t, body, string(got.body))
					}
					for key, value := range tc.want {
						require.JSONEq(t, value, gjson.GetBytes(got.body, key).Raw, key)
					}
					for _, key := range tc.absent {
						require.False(t, gjson.GetBytes(got.body, key).Exists(), key)
					}
					require.Equal(t, tc.model, info.OriginModelName)
				default:
					t.Fatalf("no upstream request: %v", apiErr)
				}
			})
		}
	}
}

func TestOpenAIResponsesToolResultsAndUsage(t *testing.T) {
	service.InitHttpClient()
	savedTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = savedTimeout })
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			response := `{"id":"resp_1","model":"gpt-6-astra","output":[{"id":"item_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12,"output_tokens_details":{"reasoning_tokens":1}}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/responses", r.URL.Path)
				b, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "function_call_output", gjson.GetBytes(b, "input.1.type").String())
				require.Equal(t, "old_call", gjson.GetBytes(b, "input.1.call_id").String())
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: "+`{"type":"response.output_item.added","output_index":0,"item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":""}}`+"\n\n")
					_, _ = io.WriteString(w, "data: "+`{"type":"response.function_call_arguments.delta","item_id":"item_1","output_index":0,"delta":"{}"}`+"\n\n")
					_, _ = io.WriteString(w, "data: "+`{"type":"response.completed","response":`+response+`}`+"\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, response)
				}
			}))
			defer server.Close()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelBaseUrl: server.URL, UpstreamModelName: "gpt-6-astra", ApiKey: "mock-key"}, OriginModelName: "gpt-6-astra", RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, RequestURLPath: "/v1/chat/completions", IsStream: stream, ShouldIncludeUsage: true, DisablePing: true, StartTime: time.Now()}
			adaptor := &openaichannel.Adaptor{}
			adaptor.Init(info)
			body := []byte(`{"model":"gpt-6-astra","stream":` + map[bool]string{false: "false", true: "true"}[stream] + `,"messages":[{"role":"assistant","tool_calls":[{"id":"old_call","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"old_call","content":"found"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)
			usage, apiErr := chatCompletionsViaResponsesBody(c, info, adaptor, body)
			require.Nil(t, apiErr)
			require.Equal(t, 12, usage.TotalTokens)
			require.Equal(t, 1, usage.CompletionTokenDetails.ReasoningTokens)
			require.Contains(t, w.Body.String(), `"tool_calls"`)
			require.Contains(t, w.Body.String(), `"call_1"`)
			require.Contains(t, w.Body.String(), `"lookup"`)
			require.Contains(t, w.Body.String(), `"total_tokens":12`)
			if stream {
				require.Contains(t, w.Body.String(), "[DONE]")
			}
			require.Equal(t, "/v1/chat/completions", info.RequestURLPath)
			require.Equal(t, relayconstant.RelayModeChatCompletions, info.RelayMode)
		})
	}
}

func TestOpenAICompatibilityConvertedFormats(t *testing.T) {
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	saved := *settings
	t.Cleanup(func() { *settings = saved })
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	for _, tc := range []struct {
		format     types.RelayFormat
		path, body string
	}{
		{types.RelayFormatClaude, "/v1/messages", `{"model":"gpt-6-astra","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object","properties":{}}}]}`},
		{types.RelayFormatGemini, "/v1beta/models/gpt-6-astra:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hi"}]}],"generationConfig":{"maxOutputTokens":64},"tools":[{"functionDeclarations":[{"name":"lookup","parameters":{"type":"object","properties":{}}}]}]}`},
	} {
		t.Run(string(tc.format), func(t *testing.T) {
			captured := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/responses", r.URL.Path)
				body, _ := io.ReadAll(r.Body)
				captured <- body
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"mock upstream"}}`)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "model", Value: "gpt-6-astra:generateContent"}}
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "mock-key")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-6-astra")
			request, err := helper.GetAndValidateRequest(c, tc.format)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: request, OriginModelName: "gpt-6-astra", RelayFormat: tc.format, RelayMode: relayconstant.Path2RelayMode(tc.path), RequestURLPath: tc.path, StartTime: time.Now(), DisablePing: true}
			var apiErr *types.NewAPIError
			if tc.format == types.RelayFormatClaude {
				apiErr = ClaudeHelper(c, info)
			} else {
				apiErr = GeminiHelper(c, info)
			}
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			select {
			case body := <-captured:
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(body, "model").String())
				require.Equal(t, int64(64), gjson.GetBytes(body, "max_output_tokens").Int())
				require.Equal(t, "lookup", gjson.GetBytes(body, "tools.0.name").String())
			default:
				t.Fatalf("request did not reach upstream: %v", apiErr)
			}
		})
	}
}

func TestOpenAICompatibilitySystemPrompt(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, ChannelSetting: dto.ChannelSettings{SystemPrompt: "channel", SystemPromptOverride: true}}}
	request := &dto.GeneralOpenAIRequest{Model: "gpt-6-astra", Messages: []dto.Message{{Role: "system", Content: "original"}, {Role: "user", Content: "hi"}}}
	applySystemPromptIfNeeded(c, info, request)
	require.Len(t, request.Messages, 2)
	require.Equal(t, "channel\noriginal", request.Messages[0].Content)
	body, err := common.Marshal(request)
	require.NoError(t, err)
	body, _, err = normalizeOfficialChatRequest(info, body)
	require.NoError(t, err)
	require.Equal(t, "developer", gjson.GetBytes(body, "messages.0.role").String())

	request = &dto.GeneralOpenAIRequest{Model: "gpt-6-astra", Messages: []dto.Message{{Role: "user", Content: "hi"}}}
	applySystemPromptIfNeeded(c, info, request)
	// A later administrator model override must determine the injected role.
	request.Model = "custom-model"
	body, err = common.Marshal(request)
	require.NoError(t, err)
	body, _, err = normalizeOfficialChatRequest(info, body)
	require.NoError(t, err)
	require.Equal(t, "system", gjson.GetBytes(body, "messages.0.role").String())
}
