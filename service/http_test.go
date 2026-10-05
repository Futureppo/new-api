package service

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIOCopyBytesGracefullyFiltersUpstreamHeaders(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Header("X-Oneapi-Request-Id", "gateway-request")
			body := []byte(`{"result":"rewritten"}`)
			resp := &http.Response{StatusCode: status, Header: http.Header{
				"Content-Type": {"application/json"}, "Content-Length": {"999"},
				"X-Litellm-Model-Api-Base": {"https://private.example/v1"},
				"Server":                   {"uvicorn"}, "Set-Cookie": {"secret=value"},
				"X-Oneapi-Request-Id": {"upstream-request"}, "Retry-After": {"2"},
			}}
			IOCopyBytesGracefully(c, resp, body)
			result := w.Result()
			require.Equal(t, status, result.StatusCode)
			require.Equal(t, string(body), w.Body.String())
			require.Equal(t, strconv.Itoa(len(body)), result.Header.Get("Content-Length"))
			require.Equal(t, "application/json", result.Header.Get("Content-Type"))
			require.Equal(t, "gateway-request", result.Header.Get("X-Oneapi-Request-Id"))
			require.Equal(t, "2", result.Header.Get("Retry-After"))
			for _, name := range []string{"X-Litellm-Model-Api-Base", "Server", "Set-Cookie"} {
				require.Empty(t, result.Header.Get(name), name)
			}
		})
	}
}
