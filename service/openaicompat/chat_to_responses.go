package openaicompat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/samber/lo"
)

func normalizeChatImageURLToString(v any) any {
	switch vv := v.(type) {
	case string:
		return vv
	case map[string]any:
		if url := common.Interface2String(vv["url"]); url != "" {
			return url
		}
		return v
	case dto.MessageImageUrl:
		if vv.Url != "" {
			return vv.Url
		}
		return v
	case *dto.MessageImageUrl:
		if vv != nil && vv.Url != "" {
			return vv.Url
		}
		return v
	default:
		return v
	}
}

func convertChatResponseFormatToResponsesText(reqFormat *dto.ResponseFormat) json.RawMessage {
	if reqFormat == nil || strings.TrimSpace(reqFormat.Type) == "" {
		return nil
	}

	format := map[string]any{
		"type": reqFormat.Type,
	}

	if reqFormat.Type == "json_schema" && len(reqFormat.JsonSchema) > 0 {
		var chatSchema map[string]any
		if err := common.Unmarshal(reqFormat.JsonSchema, &chatSchema); err == nil {
			for key, value := range chatSchema {
				if key == "type" {
					continue
				}
				format[key] = value
			}

			if nested, ok := format["json_schema"].(map[string]any); ok {
				for key, value := range nested {
					if _, exists := format[key]; !exists {
						format[key] = value
					}
				}
				delete(format, "json_schema")
			}
		} else {
			format["json_schema"] = reqFormat.JsonSchema
		}
	}

	textRaw, _ := common.Marshal(map[string]any{
		"format": format,
	})
	return textRaw
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}
	if req.Model == "" {
		return nil, errors.New("model is required")
	}
	if lo.FromPtrOr(req.N, 1) > 1 {
		return nil, fmt.Errorf("n>1 is not supported in responses compatibility mode")
	}
	if (len(req.Functions) > 0 && string(req.Functions) != "null") || (len(req.FunctionCall) > 0 && string(req.FunctionCall) != "null") {
		return nil, errors.New("legacy functions/function_call are not supported in responses compatibility mode; use tools/tool_choice or native Chat Completions")
	}

	if len(req.Audio) > 0 && string(req.Audio) != "null" {
		return nil, errors.New("audio is not supported in responses compatibility mode; use native Chat Completions")
	}
	var modalities []string
	if len(req.Modalities) > 0 {
		if err := common.Unmarshal(req.Modalities, &modalities); err != nil {
			return nil, err
		}
		for _, modality := range modalities {
			if modality != "text" {
				return nil, fmt.Errorf("modality %q is not supported in responses compatibility mode", modality)
			}
		}
	}
	var instructionsParts []string
	inputItems := make([]map[string]any, 0, len(req.Messages))
	customCalls := make(map[string]bool)
	preserveInstructions := false
	for _, msg := range req.Messages {
		for _, call := range msg.ParseToolCalls() {
			if call.Type == "custom" {
				customCalls[call.ID] = true
			}
		}
		if msg.Role == "system" || msg.Role == "developer" {
			for _, part := range msg.ParseContent() {
				if len(part.PromptCacheBreakpoint) > 0 {
					preserveInstructions = true
				}
			}
		}
	}
	for _, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			continue
		}
		if (len(msg.Audio) > 0 && string(msg.Audio) != "null") || (len(msg.FunctionCall) > 0 && string(msg.FunctionCall) != "null") {
			return nil, errors.New("message audio and legacy function_call cannot be represented in responses compatibility mode")
		}
		parts, err := chatContentToResponses(msg)
		if err != nil {
			return nil, err
		}
		if role == "tool" || role == "function" {
			callID := strings.TrimSpace(msg.ToolCallId)
			var output any = ""
			if msg.IsStringContent() {
				output = msg.StringContent()
			} else if len(parts) > 0 {
				output = parts
			}
			if callID == "" {
				inputItems = append(inputItems, map[string]any{"role": "user", "content": fmt.Sprintf("[tool_output_missing_call_id] %v", output)})
				continue
			}
			itemType := "function_call_output"
			if customCalls[callID] {
				itemType = "custom_tool_call_output"
			}
			inputItems = append(inputItems, map[string]any{"type": itemType, "call_id": callID, "output": output})
			continue
		}
		if (role == "system" || role == "developer") && !preserveInstructions {
			var texts []string
			for _, part := range parts {
				if txt, ok := part["text"].(string); ok && strings.TrimSpace(txt) != "" {
					texts = append(texts, txt)
				}
			}
			if len(texts) > 0 {
				instructionsParts = append(instructionsParts, strings.Join(texts, "\n"))
			}
			continue
		}
		if msg.Refusal != nil {
			parts = append(parts, map[string]any{"type": "refusal", "refusal": *msg.Refusal})
		}
		if msg.IsStringContent() && msg.Refusal == nil {
			inputItems = append(inputItems, map[string]any{"role": role, "content": msg.StringContent()})
		} else if len(parts) > 0 {
			inputItems = append(inputItems, map[string]any{"role": role, "content": parts})
		} else if len(msg.ToolCalls) == 0 {
			inputItems = append(inputItems, map[string]any{"role": role, "content": ""})
		}
		if role == "assistant" {
			for _, call := range msg.ParseToolCalls() {
				if call.ID == "" {
					return nil, errors.New("tool call id is required in responses compatibility mode")
				}
				switch call.Type {
				case "", "function":
					inputItems = append(inputItems, map[string]any{"type": "function_call", "call_id": call.ID, "name": call.Function.Name, "arguments": call.Function.Arguments})
				case "custom":
					var custom map[string]any
					if err := common.Unmarshal(call.Custom, &custom); err != nil {
						return nil, err
					}
					inputItems = append(inputItems, map[string]any{"type": "custom_tool_call", "call_id": call.ID, "name": custom["name"], "input": custom["input"]})
				default:
					return nil, fmt.Errorf("tool call type %q is not supported in responses compatibility mode", call.Type)
				}
			}
		}
	}

	inputRaw, err := common.Marshal(inputItems)
	if err != nil {
		return nil, err
	}

	var instructionsRaw json.RawMessage
	if len(instructionsParts) > 0 {
		instructions := strings.Join(instructionsParts, "\n\n")
		instructionsRaw, _ = common.Marshal(instructions)
	}

	var toolsRaw json.RawMessage
	if req.Tools != nil {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			switch tool.Type {
			case "function":
				tools = append(tools, map[string]any{
					"type":        "function",
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
					"parameters":  tool.Function.Parameters,
				})
				if tool.Function.Strict != nil {
					tools[len(tools)-1]["strict"] = *tool.Function.Strict
				}
			case "custom":
				var custom map[string]any
				if err := common.Unmarshal(tool.Custom, &custom); err != nil {
					return nil, err
				}
				custom["type"] = "custom"
				tools = append(tools, custom)
			default:
				// Best-effort: keep original tool shape for unknown types.
				var m map[string]any
				if b, err := common.Marshal(tool); err == nil {
					_ = common.Unmarshal(b, &m)
				}
				if len(m) == 0 {
					m = map[string]any{"type": tool.Type}
				}
				tools = append(tools, m)
			}
		}
		toolsRaw, _ = common.Marshal(tools)
	}

	var toolChoiceRaw json.RawMessage
	if req.ToolChoice != nil {
		switch v := req.ToolChoice.(type) {
		case string:
			toolChoiceRaw, _ = common.Marshal(v)
		default:
			var m map[string]any
			if b, err := common.Marshal(v); err == nil {
				_ = common.Unmarshal(b, &m)
			}
			if m == nil {
				toolChoiceRaw, _ = common.Marshal(v)
			} else if t, _ := m["type"].(string); t == "function" || t == "custom" {
				// Chat: {"type":"function","function":{"name":"..."}}
				// Responses: {"type":"function","name":"..."}
				if name, ok := m["name"].(string); ok && name != "" {
					toolChoiceRaw, _ = common.Marshal(map[string]any{
						"type": t,
						"name": name,
					})
				} else if fn, ok := m[t].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						toolChoiceRaw, _ = common.Marshal(map[string]any{
							"type": t,
							"name": name,
						})
					} else {
						toolChoiceRaw, _ = common.Marshal(v)
					}
				} else {
					toolChoiceRaw, _ = common.Marshal(v)
				}
			} else {
				toolChoiceRaw, _ = common.Marshal(v)
			}
		}
	}

	var parallelToolCallsRaw json.RawMessage
	if req.ParallelTooCalls != nil {
		parallelToolCallsRaw, _ = common.Marshal(*req.ParallelTooCalls)
	}

	textRaw := convertChatResponseFormatToResponsesText(req.ResponseFormat)

	maxOutputTokens := req.GetMaxTokens()

	var topP *float64
	if req.TopP != nil {
		topP = common.GetPointer(lo.FromPtr(req.TopP))
	}

	out := &dto.OpenAIResponsesRequest{
		Model:                req.Model,
		Input:                inputRaw,
		Instructions:         instructionsRaw,
		Stream:               req.Stream,
		Temperature:          req.Temperature,
		Text:                 textRaw,
		ToolChoice:           toolChoiceRaw,
		Tools:                toolsRaw,
		TopP:                 topP,
		User:                 req.User,
		ParallelToolCalls:    parallelToolCallsRaw,
		Store:                req.Store,
		Metadata:             req.Metadata,
		PromptCacheOptions:   req.PromptCacheOptions,
		PromptCacheRetention: req.PromptCacheRetention,
		Moderation:           req.Moderation,
		SafetyIdentifier:     req.SafetyIdentifier,
		TopLogProbs:          req.TopLogProbs,
	}
	if req.PromptCacheKey != "" {
		out.PromptCacheKey, _ = common.Marshal(req.PromptCacheKey)
	}
	if len(req.ServiceTier) > 0 && string(req.ServiceTier) != "null" {
		if err := common.Unmarshal(req.ServiceTier, &out.ServiceTier); err != nil {
			return nil, err
		}
	}
	if req.StreamOptions != nil && req.StreamOptions.IncludeObfuscation != nil {
		out.StreamOptions = &dto.StreamOptions{IncludeObfuscation: req.StreamOptions.IncludeObfuscation}
	}
	if len(req.Verbosity) > 0 {
		var text map[string]json.RawMessage
		if len(out.Text) > 0 {
			if err := common.Unmarshal(out.Text, &text); err != nil {
				return nil, err
			}
		}
		if text == nil {
			text = make(map[string]json.RawMessage)
		}
		text["verbosity"] = req.Verbosity
		out.Text, _ = common.Marshal(text)
	}
	if req.MaxTokens != nil || req.MaxCompletionTokens != nil {
		out.MaxOutputTokens = lo.ToPtr(maxOutputTokens)
	}

	if req.ReasoningEffort != "" {
		out.Reasoning = &dto.Reasoning{
			Effort:  req.ReasoningEffort,
			Summary: "detailed",
		}
	}

	return out, nil
}

// chatContentToResponses keeps cache boundaries and media references intact.
func chatContentToResponses(msg dto.Message) ([]map[string]any, error) {
	var parts []map[string]any
	for _, part := range msg.ParseContent() {
		out := map[string]any{}
		switch part.Type {
		case dto.ContentTypeText:
			out["type"] = "input_text"
			if msg.Role == "assistant" {
				out["type"] = "output_text"
			}
			out["text"] = part.Text
		case dto.ContentTypeImageURL:
			out["type"] = "input_image"
			out["image_url"] = normalizeChatImageURLToString(part.ImageUrl)
			if img := part.GetImageMedia(); img != nil && img.Detail != "" {
				out["detail"] = img.Detail
			}
		case dto.ContentTypeFile:
			body, err := common.Marshal(part.File)
			if err != nil {
				return nil, err
			}
			if err := common.Unmarshal(body, &out); err != nil {
				return nil, err
			}
			if out == nil {
				return nil, errors.New("file reference is required")
			}
			out["type"] = "input_file"
		case "refusal":
			out["type"] = "refusal"
			out["refusal"] = part.Refusal
		default:
			return nil, fmt.Errorf("content type %q is not supported in responses compatibility mode", part.Type)
		}
		if len(part.PromptCacheBreakpoint) > 0 {
			out["prompt_cache_breakpoint"] = part.PromptCacheBreakpoint
		}
		parts = append(parts, out)
	}
	return parts, nil
}
