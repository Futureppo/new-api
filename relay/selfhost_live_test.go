package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Opt in explicitly; CI never sends inference traffic to a deployment.
func TestLiveSelfHosted(t *testing.T) {
	base := os.Getenv("SELFHOST_LIVE_BASE_URL")
	models := os.Getenv("SELFHOST_LIVE_MODELS")
	if base == "" || models == "" {
		t.Skip("set SELFHOST_LIVE_BASE_URL and SELFHOST_LIVE_MODELS")
	}
	channelType := constant.ChannelTypeLiteLLM
	if os.Getenv("SELFHOST_LIVE_TYPE") == "vllm" {
		channelType = constant.ChannelTypeVLLM
	}
	service.InitHttpClient()
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	invoke := func(t *testing.T, body map[string]any, responses, stream bool) []byte {
		t.Helper()
		path, format := "/v1/chat/completions", types.RelayFormatOpenAI
		if responses {
			path, format = "/v1/responses", types.RelayFormatOpenAIResponses
		}
		raw, err := common.Marshal(body)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		c.Request = httptest.NewRequest("POST", path, bytes.NewReader(raw)).WithContext(ctx)
		c.Request.Header.Set("Content-Type", "application/json")
		defer common.CleanupBodyStorage(c)
		request, err := helper.GetAndValidateRequest(c, format)
		require.NoError(t, err)
		apiType, _ := common.ChannelType2APIType(channelType)
		a := GetAdaptor(apiType)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType, ApiType: apiType, ChannelBaseUrl: base, ApiKey: os.Getenv("SELFHOST_LIVE_KEY"), UpstreamModelName: body["model"].(string), SupportStreamOptions: true}, OriginModelName: body["model"].(string), Request: request, RelayMode: relayconstant.Path2RelayMode(path), RelayFormat: format, IsStream: stream, ShouldIncludeUsage: true, DisablePing: true, StartTime: time.Now()}
		a.Init(info)
		var converted any
		if responses {
			converted, err = a.ConvertOpenAIResponsesRequest(c, info, *request.(*dto.OpenAIResponsesRequest))
		} else {
			converted, err = a.ConvertOpenAIRequest(c, info, request.(*dto.GeneralOpenAIRequest))
		}
		require.NoError(t, err)
		raw, err = common.Marshal(converted)
		require.NoError(t, err)
		result, err := a.DoRequest(c, info, bytes.NewReader(raw))
		require.NoError(t, err)
		resp := result.(*http.Response)
		if resp.StatusCode != 200 {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("upstream HTTP %d: %s", resp.StatusCode, data)
		}
		usage, apiErr := a.DoResponse(c, resp, info)
		require.Nil(t, apiErr)
		require.Equal(t, "medium", info.ReasoningEffort)
		require.Positive(t, usage.(*dto.Usage).TotalTokens)
		if stream {
			require.False(t, info.StreamStatus.HasErrors(), info.StreamStatus)
		}
		return append([]byte(nil), w.Body.Bytes()...)
	}
	for _, model := range strings.Split(models, ",") {
		for _, responses := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				name := model + "/chat"
				if responses {
					name = model + "/responses"
				}
				if stream {
					name += "/stream"
				} else {
					name += "/json"
				}
				t.Run(name+"/message_order", func(t *testing.T) {
					cases := map[string]string{
						"late_system":     `"messages":[{"role":"user","content":"Reply only OK."},{"role":"system","content":"Be concise."}]`,
						"leading_systems": `"messages":[{"role":"system","content":"Be concise."},{"role":"system","content":"Use English."},{"role":"user","content":"Reply only OK."}]`,
						"developer_parts": `"messages":[{"role":"system","content":[{"type":"text","text":"Be concise."}]},{"role":"user","content":"Reply only OK."},{"role":"developer","content":"Use English."}]`,
					}
					if responses {
						cases = map[string]string{
							"late_system":        `"input":[{"role":"user","content":"Reply only OK."},{"role":"system","content":"Be concise."}]`,
							"leading_systems":    `"input":[{"role":"system","content":"Be concise."},{"role":"system","content":"Use English."},{"role":"user","content":"Reply only OK."}]`,
							"instructions_parts": `"instructions":"Be concise.","input":[{"role":"user","content":"Reply only OK."},{"role":"system","content":[{"type":"input_text","text":"Use English."}]}]`,
						}
					}
					for scenario, fields := range cases {
						t.Run(scenario, func(t *testing.T) {
							var body map[string]any
							require.NoError(t, common.UnmarshalJsonStr("{"+fields+"}", &body))
							body["model"], body["stream"] = strings.TrimSpace(model), stream
							if responses {
								body["reasoning"] = map[string]any{"effort": "high"}
								body["max_output_tokens"], body["store"] = 96, false
							} else {
								body["reasoning_effort"], body["max_tokens"] = "high", 96
								if stream {
									body["stream_options"] = map[string]any{"include_usage": true}
								}
							}
							out := invoke(t, body, responses, stream)
							if stream {
								if responses {
									require.Contains(t, string(out), "response.completed")
								} else {
									require.Contains(t, string(out), "[DONE]")
								}
							} else if responses {
								require.Equal(t, "completed", gjson.GetBytes(out, "status").String())
							} else {
								require.Equal(t, "stop", gjson.GetBytes(out, "choices.0.finish_reason").String())
							}
						})
					}
				})
				t.Run(name, func(t *testing.T) {
					fn := map[string]any{"name": "lookup_weather", "parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}}}
					body := map[string]any{"model": strings.TrimSpace(model), "stream": stream, "tool_choice": "required"}
					if responses {
						fn["type"] = "function"
						body["input"] = "Call lookup_weather for Beijing."
						body["tools"] = []any{fn}
						body["reasoning"] = map[string]any{"effort": "high"}
						body["max_output_tokens"] = 256
						body["store"] = false
					} else {
						body["messages"] = []any{map[string]any{"role": "user", "content": "Call lookup_weather for Beijing."}}
						body["tools"] = []any{map[string]any{"type": "function", "function": fn}}
						body["reasoning_effort"] = "high"
						body["max_tokens"] = 256
						if stream {
							body["stream_options"] = map[string]any{"include_usage": true}
						}
					}
					out := invoke(t, body, responses, stream)
					require.Contains(t, string(out), "lookup_weather")
					require.Contains(t, string(out), "Beijing")
					if stream {
						if responses {
							require.Contains(t, string(out), "response.completed")
						} else {
							require.Contains(t, string(out), "[DONE]")
						}
						return
					}
					body["tool_choice"] = "none"
					if responses {
						var inputs []any
						inputs = append(inputs, map[string]any{"role": "user", "content": "Get weather for Beijing, then report the temperature."})
						for _, item := range gjson.GetBytes(out, "output").Array() {
							if item.Get("type").String() == "function_call" {
								var call map[string]any
								require.NoError(t, common.Unmarshal([]byte(item.Raw), &call))
								inputs = append(inputs, call, map[string]any{"type": "function_call_output", "call_id": call["call_id"], "output": "The temperature is 23 degrees Celsius."})
							}
						}
						require.Greater(t, len(inputs), 1)
						inputs = append(inputs, map[string]any{"role": "system", "content": "Report the temperature from the tool result concisely."})
						body["input"] = inputs
					} else {
						var message map[string]any
						require.NoError(t, common.Unmarshal([]byte(gjson.GetBytes(out, "choices.0.message").Raw), &message))
						messages := append(body["messages"].([]any), message)
						calls := gjson.GetBytes(out, "choices.0.message.tool_calls").Array()
						require.NotEmpty(t, calls)
						for _, call := range calls {
							messages = append(messages, map[string]any{"role": "tool", "tool_call_id": call.Get("id").String(), "content": "The temperature is 23 degrees Celsius."})
						}
						messages = append(messages, map[string]any{"role": "system", "content": "Report the temperature from the tool result concisely."})
						body["messages"] = messages
					}
					out = invoke(t, body, responses, false)
					require.Contains(t, string(out), "23")
				})
			}
		}
	}
}
