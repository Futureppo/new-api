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

func TestSelfHostedRegistrationAndPipeline(t *testing.T) {
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	saved := *settings
	t.Cleanup(func() { *settings = saved })
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	for requestedEffort, wantEffort := range map[string]string{"minimal": "low", "high": "medium", "max": "xhigh"} {
		for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeLiteLLM} {
			apiType, ok := common.ChannelType2APIType(channelType)
			require.True(t, ok)
			require.NotNil(t, GetAdaptor(apiType))
			require.Empty(t, GetAdaptor(apiType).GetModelList())
			require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}, common.GetEndpointTypesByChannelType(channelType, "alias"))
			for _, responses := range []bool{false, true} {
				for _, passthrough := range []bool{false, true} {
					path, format := "/v1/chat/completions", types.RelayFormatOpenAI
					fields := `"messages":[{"role":"assistant","content":null,"reasoning_content":"reason","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"},{"role":"system","content":"late policy"},{"role":"developer","content":"developer policy"}],"reasoning_effort":"low","temperature":0,"parallel_tool_calls":false,"stream_options":{"include_usage":false},"tools":[{"type":"function","function":{"name":"lookup","strict":false,"parameters":{"type":"object"}}}],"allowed_openai_params":["tools"]`
					if responses {
						path, format = "/v1/responses", types.RelayFormatOpenAIResponses
						fields = `"input":[{"type":"function_call_output","call_id":"call_1","output":"ok"},{"role":"system","content":"late policy"}],"instructions":"request policy","reasoning":{"effort":"low","summary":"auto"},"store":false`
					}
					model := "alias"
					if passthrough {
						model = "Qwen3.8-Flash-Next-FP8"
						fields = strings.ReplaceAll(fields, `"low"`, `"`+requestedEffort+`"`)
					}
					body := `{"model":"` + model + `","stream":true,` + fields + `}`
					captured := make(chan []byte, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/prefix"+path, r.URL.Path)
						require.Empty(t, r.Header.Get("Authorization"))
						raw, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						captured <- raw
						w.WriteHeader(400)
						_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"capture only"}}`)
					}))
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL+"/prefix/v1")
					common.SetContextKey(c, constant.ContextKeyOriginalModel, model)
					common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"alias":"qwen3.8-27b-fp8"}`)
					common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: passthrough})
					override := map[string]any{"reasoning_effort": requestedEffort}
					if responses {
						override = map[string]any{"reasoning": map[string]any{"effort": requestedEffort, "summary": "auto"}, "instructions": "channel policy"}
					} else {
						var messages []map[string]any
						require.NoError(t, common.UnmarshalJsonStr(gjson.Get(body, "messages").Raw, &messages))
						override["messages"] = append(messages, map[string]any{"role": "system", "content": "channel policy"})
					}
					common.SetContextKey(c, constant.ContextKeyChannelParamOverride, override)
					request, parseErr := helper.GetAndValidateRequest(c, format)
					require.NoError(t, parseErr)
					var apiErr *types.NewAPIError
					info := &relaycommon.RelayInfo{Request: request, OriginModelName: model, RelayFormat: format, RelayMode: relayconstant.Path2RelayMode(path), RequestURLPath: path, StartTime: time.Now(), IsStream: true, DisablePing: true}
					if responses {
						apiErr = ResponsesHelper(c, info)
					} else {
						apiErr = TextHelper(c, info)
					}
					require.NotNil(t, apiErr)
					require.Equal(t, 400, apiErr.StatusCode)
					require.True(t, info.SupportStreamOptions)
					require.Equal(t, wantEffort, info.ReasoningEffort)
					select {
					case raw := <-captured:
						if !passthrough {
							require.Equal(t, "qwen3.8-27b-fp8", gjson.GetBytes(raw, "model").String())
						}
						if responses {
							require.Equal(t, wantEffort, gjson.GetBytes(raw, "reasoning.effort").String())
							require.Equal(t, "call_1", gjson.GetBytes(raw, "input.1.call_id").String())
							policy := "channel policy\n\nlate policy"
							if passthrough {
								policy = "request policy\n\nlate policy"
							}
							require.Equal(t, policy, gjson.GetBytes(raw, "input.0.content").String())
							require.False(t, gjson.GetBytes(raw, "instructions").Exists())
							require.Equal(t, "false", gjson.GetBytes(raw, "store").Raw)
						} else {
							require.Equal(t, wantEffort, gjson.GetBytes(raw, "reasoning_effort").String())
							for key, want := range map[string]string{"temperature": "0", "parallel_tool_calls": "false", "tools.0.function.strict": "false", "messages.1.content": "null"} {
								require.Equal(t, want, gjson.GetBytes(raw, key).Raw)
							}
							policy := "late policy\n\ndeveloper policy"
							if !passthrough {
								policy += "\n\nchannel policy"
							}
							require.Equal(t, policy, gjson.GetBytes(raw, "messages.0.content").String())
							require.Equal(t, "reason", gjson.GetBytes(raw, "messages.1.reasoning_content").String())
							if channelType == constant.ChannelTypeLiteLLM {
								require.JSONEq(t, `["tools","reasoning_effort"]`, gjson.GetBytes(raw, "allowed_openai_params").Raw)
							}
						}
					default:
						t.Fatal("request did not reach the upstream")
					}
					common.CleanupBodyStorage(c)
					upstream.Close()
				}
			}
		}
	}
}

func TestSelfHostedInvalidToolChoiceIsClientError(t *testing.T) {
	for _, responses := range []bool{false, true} {
		path, format, fields := "/v1/chat/completions", types.RelayFormatOpenAI, `"messages":[{"role":"user","content":"hi"}]`
		if responses {
			path, format, fields = "/v1/responses", types.RelayFormatOpenAIResponses, `"input":"hi"`
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, strings.NewReader(`{"model":"qwen3.8-27b","tool_choice":"required",`+fields+`}`))
		c.Request.Header.Set("Content-Type", "application/json")
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeVLLM)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "http://unused.invalid")
		common.SetContextKey(c, constant.ContextKeyOriginalModel, "qwen3.8-27b")
		request, parseErr := helper.GetAndValidateRequest(c, format)
		require.NoError(t, parseErr)
		var apiErr *types.NewAPIError
		info := &relaycommon.RelayInfo{Request: request, OriginModelName: "qwen3.8-27b", RelayFormat: format, RelayMode: relayconstant.Path2RelayMode(path), RequestURLPath: path, StartTime: time.Now(), DisablePing: true}
		if responses {
			apiErr = ResponsesHelper(c, info)
		} else {
			apiErr = TextHelper(c, info)
		}
		require.NotNil(t, apiErr)
		require.Equal(t, 400, apiErr.StatusCode)
		require.True(t, types.IsSkipRetryError(apiErr))
		common.CleanupBodyStorage(c)
	}
}
