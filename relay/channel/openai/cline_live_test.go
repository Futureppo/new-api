package openai

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
	"github.com/QuantumNous/new-api/relay/channel/cline"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Explicit opt-in: discovers a currently free model and sends two small chat
// requests. Credentials come only from the environment and are never logged.
func TestClineLiveChat(t *testing.T) {
	if os.Getenv("CLINE_LIVE_TEST") != "1" {
		t.Skip("set CLINE_LIVE_TEST=1 and CLINE_API_KEY to verify live Cline chat")
	}
	key := os.Getenv("CLINE_API_KEY")
	if key == "" {
		t.Fatal("CLINE_API_KEY is required")
	}
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Get(constant.ChannelBaseURLs[constant.ChannelTypeCline] + cline.FreeModelsPath)
	require.NoError(t, err)
	defer resp.Body.Close()
	catalogBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	t.Logf("catalog HTTP %d content-type=%s\n%s", resp.StatusCode, resp.Header.Get("Content-Type"), strings.ReplaceAll(string(catalogBody), key, "[REDACTED]"))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var catalog struct {
		Free []struct {
			ID string `json:"id"`
		} `json:"free"`
	}
	require.NoError(t, common.Unmarshal(catalogBody, &catalog))
	require.NotEmpty(t, catalog.Free)
	modelID := catalog.Free[0].ID
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 40
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			info := clineTestInfo(stream)
			info.ApiKey = key
			info.UpstreamModelName = modelID
			request := &dto.GeneralOpenAIRequest{Model: modelID, Messages: []dto.Message{{Role: "user", Content: "Reply only OK."}}, MaxTokens: common.GetPointer(uint(32)), Stream: &stream}
			if stream {
				request.StreamOptions = &dto.StreamOptions{IncludeUsage: common.GetPointer(true)}
			}
			a := &Adaptor{}
			a.Init(info)
			converted, err := a.ConvertOpenAIRequest(nil, info, request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			raw, err := a.DoRequest(c, info, bytes.NewReader(body))
			require.NoError(t, err)
			response := raw.(*http.Response)
			responseBody, err := io.ReadAll(response.Body)
			response.Body.Close()
			require.NoError(t, err)
			t.Logf("chat stream=%t HTTP %d content-type=%s\n%s", stream, response.StatusCode, response.Header.Get("Content-Type"), strings.ReplaceAll(string(responseBody), key, "[REDACTED]"))
			require.Equal(t, http.StatusOK, response.StatusCode)
			response.Body = io.NopCloser(bytes.NewReader(responseBody))
			defer response.Body.Close()
			usage, apiErr := a.DoResponse(c, response, info)
			require.Nil(t, apiErr)
			require.Positive(t, usage.(*dto.Usage).TotalTokens)
			require.Contains(t, w.Body.String(), "OK")
			if stream {
				require.Contains(t, w.Body.String(), "[DONE]")
			} else {
				var result dto.OpenAITextResponse
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
				require.NotEmpty(t, result.Choices)
			}
			t.Logf("model=%s stream=%t prompt_tokens=%d completion_tokens=%d", modelID, stream, usage.(*dto.Usage).PromptTokens, usage.(*dto.Usage).CompletionTokens)
		})
	}
}
