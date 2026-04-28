package cohere

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closeNotify chan bool
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.closeNotify
}

func TestRequestOpenAI2CohereV2PreservesExplicitZeroAndContent(t *testing.T) {
	stream := true
	maxTokens := uint(0)
	temperature := 0.2
	topP := 0.7
	topK := 4
	seed := 123.0
	toolCallsRaw, err := common.Marshal([]dto.ToolCallRequest{
		{
			ID:   "call_1",
			Type: "function",
			Function: dto.FunctionRequest{
				Name:      "lookup",
				Arguments: `{"q":"cohere"}`,
			},
		},
	})
	require.NoError(t, err)

	request := dto.GeneralOpenAIRequest{
		Model:       "command-a-03-2025",
		Stream:      &stream,
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
		TopP:        &topP,
		TopK:        &topK,
		Seed:        &seed,
		Stop:        []any{"END"},
		ToolChoice:  "required",
		Messages: []dto.Message{
			{Role: "system", Content: "system prompt"},
			{
				Role: "user",
				Content: []any{
					map[string]any{"type": "text", "text": "describe"},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url":    "https://example.com/image.png",
							"detail": "high",
						},
					},
				},
			},
			{Role: "assistant", ToolCalls: toolCallsRaw},
			{Role: "tool", ToolCallId: "call_1", Content: `{"ok":true}`},
		},
	}

	converted, err := requestOpenAI2Cohere(request)
	require.NoError(t, err)
	require.Equal(t, "command-a-03-2025", converted.Model)
	require.True(t, converted.Stream)
	require.NotNil(t, converted.MaxTokens)
	require.Equal(t, uint(0), *converted.MaxTokens)
	require.Equal(t, "REQUIRED", converted.ToolChoice)
	require.Equal(t, []string{"END"}, converted.StopSequences)
	require.Len(t, converted.Messages, 4)

	userBlocks, ok := converted.Messages[1].Content.([]CohereContentBlock)
	require.True(t, ok)
	require.Equal(t, "text", userBlocks[0].Type)
	require.Equal(t, "image_url", userBlocks[1].Type)
	require.Equal(t, "https://example.com/image.png", userBlocks[1].ImageURL.URL)
	require.Equal(t, "high", userBlocks[1].ImageURL.Detail)
	require.Equal(t, "lookup", converted.Messages[2].ToolCalls[0].Function.Name)
	require.Equal(t, "document", converted.Messages[3].Content.([]CohereContentBlock)[0].Type)

	body, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Contains(t, string(body), `"max_tokens":0`)
}

func TestCohereHandlerMapsTextToolCallsAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "command-a-03-2025",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "command-a-03-2025",
		},
	}

	payload := `{
		"id":"chat_123",
		"finish_reason":"TOOL_CALL",
		"message":{
			"role":"assistant",
			"content":[{"type":"text","text":"calling"}],
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}]
		},
		"usage":{
			"billed_units":{"input_tokens":1,"output_tokens":1},
			"tokens":{"input_tokens":11,"output_tokens":3}
		}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(payload)),
	}

	usage, apiErr := cohereHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":11`)
}

func TestCohereStreamHandlerMapsContentToolCallsAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closeNotify:      make(chan bool),
	}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:    "command-a-03-2025",
		ShouldIncludeUsage: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "command-a-03-2025",
		},
	}

	stream := strings.Join([]string{
		`data: {"type":"message-start","id":"chat_1","delta":{"message":{"role":"assistant"}}}`,
		`data: {"type":"content-delta","delta":{"message":{"content":{"text":"Hi"}}}}`,
		`data: {"type":"tool-call-start","index":0,"delta":{"message":{"tool_calls":{"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}}}}`,
		`data: {"type":"tool-call-delta","index":0,"delta":{"message":{"tool_calls":{"function":{"arguments":"{}"}}}}}`,
		`data: {"type":"message-end","delta":{"finish_reason":"TOOL_CALL","usage":{"tokens":{"input_tokens":7,"output_tokens":4}}}}`,
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	usage, apiErr := cohereStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Equal(t, 7, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Contains(t, recorder.Body.String(), `"content":"Hi"`)
	require.Contains(t, recorder.Body.String(), `"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"finish_reason":"tool_calls"`)
	require.Contains(t, recorder.Body.String(), `"usage":{"prompt_tokens":7`)
	require.Contains(t, recorder.Body.String(), "data: [DONE]")
}

func TestCohereRerankHandlerReturnsDocumentsFromOriginalRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/rerank", nil)

	info := &relaycommon.RelayInfo{
		RerankerInfo: &relaycommon.RerankerInfo{
			Documents:       []any{"doc-a", "doc-b"},
			ReturnDocuments: true,
		},
	}
	info.SetEstimatePromptTokens(5)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"results":[{"index":1,"relevance_score":0.9}],
			"meta":{"billed_units":{"search_units":1}}
		}`)),
	}

	usage, apiErr := cohereRerankHandler(c, resp, info)
	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.PromptTokens)
	require.Contains(t, recorder.Body.String(), `"document":"doc-b"`)
}

func TestCohereEmbeddingRequiresInputTypeAndMapsResponse(t *testing.T) {
	_, err := requestConvertEmbedding2Cohere(dto.EmbeddingRequest{
		Model: "embed-v4.0",
		Input: []any{"hello"},
	})
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)

	dimensions := 256
	converted, err := requestConvertEmbedding2Cohere(dto.EmbeddingRequest{
		Model:      "embed-v4.0",
		Input:      []any{"hello", "world"},
		InputType:  "search_document",
		Dimensions: &dimensions,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"float"}, converted.EmbeddingTypes)
	require.Equal(t, &dimensions, converted.OutputDimension)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "embed-v4.0",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "embed-v4.0",
		},
	}

	body := []byte(`{
		"id":"emb_1",
		"embeddings":{"float":[[0.1,0.2],[0.3,0.4]]},
		"meta":{"billed_units":{"input_tokens":6}}
	}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	usage, apiErr := cohereEmbeddingHandler(c, resp, info)
	require.Nil(t, apiErr)
	require.Equal(t, 6, usage.PromptTokens)
	require.Contains(t, recorder.Body.String(), `"object":"list"`)
	require.Contains(t, recorder.Body.String(), `"embedding":[0.1,0.2]`)
}
