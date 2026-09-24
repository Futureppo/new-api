package openaicompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// ValidateChatRequestForResponses rejects data the compatibility converter
// would otherwise ignore or rewrite. Official OpenAI callers opt in so other
// providers retain their existing conversion behavior.
func ValidateChatRequestForResponses(req *dto.GeneralOpenAIRequest) error {
	for i, tool := range req.Tools {
		switch tool.Type {
		case "function":
		case "custom":
			var custom map[string]any
			if err := common.Unmarshal(tool.Custom, &custom); err != nil || custom == nil {
				return fmt.Errorf("tools[%d].custom must be an object in responses compatibility mode", i)
			}
		default:
			return fmt.Errorf("tools[%d].type=%q cannot be represented in responses compatibility mode", i, tool.Type)
		}
	}
	for i, msg := range req.Messages {
		// ParseContent is deliberately permissive for legacy providers. Check
		// its result here before conversion can silently drop unknown parts.
		switch content := msg.Content.(type) {
		case nil, string, []dto.MediaContent:
		case []any:
			if len(msg.ParseContent()) != len(content) {
				return fmt.Errorf("messages[%d].content contains parts that cannot be represented in responses compatibility mode", i)
			}
		default:
			return fmt.Errorf("messages[%d].content must be a string or content array in responses compatibility mode", i)
		}
		role := strings.TrimSpace(msg.Role)
		if role == "function" {
			return fmt.Errorf("messages[%d].role=function is not supported in responses compatibility mode; use role=tool with tool_call_id", i)
		}
		if role == "tool" && strings.TrimSpace(msg.ToolCallId) == "" {
			return fmt.Errorf("messages[%d].tool_call_id is required in responses compatibility mode", i)
		}
		if len(msg.ToolCalls) == 0 {
			continue
		}
		var calls []dto.ToolCallRequest
		if err := common.Unmarshal(msg.ToolCalls, &calls); err != nil {
			return fmt.Errorf("messages[%d].tool_calls cannot be represented in responses compatibility mode: %w", i, err)
		}
		if len(calls) > 0 && role != "assistant" {
			return fmt.Errorf("messages[%d].tool_calls requires role=assistant in responses compatibility mode", i)
		}
		for j, call := range calls {
			if strings.TrimSpace(call.ID) == "" {
				return fmt.Errorf("messages[%d].tool_calls[%d].id is required in responses compatibility mode", i, j)
			}
			if call.Type == "custom" {
				var custom map[string]any
				if err := common.Unmarshal(call.Custom, &custom); err != nil || custom == nil {
					return fmt.Errorf("messages[%d].tool_calls[%d].custom must be an object in responses compatibility mode", i, j)
				}
			}
		}
	}
	if req.ToolChoice != nil {
		if _, ok := req.ToolChoice.(string); !ok {
			body, err := common.Marshal(req.ToolChoice)
			if err != nil {
				return err
			}
			var choice struct {
				Type string `json:"type"`
			}
			if err := common.Unmarshal(body, &choice); err != nil || (choice.Type != "function" && choice.Type != "custom") {
				return fmt.Errorf("tool_choice cannot be represented in responses compatibility mode; use a string or a function/custom tool selection")
			}
		}
	}
	return nil
}
