package mistralconsole

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Replay the captured conversation protocol for every advertised model and
// both downstream modes. Only Large 4 was captured; the other models reuse
// that fixture to verify routing, not to assert live upstream availability.
func TestPlaygroundModelsRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	fixture, err := os.ReadFile("testdata/playground.sse")
	require.NoError(t, err)
	for _, model := range ModelList {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", model, stream), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, conversationsURL, r.URL.Path)
					require.Equal(t, `ory_session_test="session"`, r.Header.Get("Cookie"))
					require.Empty(t, r.Header.Get("Authorization"))
					require.Equal(t, "text/event-stream", r.Header.Get("Accept"))
					require.Equal(t, "playground", r.Header.Get("Internal-Source"))
					require.JSONEq(t, `{"call_type":"agent_playground"}`, r.Header.Get("X-Metadata"))
					require.Equal(t, "http://"+r.Host+"/playground", r.Header.Get("Referer"))
					body, readErr := io.ReadAll(r.Body)
					require.NoError(t, readErr)
					// The UI's optional conversation name is not needed by the relay.
					require.JSONEq(t, fmt.Sprintf(`{
						"model":%q,"instructions":"",
						"completion_args":{"temperature":0.7,"max_tokens":2048,"top_p":1},
						"tools":[],"stream":true,
						"inputs":[{"object":"entry","type":"message.input","role":"user","content":"你好","prefix":false}]
					}`, model), string(body))
					w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
					_, _ = io.WriteString(w, strings.ReplaceAll(string(fixture), "mistral-large-4", model))
				}))
				defer upstream.Close()

				info := testRelayInfo(stream)
				info.UpstreamModelName = model
				info.ChannelBaseUrl = upstream.URL + "/"
				info.ShouldIncludeUsage = true
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				adaptor := &Adaptor{}
				adaptor.Init(info)
				converted, err := adaptor.ConvertOpenAIRequest(ctx, info, &dto.GeneralOpenAIRequest{
					Model: "client-alias", Stream: &stream,
					Temperature: common.GetPointer(0.7), TopP: common.GetPointer(1.0),
					MaxTokens: common.GetPointer(uint(2048)),
					Messages:  []dto.Message{{Role: "user", Content: "你好"}},
				})
				require.NoError(t, err)
				body, err := common.Marshal(converted)
				require.NoError(t, err)
				result, err := adaptor.DoRequest(ctx, info, bytes.NewReader(body))
				require.NoError(t, err)
				resp := result.(*http.Response)
				require.Equal(t, http.StatusOK, resp.StatusCode)
				info.IsStream = true // Relay detects the upstream SSE content type.
				usage, apiErr := adaptor.DoResponse(ctx, resp, info)
				require.Nil(t, apiErr)
				require.Equal(t, stream, info.IsStream)
				require.Equal(t, 4, usage.(*dto.Usage).PromptTokens)
				require.Equal(t, 16, usage.(*dto.Usage).CompletionTokens)
				require.Equal(t, 20, usage.(*dto.Usage).TotalTokens)
				expected := "你好！很高兴见到你。有什么我可以帮你的吗？"
				if !stream {
					var response dto.OpenAITextResponse
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
					require.Equal(t, model, response.Model)
					require.Equal(t, expected, response.Choices[0].Message.StringContent())
					require.Equal(t, "stop", response.Choices[0].FinishReason)
					require.Equal(t, 20, response.Usage.TotalTokens)
					return
				}
				lines := streamDataLines(recorder.Body.String())
				require.Equal(t, "[DONE]", lines[len(lines)-1])
				var output strings.Builder
				var usageSeen, stopSeen bool
				for _, line := range lines[:len(lines)-1] {
					var chunk dto.ChatCompletionsStreamResponse
					require.NoError(t, common.Unmarshal([]byte(line), &chunk))
					require.Equal(t, model, chunk.Model)
					for _, choice := range chunk.Choices {
						output.WriteString(choice.Delta.GetContentString())
						if choice.FinishReason != nil {
							require.Equal(t, "stop", *choice.FinishReason)
							stopSeen = true
						}
					}
					if chunk.Usage != nil {
						require.Equal(t, 20, chunk.Usage.TotalTokens)
						usageSeen = true
					}
				}
				require.Equal(t, expected, output.String())
				require.True(t, stopSeen)
				require.True(t, usageSeen)
			})
		}
	}
}

func TestPlaygroundExplicitZeroAndToolChoiceNone(t *testing.T) {
	info := testRelayInfo(false)
	info.UpstreamModelName = "mistral-medium-latest"
	info.ChannelOtherSettings = dto.ChannelOtherSettings{
		MistralConsoleCodeInterpreterEnabled: common.GetPointer(true),
		MistralConsoleImageGenerationEnabled: common.GetPointer(true),
		MistralConsoleWebSearchEnabled:       common.GetPointer(true),
	}
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"messages":[{"role":"user","content":"hi"}],
		"temperature":0,"max_tokens":100,"max_completion_tokens":0,"tool_choice":"none"
	}`), &request))
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, &request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Contains(t, string(body), `"temperature":0`)
	require.Contains(t, string(body), `"max_tokens":0`)
	require.Contains(t, string(body), `"tools":[]`)
	require.NotContains(t, string(body), "reasoning_effort")
}

func TestPlaygroundOmittedCompletionArgs(t *testing.T) {
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, testRelayInfo(false), &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Contains(t, string(body), `"completion_args":{}`)
	require.Contains(t, string(body), `"tools":[]`)
}

func TestPlaygroundBuiltinToolsRespectModelCapabilities(t *testing.T) {
	for _, model := range ModelList {
		t.Run(model, func(t *testing.T) {
			info := testRelayInfo(false)
			info.UpstreamModelName = model
			info.ChannelOtherSettings = dto.ChannelOtherSettings{
				MistralConsoleCodeInterpreterEnabled: common.GetPointer(true),
				MistralConsoleImageGenerationEnabled: common.GetPointer(true),
				MistralConsoleWebSearchEnabled:       common.GetPointer(true),
			}
			adaptor := &Adaptor{}
			converted, err := adaptor.ConvertOpenAIRequest(nil, info, &dto.GeneralOpenAIRequest{
				Messages: []dto.Message{{Role: "user", Content: "hi"}},
				Tools: []dto.ToolCallRequest{
					{Type: "web_search"},
					{Type: "code_interpreter"},
					{Type: "image_generation"},
					{Type: "function", Function: dto.FunctionRequest{Name: "get_time"}},
				},
			})
			require.NoError(t, err)
			payload := converted.(*boraConversationRequest)
			var toolTypes []string
			for _, tool := range payload.Tools {
				toolTypes = append(toolTypes, tool.Type)
			}
			if model == "mistral-medium-latest" || model == "mistral-small-latest" {
				require.Equal(t, []string{"code_interpreter", "image_generation", "web_search_premium", "function"}, toolTypes)
			} else {
				require.Equal(t, []string{"function"}, toolTypes)
			}
			function := payload.Tools[len(payload.Tools)-1].Function
			require.Equal(t, "get_time", adaptor.restoreFunctionName(function.Name))
			require.True(t, info.ChannelOtherSettings.ShouldEnableMistralConsoleCodeInterpreter())
			require.True(t, info.ChannelOtherSettings.ShouldEnableMistralConsoleImageGeneration())
			require.True(t, info.ChannelOtherSettings.ShouldEnableMistralConsoleWebSearch())
		})
	}
}
