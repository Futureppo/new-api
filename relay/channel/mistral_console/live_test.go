package mistralconsole

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestMistralConsoleLiveAdapter is opt-in so credentials never enter source or
// regular test output. Set MISTRAL_CONSOLE_TEST_COOKIE to the Cookie header
// value and optionally MISTRAL_CONSOLE_TEST_PROXY to run it.
func TestMistralConsoleLiveAdapter(t *testing.T) {
	runMistralConsoleLiveAdapter(t, false, false)
}

func TestMistralConsoleLiveDefaultParameters(t *testing.T) {
	runMistralConsoleLiveAdapter(t, true, false)
}

func TestMistralConsoleLiveEnabledTools(t *testing.T) {
	runMistralConsoleLiveAdapter(t, false, true)
}

func runMistralConsoleLiveAdapter(t *testing.T, omitParameters, enableTools bool) {
	t.Helper()
	cookie := os.Getenv("MISTRAL_CONSOLE_TEST_COOKIE")
	if cookie == "" {
		t.Skip("MISTRAL_CONSOLE_TEST_COOKIE is not set")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	if proxyValue := os.Getenv("MISTRAL_CONSOLE_TEST_PROXY"); proxyValue != "" {
		proxyURL, err := url.Parse(proxyValue)
		require.NoError(t, err)
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport, Timeout: 90 * time.Second}
	gin.SetMode(gin.TestMode)
	for _, model := range ModelList {
		t.Run(model, func(t *testing.T) {
			info := testRelayInfo(false)
			info.ApiKey = cookie
			info.UpstreamModelName = model
			info.ChannelBaseUrl = "https://console.mistral.ai"
			if enableTools {
				info.ChannelOtherSettings = dto.ChannelOtherSettings{
					MistralConsoleCodeInterpreterEnabled: common.GetPointer(true),
					MistralConsoleImageGenerationEnabled: common.GetPointer(true),
					MistralConsoleWebSearchEnabled:       common.GetPointer(true),
				}
			}
			request := &dto.GeneralOpenAIRequest{
				MaxTokens:   common.GetPointer(uint(2048)),
				Temperature: common.GetPointer(0.7), TopP: common.GetPointer(1.0),
				Messages: []dto.Message{{Role: "user", Content: "你好，请简短回复。"}},
			}
			if omitParameters {
				request.MaxTokens = nil
				request.Temperature = nil
				request.TopP = nil
			}
			adaptor := &Adaptor{}
			converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			upstreamURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			upstreamRequest, err := http.NewRequest(http.MethodPost, upstreamURL, bytes.NewReader(body))
			require.NoError(t, err)
			ctx, _ := gin.CreateTestContext(nil)
			ctx.Request = upstreamRequest
			require.NoError(t, adaptor.SetupRequestHeader(ctx, &upstreamRequest.Header, info))
			response, err := client.Do(upstreamRequest)
			require.NoError(t, err)
			if response.StatusCode != http.StatusOK {
				defer response.Body.Close()
				message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
				t.Fatalf("unexpected Bora status %d: %s", response.StatusCode, message)
			}
			state := newBoraResponseState(ctx, info)
			err = consumeBoraSSE(response, func(eventName string, event boraStreamEvent) error {
				_, handleErr := state.handleEvent(eventName, event)
				return handleErr
			})
			require.NoError(t, err)
			require.True(t, state.completed)
			require.NotEmpty(t, state.text.String())
			require.NotNil(t, state.usage)
		})
	}
}
