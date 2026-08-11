package mistral

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) *dto.GeneralOpenAIRequest {
	messages := make([]dto.Message, 0, len(request.Messages))
	idMap := make(map[string]string)
	for _, message := range request.Messages {
		// 1. tool_calls.id
		toolCalls := message.ParseToolCalls()
		if toolCalls != nil {
			for i := range toolCalls {
				if !mistralToolCallIdRegexp.MatchString(toolCalls[i].ID) {
					if newId, ok := idMap[toolCalls[i].ID]; ok {
						toolCalls[i].ID = newId
					} else {
						newId, err := common.GenerateRandomCharsKey(9)
						if err == nil {
							idMap[toolCalls[i].ID] = newId
							toolCalls[i].ID = newId
						}
					}
				}
			}
			message.SetToolCalls(toolCalls)
		}

		// 2. tool_call_id
		if message.ToolCallId != "" {
			if newId, ok := idMap[message.ToolCallId]; ok {
				message.ToolCallId = newId
			} else {
				if !mistralToolCallIdRegexp.MatchString(message.ToolCallId) {
					newId, err := common.GenerateRandomCharsKey(9)
					if err == nil {
						idMap[message.ToolCallId] = newId
						message.ToolCallId = newId
					}
				}
			}
		}

		mediaMessages := message.ParseContent()
		if message.Role == "assistant" && message.ToolCalls != nil && message.Content == "" {
			mediaMessages = []dto.MediaContent{}
		}
		for j, mediaMessage := range mediaMessages {
			if mediaMessage.Type == dto.ContentTypeImageURL {
				imageUrl := mediaMessage.GetImageMedia()
				mediaMessage.ImageUrl = imageUrl.Url
				mediaMessages[j] = mediaMessage
			}
		}
		message.SetMediaContent(mediaMessages)
		messages = append(messages, dto.Message{
			Role:       message.Role,
			Content:    message.Content,
			ToolCalls:  message.ToolCalls,
			ToolCallId: message.ToolCallId,
		})
	}
	out := &dto.GeneralOpenAIRequest{
		Model:           request.Model,
		Stream:          request.Stream,
		Messages:        messages,
		ReasoningEffort: request.ReasoningEffort,
		Temperature:     request.Temperature,
		TopP:            request.TopP,
		Tools:           request.Tools,
		ToolChoice:      request.ToolChoice,
	}
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil {
		maxTokens := request.GetMaxTokens()
		out.MaxTokens = &maxTokens
	}
	return out
}

func normalizeMistralStreamData(data string) (string, error) {
	normalized, err := normalizeMistralResponseData([]byte(data))
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

func normalizeMistralResponseData(data []byte) ([]byte, error) {
	var response map[string]any
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, err
	}

	choices, ok := response["choices"].([]any)
	if !ok {
		return data, nil
	}

	changed := false
	for _, choiceValue := range choices {
		choice, ok := choiceValue.(map[string]any)
		if !ok {
			continue
		}
		for _, field := range []string{"message", "delta"} {
			message, ok := choice[field].(map[string]any)
			if !ok {
				continue
			}
			if normalizeMistralMessageContent(message) {
				changed = true
			}
		}
	}

	if !changed {
		return data, nil
	}
	return common.Marshal(response)
}

func normalizeMistralMessageContent(message map[string]any) bool {
	contentBlocks, ok := message["content"].([]any)
	if !ok {
		return false
	}

	var content strings.Builder
	var reasoningContent strings.Builder
	for _, blockValue := range contentBlocks {
		switch block := blockValue.(type) {
		case string:
			content.WriteString(block)
		case map[string]any:
			if block["type"] == "thinking" {
				appendMistralThinkingText(&reasoningContent, block["thinking"])
				continue
			}
			if text, ok := block["text"].(string); ok {
				content.WriteString(text)
			}
		}
	}

	message["content"] = content.String()
	if reasoningContent.Len() > 0 {
		existingReasoning, _ := message["reasoning_content"].(string)
		message["reasoning_content"] = existingReasoning + reasoningContent.String()
	}
	return true
}

func appendMistralThinkingText(builder *strings.Builder, thinkingValue any) {
	switch thinking := thinkingValue.(type) {
	case string:
		builder.WriteString(thinking)
	case []any:
		for _, itemValue := range thinking {
			switch item := itemValue.(type) {
			case string:
				builder.WriteString(item)
			case map[string]any:
				if text, ok := item["text"].(string); ok {
					builder.WriteString(text)
				}
			}
		}
	}
}
