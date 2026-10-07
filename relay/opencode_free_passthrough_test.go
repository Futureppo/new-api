package relay

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
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the real relay handlers, model mapping and outgoing HTTP request.
// A terminal fixture error avoids database billing while proving that generic
// passthrough settings cannot bypass OpenCode's required wire conversion.
func TestOpenCodeFreeRelayWithPassThrough(t *testing.T) {
	service.InitHttpClient()
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	for _, flags := range []struct {
		name            string
		global, channel bool
	}{{"off", false, false}, {"global", true, false}, {"channel", false, true}, {"both", true, true}} {
		for _, protocol := range []string{"chat", "claude", "gemini", "responses", "chat-to-responses", "claude-to-responses"} {
			t.Run(flags.name+"/"+protocol, func(t *testing.T) {
				settings.PassThroughRequestEnabled = flags.global
				upstreamModel := "mimo-v2.6-flash-free"
				upstreamPath := "/v1/chat/completions"
				if strings.Contains(protocol, "responses") {
					upstreamModel = "muse-spark-1.3-contributor-free"
					upstreamPath = "/v1/responses"
				}
				type capturedRequest struct {
					body    []byte
					headers http.Header
					path    string
				}
				captured := make(chan capturedRequest, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					captured <- capturedRequest{body, r.Header.Clone(), r.URL.Path}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":{"type":"fixture","message":"fixture reached upstream"}}`)
				}))
				defer server.Close()
				body := `{"model":"alias","messages":[{"role":"user","content":"Reply OK"}],"stream":false,"max_tokens":32}`
				var request dto.Request = &dto.GeneralOpenAIRequest{}
				handler := TextHelper
				format, mode, path := types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, "/v1/chat/completions"
				switch protocol {
				case "claude", "claude-to-responses":
					request, handler = &dto.ClaudeRequest{}, ClaudeHelper
					format, mode, path = types.RelayFormatClaude, relayconstant.RelayModeUnknown, "/v1/messages"
				case "gemini":
					body = `{"model":"alias","contents":[{"role":"user","parts":[{"text":"Reply OK"}]}]}`
					request, handler = &dto.GeminiChatRequest{}, GeminiHelper
					format, mode, path = types.RelayFormatGemini, relayconstant.RelayModeGemini, "/v1beta/models/alias:generateContent"
				case "responses":
					body = `{"model":"alias","input":"Reply OK","stream":false,"store":false}`
					request, handler = &dto.OpenAIResponsesRequest{}, ResponsesHelper
					format, mode, path = types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, "/v1/responses"
				}
				require.NoError(t, common.UnmarshalJsonStr(body, request))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("User-Agent", "third-party-agent")
				c.Request.Header.Set("x-opencode-client", "third-party-agent")
				common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenCode)
				common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
				common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-key")
				common.SetContextKey(c, constant.ContextKeyOriginalModel, "alias")
				common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: flags.channel})
				common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, map[string]any{
					"*": "", "User-Agent": "Go-http-client/2.0", "x-opencode-client": "other-agent",
				})
				c.Set("model_mapping", fmt.Sprintf(`{"alias":%q}`, upstreamModel))
				info := &relaycommon.RelayInfo{Request: request, OriginModelName: "alias", RequestURLPath: path, RelayMode: mode, RelayFormat: format}
				apiErr := handler(c, info)
				require.NotNil(t, apiErr)
				require.Contains(t, apiErr.Error(), "fixture reached upstream")
				require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
				got := <-captured
				require.Equal(t, upstreamPath, got.path)
				require.Equal(t, "Bearer upstream-key", got.headers.Get("Authorization"))
				require.True(t, strings.HasPrefix(got.headers.Get("User-Agent"), "opencode/"))
				require.Equal(t, "cli", got.headers.Get("x-opencode-client"))
				require.Equal(t, got.headers.Get("x-opencode-session"), got.headers.Get("x-opencode-session-id"))
				require.Equal(t, upstreamModel, gjson.GetBytes(got.body, "model").String())
				require.True(t, gjson.GetBytes(got.body, "stream").Bool())
				require.Len(t, gjson.GetBytes(got.body, "tools").Array(), 5)
				require.Contains(t, string(got.body), "Reply OK")
			})
		}
	}
}
