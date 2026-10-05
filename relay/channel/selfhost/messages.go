package selfhost

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Qwen3.8's chat template accepts just one system message, at index zero.
// LiteLLM also maps Chat developer messages to system messages. Responses
// developer items are supported separately and must retain their role/order.
func normalizeQwenMessages(fields map[string]json.RawMessage, responses bool) error {
	key := "messages"
	if responses {
		key = "input"
	}
	raw := bytes.TrimSpace(fields[key])
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || (responses && raw[0] == '"') {
		return nil
	}
	var items []json.RawMessage
	if err := common.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("%s must be an array of messages", key)
	}
	var instructions []map[string]json.RawMessage
	remaining := make([]json.RawMessage, 0, len(items))
	firstIndex := -1
	for i, item := range items {
		var message map[string]json.RawMessage
		if err := common.Unmarshal(item, &message); err != nil || message == nil {
			return fmt.Errorf("%s[%d] must be an object", key, i)
		}
		var role, itemType string
		_ = common.Unmarshal(message["role"], &role)
		_ = common.Unmarshal(message["type"], &itemType)
		isInstruction := role == "system" || (!responses && role == "developer")
		if responses && itemType != "" && itemType != "message" {
			isInstruction = false // Function calls, references and other items are opaque.
		}
		if !isInstruction {
			remaining = append(remaining, item)
			continue
		}
		if firstIndex < 0 {
			firstIndex = i
		}
		instructions = append(instructions, message)
	}
	if len(instructions) == 0 {
		return nil
	}
	var contents []json.RawMessage
	hasInstructions := responses && len(fields["instructions"]) > 0 && !bytes.Equal(bytes.TrimSpace(fields["instructions"]), []byte("null"))
	if hasInstructions {
		var text string
		if err := common.Unmarshal(fields["instructions"], &text); err != nil {
			return fmt.Errorf("instructions must be a string")
		}
		contents = append(contents, fields["instructions"])
	}
	// Leave an already-compatible system message untouched, including metadata.
	if len(instructions) == 1 && firstIndex == 0 && !hasInstructions && string(instructions[0]["role"]) == `"system"` {
		return nil
	}
	merged := make(map[string]json.RawMessage)
	for _, instruction := range instructions {
		contents = append(contents, instruction["content"])
		for name, value := range instruction {
			if name == "role" || name == "content" {
				continue
			}
			if previous, exists := merged[name]; exists && !bytes.Equal(bytes.TrimSpace(previous), bytes.TrimSpace(value)) {
				// A single message cannot represent conflicting IDs/names/metadata.
				// Reject explicitly instead of silently discarding those fields.
				return fmt.Errorf("Qwen3.8 requires one leading system message; cannot merge conflicting instruction field %q", name)
			}
			merged[name] = value
		}
	}
	content, err := mergeInstructionContent(contents, responses)
	if err != nil {
		return err
	}
	merged["role"] = json.RawMessage(`"system"`)
	merged["content"] = content
	message, err := common.Marshal(merged)
	if err != nil {
		return err
	}
	fields[key], err = common.Marshal(append([]json.RawMessage{message}, remaining...))
	if err != nil {
		return err
	}
	if hasInstructions {
		// Otherwise vLLM would prepend instructions as a second system message.
		delete(fields, "instructions")
	}
	return nil
}

func mergeInstructionContent(contents []json.RawMessage, responses bool) (json.RawMessage, error) {
	textType := "text"
	if responses {
		textType = "input_text"
	}
	textPart := func(text string) json.RawMessage {
		part, _ := common.Marshal(map[string]string{"type": textType, "text": text})
		return part
	}
	texts := make([]string, 0, len(contents))
	parts := make([]json.RawMessage, 0, len(contents))
	allText := true
	for i, content := range contents {
		content = bytes.TrimSpace(content)
		if i > 0 {
			parts = append(parts, textPart("\n\n"))
		}
		var text string
		if len(content) > 0 && content[0] == '"' && common.Unmarshal(content, &text) == nil {
			texts = append(texts, text)
			parts = append(parts, textPart(text))
			continue
		}
		var contentParts []json.RawMessage
		if len(content) == 0 || content[0] != '[' || common.Unmarshal(content, &contentParts) != nil {
			return nil, fmt.Errorf("Qwen3.8 system/developer content must be a string or content array to merge instructions")
		}
		allText = false
		parts = append(parts, contentParts...)
	}
	if allText {
		return common.Marshal(strings.Join(texts, "\n\n"))
	}
	return common.Marshal(parts)
}
