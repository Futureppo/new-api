package mistralconsole

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const maxSSEEventSize = 64 << 20

type boraResponseState struct {
	id              string
	created         int64
	model           string
	text            strings.Builder
	reasoning       strings.Builder
	toolCalls       []dto.ToolCallResponse
	toolCallIndexes map[string]int
	usage           *boraUsage
	completed       bool
	startEmitted    bool
}

type boraEventOutput struct {
	content   string
	reasoning string
	toolCall  *dto.ToolCallResponse
}

func handleBoraStreamResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	state := newBoraResponseState(c, info)
	helper.SetEventStreamHeaders(c)

	err := consumeBoraSSE(resp, func(eventName string, event boraStreamEvent) error {
		return state.handleStreamEvent(c, eventName, event)
	})
	if err != nil {
		return nil, badResponseError(err)
	}
	if !state.completed {
		return nil, badResponseError(errors.New("upstream stream ended before the completion event"))
	}

	usage := state.finalUsage(c, info)
	if !state.startEmitted {
		if err := helper.ObjectData(c, helper.GenerateStartEmptyResponse(state.id, state.created, state.model, nil)); err != nil {
			return nil, badResponseError(err)
		}
		state.startEmitted = true
	}
	stop := helper.GenerateStopResponse(state.id, state.created, state.model, state.finishReason())
	if err := helper.ObjectData(c, stop); err != nil {
		return nil, badResponseError(err)
	}
	if info.ShouldIncludeUsage {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(state.id, state.created, state.model, *usage)); err != nil {
			return nil, badResponseError(err)
		}
	}
	helper.Done(c)
	return usage, nil
}

func handleBoraResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	state := newBoraResponseState(c, info)
	err := consumeBoraSSE(resp, func(eventName string, event boraStreamEvent) error {
		_, err := state.handleEvent(eventName, event)
		return err
	})
	if err != nil {
		return nil, badResponseError(err)
	}
	if !state.completed {
		return nil, badResponseError(errors.New("upstream stream ended before the completion event"))
	}

	usage := state.finalUsage(c, info)
	message := dto.Message{
		Role:             "assistant",
		Content:          state.text.String(),
		ReasoningContent: state.reasoning.String(),
	}
	if len(state.toolCalls) > 0 {
		message.SetToolCalls(state.nonStreamToolCalls())
	}
	response := dto.OpenAITextResponse{
		Id:      state.id,
		Object:  "chat.completion",
		Created: state.created,
		Model:   state.model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: state.finishReason(),
			},
		},
		Usage: *usage,
	}
	data, err := common.Marshal(response)
	if err != nil {
		return nil, badResponseError(err)
	}
	relaycommon.AppendConversationClientResponse(c, data)
	c.Data(http.StatusOK, "application/json", data)
	return usage, nil
}

func newBoraResponseState(c *gin.Context, info *relaycommon.RelayInfo) *boraResponseState {
	return &boraResponseState{
		id:              helper.GetResponseID(c),
		created:         common.GetTimestamp(),
		model:           info.UpstreamModelName,
		toolCallIndexes: make(map[string]int),
	}
}

func (state *boraResponseState) handleStreamEvent(c *gin.Context, eventName string, event boraStreamEvent) error {
	output, err := state.handleEvent(eventName, event)
	if err != nil {
		return err
	}
	eventType := boraEventType(eventName, event)
	if !state.startEmitted && (eventType == "conversation.response.started" || output.hasOutput()) {
		if err := helper.ObjectData(c, helper.GenerateStartEmptyResponse(state.id, state.created, state.model, nil)); err != nil {
			return err
		}
		state.startEmitted = true
	}
	if output.reasoning != "" {
		chunk := state.newStreamChunk()
		chunk.Choices[0].Delta.SetReasoningContent(output.reasoning)
		if err := helper.ObjectData(c, chunk); err != nil {
			return err
		}
	}
	if output.content != "" {
		chunk := state.newStreamChunk()
		chunk.Choices[0].Delta.SetContentString(output.content)
		if err := helper.ObjectData(c, chunk); err != nil {
			return err
		}
	}
	if output.toolCall != nil {
		chunk := state.newStreamChunk()
		chunk.Choices[0].Delta.ToolCalls = []dto.ToolCallResponse{*output.toolCall}
		if err := helper.ObjectData(c, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (state *boraResponseState) handleEvent(eventName string, event boraStreamEvent) (boraEventOutput, error) {
	var output boraEventOutput
	eventType := boraEventType(eventName, event)
	if isBoraErrorEvent(eventType) {
		return output, fmt.Errorf("upstream error event: %s", boraErrorMessage(event))
	}

	switch eventType {
	case "conversation.response.started":
		if event.ConversationID != "" && !state.startEmitted {
			state.id = boraResponseID(event.ConversationID)
		}
	case "message.output.delta":
		content, reasoning, err := parseBoraMessageContent(event.Content)
		if err != nil {
			return output, err
		}
		state.text.WriteString(content)
		state.reasoning.WriteString(reasoning)
		output.content = content
		output.reasoning = reasoning
	case "function.call.delta":
		toolCall, err := state.appendFunctionCall(event)
		if err != nil {
			return output, err
		}
		output.toolCall = toolCall
	case "tool.execution.done":
		if event.Name == "image_generation" {
			content, err := boraImageMarkdown(event.Info)
			if err != nil {
				return output, err
			}
			state.text.WriteString(content)
			output.content = content
		}
	case "conversation.response.done":
		state.completed = true
		state.usage = event.Usage
	}
	return output, nil
}

func (output boraEventOutput) hasOutput() bool {
	return output.content != "" || output.reasoning != "" || output.toolCall != nil
}

func (state *boraResponseState) newStreamChunk() dto.ChatCompletionsStreamResponse {
	return dto.ChatCompletionsStreamResponse{
		Id:      state.id,
		Object:  "chat.completion.chunk",
		Created: state.created,
		Model:   state.model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0}},
	}
}

func (state *boraResponseState) appendFunctionCall(event boraStreamEvent) (*dto.ToolCallResponse, error) {
	toolCallID := strings.TrimSpace(event.ToolCallID)
	if toolCallID == "" {
		toolCallID = strings.TrimSpace(event.ID)
	}
	if toolCallID == "" {
		return nil, errors.New("upstream function call is missing tool_call_id")
	}

	index, exists := state.toolCallIndexes[toolCallID]
	if !exists {
		if strings.TrimSpace(event.Name) == "" {
			return nil, errors.New("upstream function call is missing function name")
		}
		index = len(state.toolCalls)
		state.toolCallIndexes[toolCallID] = index
		state.toolCalls = append(state.toolCalls, dto.ToolCallResponse{
			ID:   toolCallID,
			Type: "function",
			Function: dto.FunctionResponse{
				Name: event.Name,
			},
		})
	}
	call := &state.toolCalls[index]
	if event.Name != "" {
		call.Function.Name = event.Name
	}
	call.Function.Arguments += event.Arguments

	streamIndex := index
	return &dto.ToolCallResponse{
		Index: &streamIndex,
		ID:    toolCallID,
		Type:  "function",
		Function: dto.FunctionResponse{
			Name:      call.Function.Name,
			Arguments: event.Arguments,
		},
	}, nil
}

func (state *boraResponseState) nonStreamToolCalls() []dto.ToolCallResponse {
	toolCalls := make([]dto.ToolCallResponse, len(state.toolCalls))
	copy(toolCalls, state.toolCalls)
	for index := range toolCalls {
		toolCalls[index].Index = nil
	}
	return toolCalls
}

func (state *boraResponseState) finishReason() string {
	if len(state.toolCalls) > 0 {
		return constant.FinishReasonToolCalls
	}
	return constant.FinishReasonStop
}

func (state *boraResponseState) finalUsage(c *gin.Context, info *relaycommon.RelayInfo) *dto.Usage {
	usage := &dto.Usage{}
	if state.usage != nil {
		usage.PromptTokens = state.usage.PromptTokens
		if usage.PromptTokens == 0 {
			usage.PromptTokens = state.usage.InputTokens
		}
		usage.CompletionTokens = state.usage.CompletionTokens
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = state.usage.OutputTokens
		}
		usage.TotalTokens = state.usage.TotalTokens
	}
	if usage.PromptTokens == 0 || usage.CompletionTokens == 0 {
		fallback := service.ResponseText2Usage(c, state.outputForUsage(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		if usage.PromptTokens == 0 {
			usage.PromptTokens = fallback.PromptTokens
		}
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = fallback.CompletionTokens
		}
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}

func (state *boraResponseState) outputForUsage() string {
	var output strings.Builder
	output.WriteString(state.reasoning.String())
	output.WriteString(state.text.String())
	for _, toolCall := range state.toolCalls {
		output.WriteString(toolCall.Function.Name)
		output.WriteString(toolCall.Function.Arguments)
	}
	return output.String()
}

func parseBoraMessageContent(raw []byte) (string, string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", "", nil
	}

	var content string
	if err := common.Unmarshal(trimmed, &content); err == nil {
		return content, "", nil
	}

	var thinking boraThinkingContent
	if err := common.Unmarshal(trimmed, &thinking); err != nil {
		return "", "", fmt.Errorf("invalid upstream message content: %w", err)
	}
	// image_generation also emits a tool_file metadata item. The downloadable
	// URL is carried by tool.execution.done and is rendered there, so this
	// duplicate file marker must not become an upstream parsing failure.
	if thinking.Type == "tool_file" {
		return "", "", nil
	}
	if thinking.Type != "thinking" {
		return "", "", fmt.Errorf("unsupported upstream message content type %q", thinking.Type)
	}
	var reasoning strings.Builder
	for index, item := range thinking.Thinking {
		if item.Type != "text" {
			return "", "", fmt.Errorf("unsupported upstream reasoning item %d type %q", index, item.Type)
		}
		reasoning.WriteString(item.Text)
	}
	return "", reasoning.String(), nil
}

func boraImageMarkdown(info *boraToolExecutionInfo) (string, error) {
	if info == nil || strings.TrimSpace(info.Result) == "" {
		return "", nil
	}
	var result boraImageResult
	if err := common.Unmarshal([]byte(info.Result), &result); err != nil {
		return "", fmt.Errorf("invalid upstream image generation result: %w", err)
	}
	parsed, err := url.Parse(strings.TrimSpace(result.URL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("upstream image generation returned an invalid URL")
	}
	imageURL := strings.ReplaceAll(parsed.String(), ")", "%29")
	return "![Generated image](" + imageURL + ")\n\n", nil
}

func consumeBoraSSE(resp *http.Response, handle func(eventName string, event boraStreamEvent) error) error {
	if resp == nil || resp.Body == nil {
		return errors.New("upstream response body is empty")
	}
	defer service.CloseResponseBodyGracefully(resp)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), maxSSEEventSize)
	var eventName string
	var dataLines []string
	dispatch := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		currentEvent := eventName
		eventName = ""
		var event boraStreamEvent
		if err := common.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("invalid upstream event data: %w", err)
		}
		return handle(currentEvent, event)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read upstream event stream: %w", err)
	}
	return dispatch()
}

func boraEventType(eventName string, event boraStreamEvent) string {
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		eventType = strings.TrimSpace(eventName)
	}
	return eventType
}

func boraResponseID(conversationID string) string {
	if strings.HasPrefix(conversationID, "chatcmpl-") {
		return conversationID
	}
	return "chatcmpl-" + conversationID
}

func isBoraErrorEvent(eventType string) bool {
	lower := strings.ToLower(eventType)
	return lower == "error" || strings.HasSuffix(lower, ".error") || strings.HasSuffix(lower, ".failed")
}

func boraErrorMessage(event boraStreamEvent) string {
	if strings.TrimSpace(event.Message) != "" {
		return event.Message
	}
	if event.Error != nil {
		return fmt.Sprintf("%v", event.Error)
	}
	return "unknown upstream error"
}

func badResponseError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}
