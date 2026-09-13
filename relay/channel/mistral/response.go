package mistral

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

func invalidRequest(message string) *types.NewAPIError {
	return types.WithOpenAIError(types.OpenAIError{Type: "invalid_request_error", Message: message}, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func badResponse(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

// errorFromBody handles both Mistral's top-level errors and validation details.
// Never include validation "input": it can contain the entire prompt or audio.
func errorFromBody(data []byte, status int) *types.NewAPIError {
	var envelope struct {
		Object  string             `json:"object"`
		Message any                `json:"message"`
		Type    string             `json:"type"`
		Code    any                `json:"code"`
		Param   string             `json:"param"`
		Error   *types.OpenAIError `json:"error"`
		Detail  any                `json:"detail"`
	}
	if common.Unmarshal(data, &envelope) != nil {
		return nil
	}
	if envelope.Error != nil {
		return types.WithOpenAIError(*envelope.Error, status)
	}
	if envelope.Object == "error" || (envelope.Message != nil && envelope.Type != "") {
		message, _ := envelope.Message.(string)
		if nested, ok := envelope.Message.(map[string]any); ok {
			data, err := common.Marshal(nested)
			if err == nil {
				if nestedErr := errorFromBody(data, status); nestedErr != nil {
					message = nestedErr.Error()
				}
			}
		}
		if message == "" {
			message = "Mistral upstream request failed"
		}
		return types.WithOpenAIError(types.OpenAIError{Message: message, Type: envelope.Type, Code: envelope.Code, Param: envelope.Param}, status)
	}
	if envelope.Detail != nil {
		messages := []string{}
		switch detail := envelope.Detail.(type) {
		case string:
			messages = append(messages, detail)
		case []any:
			for _, value := range detail {
				item, _ := value.(map[string]any)
				message, _ := item["msg"].(string)
				if message == "" {
					continue
				}
				if loc, ok := item["loc"].([]any); ok {
					parts := make([]string, len(loc))
					for i, part := range loc {
						parts[i] = fmt.Sprint(part)
					}
					message = strings.Join(parts, ".") + ": " + message
				}
				messages = append(messages, message)
			}
		}
		message := strings.Join(messages, "; ")
		if message == "" {
			message = "Mistral request validation failed"
		}
		return types.WithOpenAIError(types.OpenAIError{Message: message, Type: "invalid_request_error", Code: "validation_error"}, status)
	}
	return nil
}

func normalizeErrorBody(data []byte, status int) []byte {
	if apiErr := errorFromBody(data, status); apiErr != nil {
		body, _ := common.Marshal(map[string]any{"error": apiErr.ToOpenAIError()})
		return body
	}
	return nil
}

// Mistral audio models report prompt_tokens as text-only, while total_tokens
// includes audio. OpenAI counts audio inside prompt_tokens. The total guards
// against double inclusion when an upstream already uses OpenAI semantics.
func normalizeUsage(usage *dto.Usage) {
	audio := usage.PromptTokensDetails.AudioTokens
	if audio > 0 && usage.TotalTokens == usage.PromptTokens+usage.CompletionTokens+audio {
		usage.PromptTokens += audio
	}
	if usage.PromptTokensDetails.TextTokens == 0 {
		usage.PromptTokensDetails.TextTokens = max(0, usage.PromptTokens-audio)
	}
	if usage.CompletionTokenDetails.TextTokens == 0 {
		usage.CompletionTokenDetails.TextTokens = max(0, usage.CompletionTokens-usage.CompletionTokenDetails.AudioTokens)
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func normalizeUsageMap(response map[string]any) bool {
	value, ok := response["usage"].(map[string]any)
	if !ok {
		return false
	}
	data, err := common.Marshal(value)
	if err != nil {
		return false
	}
	var usage dto.Usage
	if common.Unmarshal(data, &usage) != nil {
		return false
	}
	normalizeUsage(&usage)
	value["prompt_tokens"] = usage.PromptTokens
	value["total_tokens"] = usage.TotalTokens
	for _, field := range []struct {
		key  string
		text int
	}{
		{"prompt_tokens_details", usage.PromptTokensDetails.TextTokens},
		{"completion_tokens_details", usage.CompletionTokenDetails.TextTokens},
	} {
		details, ok := value[field.key].(map[string]any)
		if !ok {
			details = make(map[string]any)
			value[field.key] = details
		}
		details["text_tokens"] = field.text
	}
	return true
}

func readUsage(response map[string]any) (*dto.Usage, error) {
	if response["usage"] == nil {
		return nil, errors.New("Mistral response is missing usage")
	}
	data, err := common.Marshal(response["usage"])
	if err != nil {
		return nil, err
	}
	usage := &dto.Usage{}
	if err := common.Unmarshal(data, usage); err != nil {
		return nil, err
	}
	normalizeUsage(usage)
	return usage, nil
}
