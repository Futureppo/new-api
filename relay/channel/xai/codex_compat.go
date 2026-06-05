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
	request.Input = normalizeXAIResponsesInput(request.Input)
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

func normalizeXAIResponsesInput(raw []byte) []byte {
	if len(raw) == 0 {
		return nil
	}

	switch common.GetJsonType(raw) {
	case "string":
		return raw
	case "object":
		var item map[string]any
		if err := common.Unmarshal(raw, &item); err != nil {
			return nil
		}
		normalized, ok := normalizeXAIResponsesInputItem(item)
		if !ok {
			return nil
		}
		return mustMarshalRaw([]any{normalized})
	case "array":
		var items []any
		if err := common.Unmarshal(raw, &items); err != nil {
			return nil
		}
		normalized := make([]any, 0, len(items))
		for _, item := range items {
			if next, ok := normalizeXAIResponsesInputItem(item); ok {
				normalized = append(normalized, next)
			}
		}
		if len(normalized) == 0 {
			return nil
		}
		return mustMarshalRaw(normalized)
	default:
		return raw
	}
}

func normalizeXAIResponsesInputItem(item any) (any, bool) {
	switch v := item.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return map[string]any{"role": "user", "content": v}, true
	case map[string]any:
		itemType, _ := v["type"].(string)
		itemType = strings.ToLower(strings.TrimSpace(itemType))
		if itemType == "" {
			if _, ok := v["role"].(string); ok {
				return normalizeXAIResponsesMessageInput(v)
			}
			return nil, false
		}

		switch itemType {
		case "message":
			return normalizeXAIResponsesMessageInput(v)
		case "function_call":
			return normalizeXAIResponsesFunctionCallInput(v)
		case "function_call_output":
			return normalizeXAIResponsesFunctionCallOutputInput(v)
		case "local_shell_call_output", "custom_tool_call_output":
			return normalizeUnsupportedCallOutputAsUserMessage(v, itemType)
		case "computer_call_output":
			return normalizeUnsupportedCallOutputAsUserMessage(v, itemType)
		case "reasoning", "web_search_call", "file_search_call",
			"computer_call", "local_shell_call", "custom_tool_call",
			"image_generation_call":
			return nil, false
		default:
			if _, ok := v["role"].(string); ok {
				return normalizeXAIResponsesMessageInput(v)
			}
			return nil, false
		}
	default:
		return nil, false
	}
}

func normalizeXAIResponsesMessageInput(item map[string]any) (map[string]any, bool) {
	role, _ := item["role"].(string)
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "developer":
		role = "system"
	case "system", "user", "assistant":
	default:
		return nil, false
	}

	content, ok := normalizeXAIResponsesMessageContent(item["content"])
	if !ok {
		return nil, false
	}
	return map[string]any{
		"role":    role,
		"content": content,
	}, true
}

func normalizeXAIResponsesMessageContent(content any) (any, bool) {
	switch v := content.(type) {
	case string:
		return v, true
	case []any:
		parts := make([]map[string]any, 0, len(v))
		onlyText := true
		textParts := make([]string, 0, len(v))
		for _, part := range v {
			normalized, isText, ok := normalizeXAIResponsesContentPart(part)
			if !ok {
				continue
			}
			if isText {
				if text, _ := normalized["text"].(string); text != "" {
					textParts = append(textParts, text)
				}
			} else {
				onlyText = false
			}
			parts = append(parts, normalized)
		}
		if len(parts) == 0 {
			return nil, false
		}
		if onlyText {
			return strings.Join(textParts, "\n"), true
		}
		return parts, true
	case map[string]any:
		part, _, ok := normalizeXAIResponsesContentPart(v)
		if !ok {
			return nil, false
		}
		return []map[string]any{part}, true
	default:
		return nil, false
	}
}

func normalizeXAIResponsesContentPart(part any) (map[string]any, bool, bool) {
	partMap, ok := part.(map[string]any)
	if !ok {
		return nil, false, false
	}
	partType, _ := partMap["type"].(string)
	partType = strings.ToLower(strings.TrimSpace(partType))
	switch partType {
	case "input_text", "output_text", "text":
		text, _ := partMap["text"].(string)
		return map[string]any{
			"type": "input_text",
			"text": text,
		}, true, text != ""
	case "input_image":
		out := map[string]any{"type": "input_image"}
		copyIfPresent(out, partMap, "image_url")
		copyIfPresent(out, partMap, "file_id")
		copyIfPresent(out, partMap, "detail")
		if _, hasImageURL := out["image_url"]; !hasImageURL {
			if _, hasFileID := out["file_id"]; !hasFileID {
				return nil, false, false
			}
		}
		return out, false, true
	default:
		if text, ok := partMap["text"].(string); ok && strings.TrimSpace(text) != "" {
			return map[string]any{
				"type": "input_text",
				"text": text,
			}, true, true
		}
		return nil, false, false
	}
}

func normalizeXAIResponsesFunctionCallInput(item map[string]any) (map[string]any, bool) {
	out := map[string]any{"type": "function_call"}
	copyIfPresent(out, item, "call_id")
	copyIfPresent(out, item, "name")
	copyIfPresent(out, item, "status")
	if arguments, ok := callOutputString(item["arguments"]); ok {
		out["arguments"] = arguments
	}
	if _, ok := out["call_id"].(string); !ok {
		return nil, false
	}
	if _, ok := out["name"].(string); !ok {
		return nil, false
	}
	return out, true
}

func normalizeXAIResponsesFunctionCallOutputInput(item map[string]any) (map[string]any, bool) {
	callID, _ := item["call_id"].(string)
	callID = strings.TrimSpace(callID)
	output, ok := callOutputString(item["output"])
	if callID == "" || !ok {
		return nil, false
	}
	return map[string]any{
		"type":    "function_call_output",
		"call_id": callID,
		"output":  output,
	}, true
}

func normalizeUnsupportedCallOutputAsUserMessage(item map[string]any, itemType string) (map[string]any, bool) {
	output, ok := callOutputString(item["output"])
	if !ok || isImageOnlyToolOutput(item["output"]) {
		return nil, false
	}
	callID, _ := item["call_id"].(string)
	prefix := itemType
	if strings.TrimSpace(callID) != "" {
		prefix += " " + strings.TrimSpace(callID)
	}
	return map[string]any{
		"role":    "user",
		"content": prefix + " output:\n" + output,
	}, true
}

func callOutputString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, strings.TrimSpace(v) != ""
	case nil:
		return "", false
	default:
		raw, err := common.Marshal(v)
		if err != nil {
			return "", false
		}
		out := strings.TrimSpace(string(raw))
		if out == "" || out == "null" {
			return "", false
		}
		return out, true
	}
}

func isImageOnlyToolOutput(value any) bool {
	part, ok := value.(map[string]any)
	if !ok {
		return false
	}
	partType, _ := part["type"].(string)
	if strings.EqualFold(strings.TrimSpace(partType), "input_image") {
		return true
	}
	if _, ok := part["image_url"]; ok {
		return true
	}
	return false
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
