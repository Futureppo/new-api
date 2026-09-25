package openai

import (
	"bytes"
	"context"
	"fmt"
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
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Explicit opt-in: exercise every current free model, not just the first one.
// Credentials come only from the environment and are never logged.
func TestClineLiveChat(t *testing.T) {
	if os.Getenv("CLINE_LIVE_TEST") != "1" {
		t.Skip("set CLINE_LIVE_TEST=1 and CLINE_API_KEY to verify live Cline chat")
	}
	key := os.Getenv("CLINE_API_KEY")
	if key == "" {
		t.Fatal("CLINE_API_KEY is required")
	}
	client := &http.Client{Timeout: 25 * time.Second}
	req, err := http.NewRequest(http.MethodGet, constant.ChannelBaseURLs[constant.ChannelTypeCline]+cline.FreeModelsPath, nil)
	require.NoError(t, err)
	cline.SetClientHeaders(req.Header)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
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
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 45
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	selected := os.Getenv("CLINE_LIVE_MODEL")
	matched := false
	for _, model := range catalog.Free {
		modelID := model.ID
		if selected != "" && selected != modelID {
			continue
		}
		matched = true
		for _, useTools := range []bool{false, true} {
			if useTools && !strings.Contains(modelID, "deepseek") {
				continue
			}
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/tools=%t/stream=%t", modelID, useTools, stream), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					info := clineTestInfo(stream)
					info.ApiKey = key
					info.UpstreamModelName = modelID
					info.HeadersOverride = map[string]any{"*": ""}
					request := &dto.GeneralOpenAIRequest{Model: modelID, Messages: []dto.Message{{Role: "user", Content: "Reply only OK."}}, MaxTokens: common.GetPointer(uint(128)), Stream: &stream}
					if useTools {
						require.NoError(t, common.UnmarshalJsonStr(`{"tools":[{"type":"function","function":{"name":"lookup","description":"Return the current test status.","parameters":{"type":"object","properties":{},"additionalProperties":false}}}],"tool_choice":{"type":"function","function":{"name":"lookup"}},"parallel_tool_calls":false}`, request))
						request.Messages[0].Content = "Call lookup with no arguments."
					}
					if stream {
						request.StreamOptions = &dto.StreamOptions{IncludeUsage: common.GetPointer(true)}
					}
					a := &Adaptor{}
					a.Init(info)
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
					c.Request.Header.Set("Content-Type", "application/json")
					c.Request.Header.Set("User-Agent", "OtherClient/1.0")
					c.Request.Header.Set("X-CLIENT-TYPE", "other-client")
					if _, alias, ok := strings.Cut(modelID, "/"); ok {
						mapping, err := common.Marshal(map[string]string{alias: modelID})
						require.NoError(t, err)
						c.Set("model_mapping", string(mapping))
						info.OriginModelName, info.ClientModelName = alias, alias
						request.Model = alias
						require.NoError(t, helper.ModelMappedHelper(c, info, request))
						require.Equal(t, modelID, request.Model)
						t.Logf("client model=%s upstream model=%s", alias, request.Model)
					}
					converted, err := a.ConvertOpenAIRequest(c, info, request)
					require.NoError(t, err)
					body, err := common.Marshal(converted)
					require.NoError(t, err)
					raw, err := a.DoRequest(c, info, bytes.NewReader(body))
					require.NoError(t, err)
					response := raw.(*http.Response)
					defer response.Body.Close()
					var responseBody bytes.Buffer
					// Capture the actual wire response without buffering ahead of the relay.
					response.Body = &clineRecordingBody{Reader: io.TeeReader(response.Body, &responseBody), Closer: response.Body}
					if response.StatusCode != http.StatusOK {
						_, _ = io.Copy(io.Discard, response.Body)
						t.Fatalf("model=%s HTTP %d\n%s", modelID, response.StatusCode, strings.ReplaceAll(responseBody.String(), key, "[REDACTED]"))
					}
					usage, apiErr := a.DoResponse(c, response, info)
					t.Logf("model=%s stream=%t tools=%t HTTP %d content-type=%s\n%s", modelID, stream, useTools, response.StatusCode, response.Header.Get("Content-Type"), strings.ReplaceAll(responseBody.String(), key, "[REDACTED]"))
					require.Nil(t, apiErr)
					require.True(t, strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream"), "Cline must use SSE upstream even for non-stream clients")
					require.Positive(t, usage.(*dto.Usage).TotalTokens)
					var content, toolName strings.Builder
					if stream {
						require.Contains(t, w.Body.String(), "[DONE]")
						for _, line := range strings.Split(w.Body.String(), "\n") {
							data, ok := strings.CutPrefix(line, "data: ")
							if !ok || strings.TrimSpace(data) == "[DONE]" {
								continue
							}
							var chunk dto.ChatCompletionsStreamResponse
							require.NoError(t, common.UnmarshalJsonStr(data, &chunk))
							for _, choice := range chunk.Choices {
								content.WriteString(choice.Delta.GetContentString())
								for _, call := range choice.Delta.ToolCalls {
									toolName.WriteString(call.Function.Name)
								}
							}
						}
					} else {
						require.Equal(t, "application/json", w.Header().Get("Content-Type"))
						require.NotContains(t, w.Body.String(), "data:")
						t.Logf("aggregated downstream JSON\n%s", strings.ReplaceAll(w.Body.String(), key, "[REDACTED]"))
						var result dto.OpenAITextResponse
						require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
						require.NotEmpty(t, result.Choices)
						content.WriteString(result.Choices[0].Message.StringContent())
						for _, call := range result.Choices[0].Message.ParseToolCalls() {
							toolName.WriteString(call.Function.Name)
						}
					}
					if useTools {
						require.Equal(t, "lookup", toolName.String())
					} else {
						require.Equal(t, "OK", strings.TrimSpace(content.String()))
					}
					t.Logf("model=%s stream=%t tools=%t prompt_tokens=%d completion_tokens=%d", modelID, stream, useTools, usage.(*dto.Usage).PromptTokens, usage.(*dto.Usage).CompletionTokens)
				})
			}
		}
	}
	require.True(t, matched, "CLINE_LIVE_MODEL must belong to the current free catalog")
}

type clineRecordingBody struct {
	io.Reader
	io.Closer
}
