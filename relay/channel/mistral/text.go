package mistral

import (
	"crypto/sha256"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

var mistralToolCallIdRegexp = regexp.MustCompile("^[a-zA-Z0-9]{9}$")

func requestOpenAI2Mistral(request *dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	if request == nil {
		return nil, invalidRequest("request is nil")
	}
	if len(request.Functions) > 0 || len(request.FunctionCall) > 0 {
		return nil, invalidRequest("use tools and tool_choice instead of legacy functions")
	}
	// Keep the shared DTO so channel system prompts and parameter overrides still
	// apply after conversion. Deep copy prevents rewriting reusable client history.
	cloned, err := common.DeepCopy(request)
	if err != nil {
		return nil, err
	}
	messages := make([]dto.Message, 0, len(cloned.Messages))
	usedIDs := make(map[string]bool)
	for _, m := range cloned.Messages {
		for _, t := range m.ParseToolCalls() {
			if mistralToolCallIdRegexp.MatchString(t.ID) {
				usedIDs[t.ID] = true
			}
		}
		if mistralToolCallIdRegexp.MatchString(m.ToolCallId) {
			usedIDs[m.ToolCallId] = true
		}
	}
	idMap := make(map[string]string)
	convertID := func(id string) string {
		if mistralToolCallIdRegexp.MatchString(id) {
			return id
		}
		if mapped, ok := idMap[id]; ok {
			return mapped
		}
		for salt := 0; ; salt++ {
			digest := sha256.Sum256([]byte(id + ":" + strconv.Itoa(salt)))
			candidate := fmt.Sprintf("%x", digest)[:9]
			if !usedIDs[candidate] {
				usedIDs[candidate] = true
				idMap[id] = candidate
				return candidate
			}
		}
	}
	for _, m := range cloned.Messages {
		if m.Role == "developer" {
			m.Role = "system"
		}
		switch m.Role {
		case "system", "user", "assistant", "tool":
		default:
			return nil, invalidRequest("unsupported message role: " + m.Role)
		}
		if len(m.ToolCalls) > 0 {
			var calls []dto.ToolCallResponse
			if err := common.Unmarshal(m.ToolCalls, &calls); err != nil {
				return nil, invalidRequest("invalid tool_calls")
			}
			for i := range calls {
				if calls[i].ID == "" {
					return nil, invalidRequest("tool_calls require an id")
				}
				calls[i].ID = convertID(calls[i].ID)
			}
			m.ToolCalls, err = common.Marshal(calls)
			if err != nil {
				return nil, err
			}
		}
		if m.ToolCallId != "" {
			m.ToolCallId = convertID(m.ToolCallId)
		}
		// Native Mistral accepts the OpenAI image_url and input_audio object forms.
		// Do not flatten through ParseContent: that loses detail and null/string shape.
		converted := dto.Message{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallId: m.ToolCallId}
		if m.Role == "tool" {
			converted.Name = m.Name
		}
		if m.Role == "assistant" {
			converted.Prefix = m.Prefix
		}
		messages = append(messages, converted)
	}
	for _, tool := range cloned.Tools {
		if tool.Type != "function" {
			return nil, invalidRequest("only function tools are supported by this Mistral adapter")
		}
	}
	out := &dto.GeneralOpenAIRequest{
		Model: cloned.Model, Stream: cloned.Stream, Messages: messages,
		ReasoningEffort: cloned.ReasoningEffort, Temperature: cloned.Temperature,
		TopP: cloned.TopP, Tools: cloned.Tools, ToolChoice: cloned.ToolChoice,
		Stop: cloned.Stop, N: cloned.N, FrequencyPenalty: cloned.FrequencyPenalty,
		PresencePenalty: cloned.PresencePenalty, ResponseFormat: cloned.ResponseFormat,
		ParallelTooCalls: cloned.ParallelTooCalls, Prediction: cloned.Prediction,
		Metadata: cloned.Metadata, PromptCacheKey: cloned.PromptCacheKey, ServiceTier: cloned.ServiceTier,
		MaxTokens: cloned.MaxTokens, RandomSeed: cloned.RandomSeed,
	}
	if cloned.MaxCompletionTokens != nil {
		out.MaxTokens = cloned.MaxCompletionTokens
	}
	if cloned.Seed != nil {
		seed := *cloned.Seed
		if math.IsNaN(seed) || math.IsInf(seed, 0) || seed < 0 || seed >= math.Exp2(63) || math.Trunc(seed) != seed {
			return nil, invalidRequest("seed must be a non-negative int64")
		}
		value := int64(seed)
		out.RandomSeed = &value
	}
	if out.RandomSeed != nil && *out.RandomSeed < 0 {
		return nil, invalidRequest("random_seed must be non-negative")
	}
	return out, nil
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
	if response == nil {
		return nil, fmt.Errorf("empty Mistral response")
	}
	if normalized := normalizeErrorBody(data, 502); normalized != nil {
		return normalized, nil
	}
	changed := normalizeUsageMap(response)
	if _, ok := response["p"]; ok {
		delete(response, "p") // Provider SSE padding is not part of the completion.
		changed = true
	}

	choices, ok := response["choices"].([]any)
	if !ok {
		return nil, fmt.Errorf("Mistral chat response has no choices")
	}

	for _, choiceValue := range choices {
		choice, ok := choiceValue.(map[string]any)
		if !ok {
			continue
		}
		if choice["finish_reason"] == "error" {
			return nil, fmt.Errorf("Mistral generation failed (finish_reason=error)")
		}
		for _, field := range []string{"message", "delta"} {
			message, ok := choice[field].(map[string]any)
			if !ok {
				continue
			}
			if normalizeMistralMessageContent(message) {
				changed = true
			}
			if calls, ok := message["tool_calls"].([]any); ok {
				for _, value := range calls {
					call, _ := value.(map[string]any)
					function, _ := call["function"].(map[string]any)
					if arguments, ok := function["arguments"].(map[string]any); ok {
						encoded, err := common.Marshal(arguments)
						if err != nil {
							return nil, err
						}
						function["arguments"] = string(encoded)
						changed = true
					}
				}
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
