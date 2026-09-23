package opencode

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
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Opt-in anonymous requests to free models only; no account credentials are read.
func TestOpenCodeLiveClientIdentifiers(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE_TEST") != "1" {
		t.Skip("set OPENCODE_LIVE_TEST=1 to test the live OpenCode free models")
	}
	models := []string{"mimo-v2.5-free", "muse-spark-1.3-contributor-free"}
	if model := os.Getenv("OPENCODE_LIVE_MODEL"); model != "" {
		require.True(t, strings.HasSuffix(model, "-free"), "live test accepts only free models")
		models = []string{model}
	}
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			info := newRelayInfo(model)
			info.ApiKey = "public"
			info.IsChannelTest = true
			payload := map[string]any{"model": model, "stream": false}
			if endpoint(info) == constant.OpenCodeEndpointResponses {
				payload["input"] = "Reply with OK."
				payload["max_output_tokens"] = 32
				payload["store"] = false
			} else {
				payload["messages"] = []map[string]string{{"role": "user", "content": "Reply with OK."}}
				payload["max_tokens"] = 32
			}
			body, err := common.Marshal(payload)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			adaptor := &Adaptor{}
			adaptor.Init(info)
			raw, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
			require.NoError(t, err)
			response := raw.(*http.Response)
			defer response.Body.Close()
			responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2000))
			require.NoError(t, err)
			// Log only identity headers, never authorization or account credentials.
			for _, name := range []string{"User-Agent", "x-opencode-client", "x-opencode-session", "x-opencode-request", "x-opencode-project"} {
				t.Logf("outbound %s: %s", name, response.Request.Header.Get(name))
			}
			require.Equal(t, http.StatusOK, response.StatusCode, "upstream response: %s", responseBody)
			t.Logf("model=%s endpoint=%s status=%d", model, endpoint(info), response.StatusCode)
		})
	}
}
