package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponseHeadersHideUpstream(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, tc := range []struct {
		name, body, contentType string
		mode                    int
		stream                  bool
	}{
		{"chat", `{"id":"chat-test","choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, "application/json", relayconstant.RelayModeChatCompletions, false},
		{"responses", `{"id":"resp-test","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`, "application/json", relayconstant.RelayModeResponses, false},
		{"audio", "\x00\x01\x02\x03", "audio/pcm", relayconstant.RelayModeAudioSpeech, false},
		{"stream", "data: {\"id\":\"chat-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n", "text/event-stream", relayconstant.RelayModeChatCompletions, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Header("X-Oneapi-Request-Id", "gateway-request")
			info := officialTestInfo()
			info.RelayMode, info.RelayFormat = tc.mode, types.RelayFormatOpenAI
			info.IsStream, info.ShouldIncludeUsage, info.DisablePing = tc.stream, true, true
			info.Request = &dto.AudioRequest{ResponseFormat: "pcm"}
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{
				"Content-Type": {tc.contentType}, "Content-Length": {strconv.Itoa(len(tc.body))},
				"X-Litellm-Model-Api-Base": {"https://private.example/v1"}, "X-Litellm-Version": {"1.90.0"},
				"Server": {"uvicorn"}, "Llm_provider-Via": {"1.1 Caddy"}, "Set-Cookie": {"secret=value"},
				"X-Oneapi-Request-Id": {"upstream-request"}, "Location": {"https://private.example"},
			}}
			_, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, apiErr)
			result := w.Result()
			require.Equal(t, http.StatusOK, result.StatusCode)
			require.Equal(t, tc.contentType, result.Header.Get("Content-Type"))
			require.Equal(t, "gateway-request", result.Header.Get("X-Oneapi-Request-Id"))
			for _, name := range []string{"X-Litellm-Model-Api-Base", "X-Litellm-Version", "Server", "Llm_provider-Via", "Set-Cookie", "Location"} {
				require.Empty(t, result.Header.Get(name), name)
			}
			if tc.stream {
				require.Contains(t, w.Body.String(), "[DONE]")
				require.Empty(t, result.Header.Get("Content-Length"))
			} else {
				require.Equal(t, tc.body, w.Body.String())
				require.Equal(t, strconv.Itoa(len(tc.body)), result.Header.Get("Content-Length"))
			}
		})
	}
}
