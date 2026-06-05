package xai

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const (
	xaiCodexUserAgentNeedle = "codex"
	xaiGrokConversationID   = "X-Grok-Conv-Id"
)

func IsCodexCompatibilityRequest(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c == nil || c.Request == nil || info == nil || info.ChannelMeta == nil {
		return false
	}
	if !info.ChannelOtherSettings.XAICodexCompatibilityEnabled {
		return false
	}
	userAgent := strings.ToLower(strings.TrimSpace(c.Request.Header.Get("User-Agent")))
	return strings.Contains(userAgent, xaiCodexUserAgentNeedle)
}

func convertCodexResponsesRequestForXAI(request dto.OpenAIResponsesRequest, compact bool) dto.OpenAIResponsesRequest {
	request = moveInstructionsIntoInput(request)
	if compact {
		return dto.OpenAIResponsesRequest{
			Model: request.Model,
			Input: request.Input,
		}
	}

	request.Include = filterXAIResponsesInclude(request.Include)
	request.Tools, request.ToolChoice = normalizeXAIResponsesTools(request.Tools, request.ToolChoice)

	request.Conversation = nil
	request.ContextManagement = nil
	request.Instructions = nil
	request.PromptCacheRetention = nil
	request.SafetyIdentifier = nil
	request.StreamOptions = nil
	request.Prompt = nil
	request.EnableThinking = nil
	request.Preset = nil
	request.ServiceTier = ""
	request.MaxToolCalls = nil

	return request
}

func moveInstructionsIntoInput(request dto.OpenAIResponsesRequest) dto.OpenAIResponsesRequest {
	instructions := parseStringRawMessage(request.Instructions)
	request.Instructions = nil
	if strings.TrimSpace(instructions) == "" {
		return request
	}

	input, ok := parseResponsesInputItems(request.Input)
	if !ok {
		request.Input = mustMarshalRaw([]map[string]any{
			{"role": "system", "content": instructions},
			{"role": "user", "content": parseStringRawMessage(request.Input)},
		})
		return request
	}

	for i := range input {
		role, _ := input[i]["role"].(string)
		if strings.EqualFold(strings.TrimSpace(role), "system") {
			input[i]["content"] = mergeInstructionContent(instructions, input[i]["content"])
			request.Input = mustMarshalRaw(input)
			return request
		}
	}

	withSystem := make([]map[string]any, 0, len(input)+1)
	withSystem = append(withSystem, map[string]any{"role": "system", "content": instructions})
	withSystem = append(withSystem, input...)
	request.Input = mustMarshalRaw(withSystem)
	return request
}

func parseStringRawMessage(raw []byte) string {
	if len(raw) == 0 || common.GetJsonType(raw) != "string" {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func parseResponsesInputItems(raw []byte) ([]map[string]any, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	if common.GetJsonType(raw) != "array" {
		return nil, false
	}
	var input []map[string]any
	if err := common.Unmarshal(raw, &input); err != nil {
		return nil, false
	}
	return input, true
}

func mergeInstructionContent(instructions string, content any) any {
	switch v := content.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return instructions
		}
		return instructions + "\n\n" + v
	case []any:
		merged := make([]any, 0, len(v)+1)
		merged = append(merged, map[string]any{
			"type": "input_text",
			"text": instructions,
		})
		merged = append(merged, v...)
		return merged
	default:
		return instructions
	}
}

func filterXAIResponsesInclude(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}
	values := make([]string, 0, 1)
	switch common.GetJsonType(raw) {
	case "string":
		if value := parseStringRawMessage(raw); value == "reasoning.encrypted_content" {
			values = append(values, value)
		}
	case "array":
		var items []string
		if err := common.Unmarshal(raw, &items); err != nil {
			return nil
		}
		for _, item := range items {
			if item == "reasoning.encrypted_content" {
				values = append(values, item)
				break
			}
		}
	default:
		return nil
	}
	if len(values) == 0 {
		return nil
	}
	return mustMarshalRaw(values)
}

func normalizeXAIResponsesTools(toolsRaw, toolChoiceRaw []byte) ([]byte, []byte) {
	tools, functionNames := normalizeXAIResponsesToolList(toolsRaw)
	toolChoice := normalizeXAIResponsesToolChoice(toolChoiceRaw, functionNames)
	return tools, toolChoice
}

func normalizeXAIResponsesToolList(raw []byte) ([]byte, map[string]struct{}) {
	functionNames := map[string]struct{}{}
	if len(raw) == 0 || common.GetJsonType(raw) != "array" {
		return nil, functionNames
	}

	var tools []map[string]any
	if err := common.Unmarshal(raw, &tools); err != nil {
		return nil, functionNames
	}

	normalized := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		next, ok := normalizeXAIResponsesTool(tool)
		if !ok {
			continue
		}
		if nextType, _ := next["type"].(string); nextType == "function" {
			if name, _ := next["name"].(string); strings.TrimSpace(name) != "" {
				functionNames[name] = struct{}{}
			}
		}
		normalized = append(normalized, next)
	}
	if len(normalized) == 0 {
		return nil, functionNames
	}
	return mustMarshalRaw(normalized), functionNames
}

func normalizeXAIResponsesTool(tool map[string]any) (map[string]any, bool) {
	toolType, _ := tool["type"].(string)
	toolType = strings.ToLower(strings.TrimSpace(toolType))
	switch toolType {
	case "function":
		return normalizeXAIResponsesFunctionTool(tool)
	case "web_search_preview":
		tool["type"] = "web_search"
		return tool, true
	case "web_search", "x_search":
		tool["type"] = toolType
		return tool, true
	default:
		return nil, false
	}
}

func normalizeXAIResponsesFunctionTool(tool map[string]any) (map[string]any, bool) {
	out := map[string]any{
		"type": "function",
	}

	if fn, ok := tool["function"].(map[string]any); ok {
		copyIfPresent(out, fn, "name")
		copyIfPresent(out, fn, "description")
		copyIfPresent(out, fn, "parameters")
		copyIfPresent(out, fn, "strict")
	} else {
		copyIfPresent(out, tool, "name")
		copyIfPresent(out, tool, "description")
		copyIfPresent(out, tool, "parameters")
		copyIfPresent(out, tool, "strict")
	}

	name, _ := out["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, false
	}
	out["name"] = strings.TrimSpace(name)
	return out, true
}

func normalizeXAIResponsesToolChoice(raw []byte, functionNames map[string]struct{}) []byte {
	if len(raw) == 0 {
		return nil
	}
	if common.GetJsonType(raw) == "string" {
		choice := strings.ToLower(strings.TrimSpace(parseStringRawMessage(raw)))
		switch choice {
		case "auto", "required", "none":
			return mustMarshalRaw(choice)
		default:
			return nil
		}
	}
	if common.GetJsonType(raw) != "object" {
		return nil
	}

	var choice map[string]any
	if err := common.Unmarshal(raw, &choice); err != nil {
		return nil
	}
	choiceType, _ := choice["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(choiceType), "function") {
		return nil
	}
	name := toolChoiceFunctionName(choice)
	if name == "" {
		return nil
	}
	if _, ok := functionNames[name]; !ok {
		return nil
	}
	return mustMarshalRaw(map[string]any{
		"type": "function",
		"name": name,
	})
}

func toolChoiceFunctionName(choice map[string]any) string {
	if name, ok := choice["name"].(string); ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if fn, ok := choice["function"].(map[string]any); ok {
		if name, ok := fn["name"].(string); ok && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func copyIfPresent(dst, src map[string]any, key string) {
	if value, ok := src[key]; ok {
		dst[key] = value
	}
}

func mustMarshalRaw(v any) []byte {
	raw, err := common.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}
