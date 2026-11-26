package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/ir"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAzureProductionRequestPipeline(t *testing.T) {
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	saved := *settings
	t.Cleanup(func() { *settings = saved })
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{}
	for _, irEnabled := range []bool{false, true} {
		for _, tc := range []struct{ name, model, source, fields, path, token string }{
			{"terra chat", "gpt-6.1-terra", "chat", `"max_tokens":32,"temperature":0`, "/openai/deployments/opaque.v2-high/chat/completions", "max_completion_tokens"},
			{"sol chat", "gpt-6.2-sol", "chat", `"max_tokens":32`, "/openai/deployments/opaque.v2-high/chat/completions", "max_completion_tokens"},
			{"sol tools", "gpt-6.2-sol", "chat", `"max_tokens":32,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]`, "/openai/v1/responses", "max_output_tokens"},
			{"native responses", "gpt-6.2-sol", "responses", `"max_output_tokens":32`, "/openai/v1/responses", "max_output_tokens"},
			{"claude client to gpt", "gpt-6.1-terra", "claude", `"max_tokens":32`, "/openai/deployments/opaque.v2-high/chat/completions", "max_completion_tokens"},
			{"chat client to claude", "claude-opus-5", "chat", `"max_tokens":32`, "/anthropic/v1/messages", "max_tokens"},
			{"native claude", "claude-opus-5", "claude", `"max_tokens":32`, "/anthropic/v1/messages", "max_tokens"},
		} {
			for _, stream := range []bool{false, true} {
				t.Run(tc.name+"/ir="+strconv.FormatBool(irEnabled)+"/stream="+strconv.FormatBool(stream), func(t *testing.T) {
					setIRToggleForTest(t, irEnabled)
					type wire struct {
						path   string
						body   []byte
						header http.Header
					}
					captured := make(chan wire, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						b, _ := io.ReadAll(r.Body)
						captured <- wire{r.URL.Path, b, r.Header.Clone()}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(400)
						_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"mock upstream"}}`)
					}))
					defer server.Close()
					path, format, input := "/v1/chat/completions", types.RelayFormatOpenAI, `"messages":[{"role":"user","content":"hi"}]`
					if tc.source == "responses" {
						path, format, input = "/v1/responses", types.RelayFormatOpenAIResponses, `"input":"hi"`
					}
					if tc.source == "claude" {
						path, format = "/v1/messages", types.RelayFormatClaude
					}
					body := `{"model":"alias","stream":` + strconv.FormatBool(stream) + `,` + input + `,` + tc.fields + `}`
					c, _ := newIRTestGinContext(http.MethodPost, path)
					c.Request.Body = io.NopCloser(strings.NewReader(body))
					common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAzure)
					common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
					common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
					common.SetContextKey(c, constant.ContextKeyOriginalModel, "alias")
					common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"alias":"opaque.v2-high"}`)
					common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{AzureDeploymentModels: map[string]string{"opaque.v2-high": tc.model}})
					// Overrides happen after conversion; their token value must be normalized too.
					if tc.source == "chat" && !strings.HasPrefix(tc.model, "claude-") {
						common.SetContextKey(c, constant.ContextKeyChannelParamOverride, map[string]any{"max_tokens": 0})
					}
					request, err := helper.GetAndValidateRequest(c, format)
					require.NoError(t, err)
					info := &relaycommon.RelayInfo{Request: request, OriginModelName: "alias", RelayFormat: format, RelayMode: relayconstant.Path2RelayMode(path), RequestURLPath: path, StartTime: time.Now(), DisablePing: true, IsStream: stream}
					var apiErr *types.NewAPIError
					switch tc.source {
					case "responses":
						apiErr = ResponsesHelper(c, info)
					case "claude":
						apiErr = ClaudeHelper(c, info)
					default:
						apiErr = TextHelper(c, info)
					}
					require.NotNil(t, apiErr)
					require.Equal(t, 400, apiErr.StatusCode, apiErr.Error())
					require.Len(t, captured, 1, apiErr.Error())
					got := <-captured
					require.Equal(t, tc.path, got.path)
					require.Equal(t, "opaque.v2-high", gjson.GetBytes(got.body, "model").String())
					require.True(t, gjson.GetBytes(got.body, tc.token).Exists(), string(got.body))
					if tc.source == "chat" && !strings.HasPrefix(tc.model, "claude-") {
						require.Equal(t, "0", gjson.GetBytes(got.body, tc.token).Raw)
						require.False(t, gjson.GetBytes(got.body, "max_tokens").Exists())
					}
					if strings.HasPrefix(tc.model, "claude-") {
						require.Equal(t, "test-key", got.header.Get("x-api-key"))
						require.NotEmpty(t, got.header.Get("anthropic-version"))
					} else {
						require.Equal(t, "test-key", got.header.Get("api-key"))
					}
					require.Empty(t, got.header.Get("Authorization"))
				})
			}
		}
	}
}

func TestAzureClaudeResponsesBridge(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)
	for _, stream := range []bool{false, true} {
		t.Run(strconv.FormatBool(stream), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/anthropic/v1/messages", r.URL.Path)
				require.Equal(t, "test-key", r.Header.Get("x-api-key"))
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				require.Equal(t, "deployment", body["model"])
				if !stream {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"deployment","content":[{"type":"text","text":"Hello"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":2}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				for _, event := range []struct{ name, data string }{
					{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"deployment","content":[],"usage":{"input_tokens":5,"output_tokens":0}}}`},
					{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
					{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
					{"content_block_stop", `{"type":"content_block_stop","index":0}`},
					{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`},
					{"message_stop", `{"type":"message_stop"}`},
				} {
					_, _ = io.WriteString(w, "event: "+event.name+"\ndata: "+event.data+"\n\n")
				}
			}))
			defer server.Close()
			c, rec := newIRTestGinContext(http.MethodPost, "/v1/responses")
			info := &relaycommon.RelayInfo{OriginModelName: "claude-opus-5", ClientModelName: "claude-opus-5", ModelMappingTargetName: "deployment", RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses, RequestURLPath: "/v1/responses", IsStream: stream, DisablePing: true, StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeAzure, ApiType: constant.APITypeOpenAI, ChannelBaseUrl: server.URL, ApiKey: "test-key", UpstreamModelName: "deployment"}}
			info.InitRequestConversionChain()
			relaycommon.SetRelayInfo(c, info)
			relaycommon.InstallRelayResponseWriter(c)
			var req dto.OpenAIResponsesRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"deployment","input":"hi","stream":`+strconv.FormatBool(stream)+`}`, &req))
			handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, &openai.Adaptor{}, ir.FormatOpenAIResponses, &req)
			require.True(t, handled)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			require.Equal(t, 5, usage.PromptTokens)
			require.Equal(t, 2, usage.CompletionTokens)
			require.Contains(t, rec.Body.String(), "Hello")
			if stream {
				require.Contains(t, rec.Body.String(), "response.completed")
			} else {
				require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
			}
		})
	}
}
