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
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Opt in explicitly: this test sends two small, anonymous requests to a model
// discovered from Kilo's current free catalogue. No credentials are used.
func TestKiloLiveAnonymousChat(t *testing.T) {
	if os.Getenv("KILO_LIVE_TEST") != "1" {
		t.Skip("set KILO_LIVE_TEST=1 to test the live anonymous Kilo gateway")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(constant.ChannelBaseURLs[constant.ChannelTypeKilo] + "/models")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var catalog struct {
		Data []struct {
			ID     string `json:"id"`
			IsFree bool   `json:"isFree"`
		} `json:"data"`
	}
	require.NoError(t, common.DecodeJson(resp.Body, &catalog))
	modelID := ""
	requestedModel := os.Getenv("KILO_LIVE_MODEL")
	for _, item := range catalog.Data {
		if item.IsFree && ((requestedModel != "" && item.ID == requestedModel) || (requestedModel == "" && strings.HasSuffix(item.ID, ":free"))) {
			modelID = item.ID
			break
		}
	}
	require.NotEmpty(t, modelID, "catalog must contain a free chat model")
	t.Logf("discovered free model: %s", modelID)
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "stream"}[stream], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			info := kiloTestInfo(stream)
			info.ApiKey = ""
			info.UpstreamModelName = modelID
			info.ChannelOtherSettings.KiloAnonymousEnabled = true
			request := &dto.GeneralOpenAIRequest{Model: modelID, Messages: []dto.Message{{Role: "user", Content: "Reply with OK."}}, MaxTokens: common.GetPointer(uint(32)), Stream: &stream}
			if stream {
				request.StreamOptions = &dto.StreamOptions{IncludeUsage: common.GetPointer(true)}
			}
			a := &Adaptor{}
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
			if response.StatusCode != http.StatusOK {
				defer response.Body.Close()
				errorBody, _ := io.ReadAll(io.LimitReader(response.Body, 1000))
				t.Fatalf("Kilo returned HTTP %d: %s", response.StatusCode, errorBody)
			}
			usage, apiErr := a.DoResponse(c, response, info)
			require.Nil(t, apiErr)
			require.Positive(t, usage.(*dto.Usage).TotalTokens)
			if stream {
				require.Contains(t, w.Body.String(), "[DONE]")
			} else {
				require.Contains(t, w.Body.String(), `"choices"`)
			}
			t.Logf("stream=%t, prompt_tokens=%d, completion_tokens=%d", stream, usage.(*dto.Usage).PromptTokens, usage.(*dto.Usage).CompletionTokens)
		})
	}
}
