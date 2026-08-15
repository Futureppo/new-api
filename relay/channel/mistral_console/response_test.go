package mistralconsole

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const testBoraSSE = `event: response
data: {"type":"conversation.response.started","conversation_id":"conv-123"}

: keepalive

event: message
data: {"type":"future.event","value":true}

event: message
data: {"type":"message.output.delta",
data: "content":"Hello "}

event: message
data: {"type":"message.output.delta","content":"world"}

event: response
data: {"type":"conversation.response.done","usage":{"input_tokens":7,"output_tokens":2,"total_tokens":9}}

`

func TestHandleBoraStreamResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(true)
	info.ShouldIncludeUsage = true
	adaptor := &Adaptor{}
	adaptor.Init(info)
	resp := boraHTTPResponse(testBoraSSE)
	initialFirstResponseTime := info.FirstResponseTime

	usageAny, apiErr := adaptor.DoResponse(ctx, resp, info)
	require.Nil(t, apiErr)
	usage, ok := usageAny.(*dto.Usage)
	require.True(t, ok)
	require.Equal(t, 7, usage.PromptTokens)
	require.Equal(t, 2, usage.CompletionTokens)
	require.Equal(t, 9, usage.TotalTokens)
	require.True(t, info.IsStream)
	require.True(t, info.FirstResponseTime.After(initialFirstResponseTime))
	require.GreaterOrEqual(t, info.FirstResponseTime.UnixNano(), info.StartTime.UnixNano())
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))

	data := streamDataLines(recorder.Body.String())
	require.Len(t, data, 6)
	require.Equal(t, "[DONE]", data[5])

	var start dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[0]), &start))
	require.Equal(t, "chatcmpl-conv-123", start.Id)
	require.Equal(t, "assistant", start.Choices[0].Delta.Role)

	var firstDelta dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[1]), &firstDelta))
	require.Equal(t, "Hello ", firstDelta.Choices[0].Delta.GetContentString())

	var stop dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[3]), &stop))
	require.Equal(t, "stop", *stop.Choices[0].FinishReason)

	var usageChunk dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[4]), &usageChunk))
	require.Empty(t, usageChunk.Choices)
	require.Equal(t, 9, usageChunk.Usage.TotalTokens)
}

func TestHandleBoraNonStreamResponseRestoresClientMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(false)
	adaptor := &Adaptor{}
	adaptor.Init(info)
	initialFirstResponseTime := info.FirstResponseTime
	// Simulate the relay detecting the upstream text/event-stream Content-Type.
	info.IsStream = true

	usageAny, apiErr := adaptor.DoResponse(ctx, boraHTTPResponse(testBoraSSE), info)
	require.Nil(t, apiErr)
	usage, ok := usageAny.(*dto.Usage)
	require.True(t, ok)
	require.Equal(t, 9, usage.TotalTokens)
	require.False(t, info.IsStream)
	require.True(t, info.FirstResponseTime.After(initialFirstResponseTime))
	require.GreaterOrEqual(t, info.FirstResponseTime.UnixNano(), info.StartTime.UnixNano())
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var response dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "chatcmpl-conv-123", response.Id)
	require.Equal(t, "chat.completion", response.Object)
	require.Equal(t, "glm-5-2", response.Model)
	require.Equal(t, "Hello world", response.Choices[0].Message.StringContent())
	require.Equal(t, "stop", response.Choices[0].FinishReason)
	require.Equal(t, 7, response.Usage.PromptTokens)
}

func TestHandleBoraStreamResponseOmitsUsageChunkWhenNotRequested(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(true)
	info.ShouldIncludeUsage = false
	adaptor := &Adaptor{}
	adaptor.Init(info)

	usageAny, apiErr := adaptor.DoResponse(ctx, boraHTTPResponse(testBoraSSE), info)
	require.Nil(t, apiErr)
	require.Equal(t, 9, usageAny.(*dto.Usage).TotalTokens)

	data := streamDataLines(recorder.Body.String())
	require.Len(t, data, 5)
	require.Equal(t, "[DONE]", data[4])
	for _, item := range data[:4] {
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.Unmarshal([]byte(item), &chunk))
		require.Nil(t, chunk.Usage)
	}
}

func TestHandleBoraResponseUsesPromptCompletionUsageAliases(t *testing.T) {
	sse := `data: {"type":"conversation.response.started","conversation_id":"conv-alias"}

data: {"type":"message.output.delta","content":"ok"}

data: {"type":"conversation.response.done","usage":{"prompt_tokens":11,"completion_tokens":3}}

`
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(false)

	usage, apiErr := handleBoraResponse(ctx, boraHTTPResponse(sse), info)
	require.Nil(t, apiErr)
	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 14, usage.TotalTokens)
}

func TestHandleBoraStreamResponseMapsThinkingAndFunctionCalls(t *testing.T) {
	sse := `data: {"type":"conversation.response.started","conversation_id":"conv-tools"}

data: {"type":"message.output.delta","content":{"type":"thinking","thinking":[{"type":"text","text":"Need the current time. "}],"closed":true}}

data: {"type":"function.call.delta","output_index":0,"id":"fc-1","name":"get_time","tool_call_id":"call-1","arguments":"{\"timezone\":\"Asia"}

data: {"type":"function.call.delta","output_index":0,"id":"fc-1","name":"get_time","tool_call_id":"call-1","arguments":"/Shanghai\"}"}

data: {"type":"conversation.response.done","usage":{"prompt_tokens":25,"completion_tokens":9,"total_tokens":34,"connectors":{"web_search_premium":1}}}

`
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(true)
	info.ShouldIncludeUsage = true

	usage, apiErr := handleBoraStreamResponse(ctx, boraHTTPResponse(sse), info)
	require.Nil(t, apiErr)
	require.Equal(t, 34, usage.TotalTokens)

	data := streamDataLines(recorder.Body.String())
	require.Len(t, data, 7)
	require.Equal(t, "[DONE]", data[6])

	var reasoning dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[1]), &reasoning))
	require.Equal(t, "Need the current time. ", reasoning.Choices[0].Delta.GetReasoningContent())

	var firstCall dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[2]), &firstCall))
	require.Len(t, firstCall.Choices[0].Delta.ToolCalls, 1)
	require.Equal(t, "call-1", firstCall.Choices[0].Delta.ToolCalls[0].ID)
	require.Equal(t, 0, *firstCall.Choices[0].Delta.ToolCalls[0].Index)
	require.Equal(t, "get_time", firstCall.Choices[0].Delta.ToolCalls[0].Function.Name)
	require.Equal(t, `{"timezone":"Asia`, firstCall.Choices[0].Delta.ToolCalls[0].Function.Arguments)

	var secondCall dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[3]), &secondCall))
	require.Equal(t, `/Shanghai"}`, secondCall.Choices[0].Delta.ToolCalls[0].Function.Arguments)

	var stop dto.ChatCompletionsStreamResponse
	require.NoError(t, common.Unmarshal([]byte(data[4]), &stop))
	require.Equal(t, "tool_calls", *stop.Choices[0].FinishReason)
}

func TestHandleBoraNonStreamResponseAggregatesThinkingAndFunctionCalls(t *testing.T) {
	sse := `data: {"type":"conversation.response.started","conversation_id":"conv-function"}

data: {"type":"message.output.delta","content":{"type":"thinking","thinking":[{"type":"text","text":"Calling tool"}],"closed":true}}

data: {"type":"function.call.delta","id":"fc-1","name":"get_time","tool_call_id":"call-1","arguments":"{\"timezone\":"}

data: {"type":"function.call.delta","id":"fc-1","name":"get_time","tool_call_id":"call-1","arguments":"\"Asia/Shanghai\"}"}

data: {"type":"conversation.response.done","usage":{"input_tokens":15,"output_tokens":8}}

`
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	usage, apiErr := handleBoraResponse(ctx, boraHTTPResponse(sse), testRelayInfo(false))
	require.Nil(t, apiErr)
	require.Equal(t, 23, usage.TotalTokens)

	var response dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "tool_calls", response.Choices[0].FinishReason)
	require.Equal(t, "Calling tool", response.Choices[0].Message.ReasoningContent)
	require.Empty(t, response.Choices[0].Message.StringContent())
	toolCalls := response.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call-1", toolCalls[0].ID)
	require.Equal(t, "get_time", toolCalls[0].Function.Name)
	require.JSONEq(t, `{"timezone":"Asia/Shanghai"}`, toolCalls[0].Function.Arguments)
}

func TestHandleBoraResponseExposesGeneratedImageURL(t *testing.T) {
	sse := `data: {"type":"conversation.response.started","conversation_id":"conv-image"}

data: {"type":"tool.execution.started","name":"image_generation","function":"generate_image"}

data: {"type":"tool.execution.done","name":"image_generation","function":"generate_image","info":{"result":"{\"url\":\"https://images.example.com/generated.jpg?sig=test\"}"}}

data: {"type":"message.output.delta","content":{"type":"tool_file","tool":"image_generation","file_id":"file-1","file_name":"generated.jpg","file_type":"jpg"}}

data: {"type":"message.output.delta","content":"Done."}

data: {"type":"conversation.response.done","usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"connectors":{"image_generation":1}}}

`
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	usage, apiErr := handleBoraResponse(ctx, boraHTTPResponse(sse), testRelayInfo(false))
	require.Nil(t, apiErr)
	require.Equal(t, 14, usage.TotalTokens)

	var response dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "![Generated image](https://images.example.com/generated.jpg?sig=test)\n\nDone.", response.Choices[0].Message.StringContent())
	require.Equal(t, "stop", response.Choices[0].FinishReason)
}

func TestHandleBoraResponseFallsBackToEstimatedUsage(t *testing.T) {
	sse := `data: {"type":"conversation.response.started","conversation_id":"conv-estimated"}

data: {"type":"message.output.delta","content":"estimated response"}

data: {"type":"conversation.response.done"}

`
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(false)
	info.SetEstimatePromptTokens(5)

	usage, apiErr := handleBoraResponse(ctx, boraHTTPResponse(sse), info)
	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.PromptTokens)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.PromptTokens+usage.CompletionTokens, usage.TotalTokens)
}

func TestHandleBoraResponseRejectsInvalidOrIncompleteSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		sse  string
	}{
		{name: "invalid JSON", sse: "data: {not-json}\n\n"},
		{name: "missing done", sse: `data: {"type":"message.output.delta","content":"partial"}` + "\n\n"},
		{name: "error event", sse: `data: {"type":"conversation.response.error","message":"session expired"}` + "\n\n"},
		{name: "unknown output content", sse: `data: {"type":"message.output.delta","content":{"type":"audio"}}` + "\n\n"},
		{name: "function call missing id", sse: `data: {"type":"function.call.delta","name":"get_time","arguments":"{}"}` + "\n\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			_, apiErr := handleBoraResponse(ctx, boraHTTPResponse(test.sse), testRelayInfo(false))
			require.NotNil(t, apiErr)
			require.NotContains(t, apiErr.Error(), ChannelName)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
		})
	}
}

func boraHTTPResponse(sse string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
}

func streamDataLines(body string) []string {
	lines := strings.Split(body, "\n")
	data := make([]string, 0)
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			data = append(data, strings.TrimPrefix(line, "data: "))
		}
	}
	return data
}
