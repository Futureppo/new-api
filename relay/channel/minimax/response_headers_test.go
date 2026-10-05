package minimax

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMiniMaxResponseHeadersHideUpstream(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"choices":[{"message":{"content":"OK"}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{
		"Content-Type": {"application/json"}, "Server": {"private-provider"},
		"X-Litellm-Model-Api-Base": {"https://private.example/v1"}, "Set-Cookie": {"secret=value"},
	}}
	_, apiErr := handleChatCompletionResponse(c, resp, nil)
	require.Nil(t, apiErr)
	require.Equal(t, body, w.Body.String())
	for _, name := range []string{"Server", "X-Litellm-Model-Api-Base", "Set-Cookie"} {
		require.Empty(t, w.Result().Header.Get(name), name)
	}
}
