package mistralconsole

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestMistralConsoleLiveAdapter is opt-in so credentials never enter source or
// regular test output. Set MISTRAL_CONSOLE_TEST_COOKIE to the Cookie header
// value and optionally MISTRAL_CONSOLE_TEST_PROXY to run it.
func TestMistralConsoleLiveAdapter(t *testing.T) {
	cookie := os.Getenv("MISTRAL_CONSOLE_TEST_COOKIE")
	if cookie == "" {
		t.Skip("MISTRAL_CONSOLE_TEST_COOKIE is not set")
	}

	info := testRelayInfo(false)
	info.ApiKey = cookie
	info.ChannelBaseUrl = "https://console.mistral.ai"
	maxTokens := uint(512)
	request := &dto.GeneralOpenAIRequest{
		MaxTokens: &maxTokens,
		// Deliberately omit Tools: the adapter must force-enable Bora built-ins.
		Messages: []dto.Message{{
			Role:    "user",
			Content: "Use the built-in image generation tool to create a simple yellow banana on a white background.",
		}},
	}
	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)

	upstreamRequest, err := http.NewRequest(http.MethodPost, "https://console.mistral.ai"+conversationsURL, bytes.NewReader(body))
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request = upstreamRequest
	require.NoError(t, adaptor.SetupRequestHeader(ctx, &upstreamRequest.Header, info))

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if proxyValue := os.Getenv("MISTRAL_CONSOLE_TEST_PROXY"); proxyValue != "" {
		proxyURL, err := url.Parse(proxyValue)
		require.NoError(t, err)
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport}
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
	require.True(t, strings.Contains(state.text.String(), "![Generated image]("), "expected generated image markdown in response")
}
