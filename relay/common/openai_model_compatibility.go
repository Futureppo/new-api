package common

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/openaimodel"
)

// NormalizeOpenAIRequest operates on the final, overridden JSON, preserving
// fields it does not own (including custom fields introduced by an override).
// Callers gate it on ChannelTypeOpenAI and bypass it for raw body passthrough.
// The bool result says a Chat request requires the Responses wire protocol.
func NormalizeOpenAIRequest(body []byte, responses bool) ([]byte, string, bool, error) {
	var identity struct {
		Model string `json:"model"`
	}
	if err := common.Unmarshal(body, &identity); err != nil {
		return nil, "", false, err
	}
	model, suffixEffort, capabilities, known := openaimodel.Resolve(identity.Model)
	if !known {
		return body, identity.Model, false, nil
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil {
		return nil, "", false, err
	}
	if model != identity.Model {
		fields["model"], _ = common.Marshal(model)
	}
	var effort string
	var hasTools bool
	if responses {
		var request dto.OpenAIResponsesRequest
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, "", false, err
		}
		if request.Reasoning != nil {
			effort = request.Reasoning.Effort
		}
		if suffixEffort != "" {
			var reasoning map[string]json.RawMessage
			if raw := fields["reasoning"]; len(raw) > 0 {
				if err := common.Unmarshal(raw, &reasoning); err != nil {
					return nil, "", false, err
				}
			}
			if reasoning == nil {
				reasoning = make(map[string]json.RawMessage)
			}
			effort = suffixEffort
			reasoning["effort"], _ = common.Marshal(effort)
			fields["reasoning"], _ = common.Marshal(reasoning)
		}
		if err := capabilities.ValidateEffort(model, "reasoning.effort", effort); err != nil {
			return nil, "", false, err
		}
	} else {
		var request dto.GeneralOpenAIRequest
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, "", false, err
		}
		effort = request.ReasoningEffort
		if suffixEffort != "" {
			effort = suffixEffort
			fields["reasoning_effort"], _ = common.Marshal(effort)
		}
		if err := capabilities.ValidateEffort(model, "reasoning_effort", effort); err != nil {
			return nil, "", false, err
		}
		if capabilities.CompletionTokens {
			if request.MaxCompletionTokens == nil && request.MaxTokens != nil {
				fields["max_completion_tokens"] = fields["max_tokens"]
			}
			delete(fields, "max_tokens")
		}
		if capabilities.DeveloperRole && len(request.Messages) > 0 && request.Messages[0].Role == "system" {
			var messages []map[string]json.RawMessage
			if err := common.Unmarshal(fields["messages"], &messages); err != nil {
				return nil, "", false, err
			}
			messages[0]["role"] = json.RawMessage(`"developer"`)
			fields["messages"], _ = common.Marshal(messages)
		}
		hasTools = hasOpenAIChatTools(&request)
	}
	if capabilities.RemoveSampling(effort) {
		for _, name := range []string{"temperature", "top_p", "logprobs", "top_logprobs"} {
			delete(fields, name)
		}
		if responses && fields["include"] != nil {
			var include []string
			if err := common.Unmarshal(fields["include"], &include); err != nil {
				return nil, "", false, err
			}
			filtered := make([]string, 0, len(include))
			for _, value := range include {
				if value != "message.output_text.logprobs" {
					filtered = append(filtered, value)
				}
			}
			if len(filtered) != len(include) {
				fields["include"], _ = common.Marshal(filtered)
			}
		}
	}
	result, err := common.Marshal(fields)
	return result, model, !responses && capabilities.RequiresResponses(effort, hasTools), err
}

func hasOpenAIChatTools(request *dto.GeneralOpenAIRequest) bool {
	if len(request.Tools) > 0 || hasJSONValue(request.Functions) || hasJSONValue(request.FunctionCall) {
		return true
	}
	for _, message := range request.Messages {
		if message.Role == "tool" || message.Role == "function" ||
			hasJSONValue(message.ToolCalls) || hasJSONValue(message.FunctionCall) {
			return true
		}
	}
	return false
}

func hasJSONValue(value json.RawMessage) bool {
	return len(value) > 0 && string(value) != "null" && string(value) != "[]"
}
