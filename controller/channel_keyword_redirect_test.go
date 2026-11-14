package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChannelKeywordRedirectSaveDisableAndReload(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	channel := model.Channel{Type: constant.ChannelTypeOpenAI, Name: "keyword-rules", Key: "test-key", Models: "keyword-model", Group: "default", Status: common.ChannelStatusEnabled}
	settings := dto.ChannelSettings{KeywordRedirect: &dto.ChannelKeywordRedirectSettings{Enabled: true, Rules: map[string]string{"apple": "pear", "remove": "", "0": "false"}}}
	channel.SetSetting(settings)
	callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &channel}, AddChannel)
	var saved model.Channel
	require.NoError(t, db.Where("name = ?", channel.Name).First(&saved).Error)
	require.Equal(t, settings.KeywordRedirect, saved.GetSetting().KeywordRedirect)
	for _, enabled := range []bool{false, true} {
		settings.KeywordRedirect.Enabled = enabled
		body, err := common.Marshal(settings)
		require.NoError(t, err)
		callKiloChannelMutation(t, http.MethodPut, map[string]any{"id": saved.Id, "setting": string(body)}, UpdateChannel)
		require.NoError(t, db.First(&saved, saved.Id).Error)
		require.Equal(t, settings.KeywordRedirect, saved.GetSetting().KeywordRedirect)
	}
	callKiloChannelMutation(t, http.MethodPut, map[string]any{"id": saved.Id, "name": "keyword-renamed"}, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.Equal(t, settings.KeywordRedirect, saved.GetSetting().KeywordRedirect)
	for _, rules := range []string{`{"":"x"}`, `{"word":null}`, `{"word":false}`, `{"word":0}`} {
		saved.Setting = common.GetPointer(`{"keyword_redirect":{"enabled":true,"rules":` + rules + `}}`)
		require.Error(t, validateChannel(&saved, false))
	}
}

func TestKeywordRedirectGatewayProtocolsBillingAndLogs(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	chat := `{"id":"chat-1","object":"chat.completion","model":"keyword-model","choices":[{"index":0,"message":{"role":"assistant","content":"apple","reasoning_content":"apple"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
	responses := `{"id":"resp-1","object":"response","model":"keyword-model","status":"completed","output":[{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"output_text","text":"apple"}]}],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
	claude := `{"id":"msg-1","type":"message","role":"assistant","model":"keyword-model","content":[{"type":"thinking","thinking":"apple","signature":"signature"},{"type":"text","text":"apple"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`
	gemini := `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"thought":true,"text":"apple","thoughtSignature":"signature"},{"text":"apple"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
	tests := []struct {
		name      string
		channel   int
		format    types.RelayFormat
		path      string
		request   string
		response  string
		streaming []string
	}{
		{"chat", constant.ChannelTypeOpenAI, types.RelayFormatOpenAI, "/v1/chat/completions", `{"model":"keyword-model","messages":[{"role":"user","content":"apple"}]}`, chat, []string{
			`{"id":"chat-1","object":"chat.completion.chunk","model":"keyword-model","choices":[{"index":0,"delta":{"reasoning_content":"apple"}}]}`,
			`{"id":"chat-1","object":"chat.completion.chunk","model":"keyword-model","choices":[{"index":0,"delta":{"content":"app"}}]}`,
			`{"id":"chat-1","object":"chat.completion.chunk","model":"keyword-model","choices":[{"index":0,"delta":{"content":"le"}}]}`,
			`{"id":"chat-1","object":"chat.completion.chunk","model":"keyword-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			`[DONE]`,
		}},
		{"responses", constant.ChannelTypeOpenAI, types.RelayFormatOpenAIResponses, "/v1/responses", `{"model":"keyword-model","input":"apple"}`, responses, []string{
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"sequence_number":0,"delta":"app"}`,
			`{"type":"response.output_text.delta","output_index":0,"content_index":0,"sequence_number":1,"delta":"le"}`,
			`{"type":"response.completed","sequence_number":2,"response":` + responses + `}`,
		}},
		{"claude", constant.ChannelTypeAnthropic, types.RelayFormatClaude, "/v1/messages", `{"model":"keyword-model","max_tokens":32,"messages":[{"role":"user","content":"apple"}]}`, claude, []string{
			`{"type":"message_start","message":{"id":"msg-1","type":"message","role":"assistant","model":"keyword-model","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"apple"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signature"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"app"}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"le"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			`{"type":"message_stop"}`,
		}},
		{"gemini", constant.ChannelTypeGemini, types.RelayFormatGemini, "/v1beta/models/keyword-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"apple"}]}]}`, gemini, []string{
			`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"thought":true,"text":"apple","thoughtSignature":"signature"}]}}]}`,
			`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"app"}]}}]}`,
			`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"le"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`,
		}},
	}
	for _, tt := range tests {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tt.name, stream), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Contains(t, string(body), "apple")
					require.NotContains(t, string(body), "pear")
					if !stream {
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, tt.response)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					for _, data := range tt.streaming {
						if name := gjson.Get(data, "type").String(); name != "" {
							_, _ = fmt.Fprintf(w, "event: %s\n", name)
						}
						_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
						w.(http.Flusher).Flush()
					}
				}))
				defer upstream.Close()
				db, engine, channel := setupDecisionsGateway(t, upstream.URL)
				channel.Type, channel.Models = tt.channel, "keyword-model"
				channel.SetOtherSettings(dto.ChannelOtherSettings{ConversationLogEnabled: true})
				require.NoError(t, channel.UpdateAbilities(db))
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"keyword-model":1}`))
				if tt.format == types.RelayFormatClaude {
					engine.POST("/v1/messages", func(c *gin.Context) { Relay(c, types.RelayFormatClaude) })
				}
				if tt.format == types.RelayFormatGemini {
					engine.POST("/v1beta/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
				}
				path, body := tt.path, tt.request
				if stream {
					if tt.format == types.RelayFormatGemini {
						path = strings.Replace(path, ":generateContent", ":streamGenerateContent?alt=sse", 1)
					} else {
						body = strings.TrimSuffix(body, "}") + `,"stream":true}`
					}
				}
				var baselineQuota int
				for _, enabled := range []bool{false, true} {
					channel.SetSetting(dto.ChannelSettings{ShowErrorDetails: true, KeywordRedirect: &dto.ChannelKeywordRedirectSettings{Enabled: enabled, Rules: map[string]string{"apple": "pear", "pear": "fruit"}}})
					require.NoError(t, channel.Save())
					w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					var usageLog model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Last(&usageLog).Error)
					require.Equal(t, 10, usageLog.PromptTokens)
					require.Equal(t, 5, usageLog.CompletionTokens)
					if !enabled {
						baselineQuota = usageLog.Quota
						continue
					}
					require.Contains(t, w.Body.String(), "pear")
					require.NotContains(t, w.Body.String(), "fruit")
					require.Equal(t, baselineQuota, usageLog.Quota)
					var log model.ConversationLog
					require.NoError(t, db.Last(&log).Error)
					require.Equal(t, w.Body.String(), log.ClientResponseBody)
					require.Contains(t, log.ClientRequestBody, "apple")
					require.Contains(t, log.UpstreamResponseBody, streamKeywordOriginal(tt.format, stream))
				}
			})
		}
	}
}

func streamKeywordOriginal(format types.RelayFormat, stream bool) string {
	if stream && format == types.RelayFormatOpenAIResponses {
		return `"delta":"app"`
	}
	return "apple"
}
