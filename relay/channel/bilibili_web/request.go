package bilibili_web

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func hasValue(raw json.RawMessage) bool {
	v := strings.TrimSpace(string(raw))
	return v != "" && v != "null"
}

func hasItems(raw json.RawMessage) bool {
	return hasValue(raw) && strings.TrimSpace(string(raw)) != "[]"
}

func validateMessage(msg dto.Message) error {
	if hasItems(msg.ToolCalls) || hasValue(msg.FunctionCall) || msg.ToolCallId != "" ||
		hasValue(msg.Audio) || msg.GetReasoningContent() != "" || msg.Reasoning != "" ||
		hasValue(msg.ReasoningDetails) || msg.Refusal != nil {
		return invalid("Bilibili Web does not accept tool, reasoning, refusal or stored audio messages")
	}
	return nil
}

func validateCommon(req *dto.GeneralOpenAIRequest) error {
	if len(req.Tools) > 0 || hasItems(req.Functions) || hasValue(req.FunctionCall) ||
		(req.ToolChoice != nil && req.ToolChoice != "none") {
		return invalid("Bilibili Web does not support tool calls")
	}
	if req.N != nil && *req.N != 1 {
		return invalid("Bilibili Web supports n=1 only")
	}
	if hasValue(req.Store) && strings.TrimSpace(string(req.Store)) != "false" {
		return invalid("Bilibili Web does not store conversations")
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type != "text" {
		return invalid("Bilibili Web supports text response_format only")
	}
	if hasValue(req.ExtraBody) || hasValue(req.WebSearch) || req.WebSearchOptions != nil {
		return invalid("unsupported Bilibili Web extension")
	}
	if req.LogProbs != nil || req.TopLogProbs != nil {
		return invalid("Bilibili Web does not expose logprobs")
	}
	if req.ParallelTooCalls != nil && *req.ParallelTooCalls {
		return invalid("Bilibili Web does not support parallel tool calls")
	}
	for _, msg := range req.Messages {
		if err := validateMessage(msg); err != nil {
			return err
		}
	}
	return nil
}

func validateTextRequest(req *dto.GeneralOpenAIRequest) error {
	if err := validateCommon(req); err != nil {
		return err
	}
	if hasValue(req.Audio) || hasValue(req.BilibiliWeb) {
		return invalid("audio and bilibili_web parameters are reserved for Echo models")
	}
	if hasValue(req.ChatTemplateKwargs) {
		var kwargs map[string]json.RawMessage
		if common.Unmarshal(req.ChatTemplateKwargs, &kwargs) != nil || kwargs == nil {
			return invalid("chat_template_kwargs must be an object")
		}
		if thinking := kwargs["enable_thinking"]; hasValue(thinking) && string(thinking) != "false" {
			return invalid("Bilibili Web translation does not support thinking; use enable_thinking=false")
		}
	}
	if hasValue(req.Modalities) {
		var modalities []string
		if common.Unmarshal(req.Modalities, &modalities) != nil || len(modalities) != 1 || modalities[0] != "text" {
			return invalid("this Bilibili Web model accepts text only")
		}
	}
	if len(req.Messages) == 0 {
		return invalid("messages are required")
	}
	for _, msg := range req.Messages {
		switch msg.Role {
		case "user", "assistant", "system", "developer":
		default:
			return invalid("unsupported message role: " + msg.Role)
		}
		raw, err := common.Marshal(msg.Content)
		if err != nil {
			return err
		}
		if _, err = textContent(raw, false); err != nil {
			return err
		}
	}
	return nil
}

// Validate the Responses subset before translating it. In particular do not
// silently discard stored conversations, tools, or non-text input items.
func responsesToChat(raw []byte) (*dto.GeneralOpenAIRequest, any, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil {
		return nil, nil, err
	}
	allowed := map[string]bool{
		"model": true, "input": true, "instructions": true, "stream": true,
		"max_output_tokens": true, "temperature": true, "top_p": true,
		"metadata": true, "user": true, "chat_template_kwargs": true,
		"stream_options": true,
	}
	for key, value := range fields {
		if allowed[key] || !hasValue(value) {
			continue
		}
		v := strings.TrimSpace(string(value))
		if (key == "store" || key == "background" || key == "parallel_tool_calls") && v == "false" {
			continue
		}
		if (key == "tools" || key == "include") && v == "[]" {
			continue
		}
		if key == "tool_choice" && v == `"none"` || key == "truncation" && v == `"disabled"` {
			continue
		}
		if key == "text" {
			var text map[string]json.RawMessage
			var format map[string]string
			if common.Unmarshal(value, &text) == nil && len(text) == 1 &&
				common.Unmarshal(text["format"], &format) == nil && len(format) == 1 && format["type"] == "text" {
				continue
			}
		}
		return nil, nil, fmt.Errorf("Bilibili Web Responses does not support %s", key)
	}
	var source dto.OpenAIResponsesRequest
	if err := common.Unmarshal(raw, &source); err != nil {
		return nil, nil, err
	}
	req := &dto.GeneralOpenAIRequest{
		Model: source.Model, Stream: source.Stream, MaxTokens: source.MaxOutputTokens,
		Temperature: source.Temperature, TopP: source.TopP,
		ChatTemplateKwargs: source.ChatTemplateKwargs, User: source.User,
	}
	if hasValue(source.Instructions) {
		var instructions string
		if err := common.Unmarshal(source.Instructions, &instructions); err != nil {
			return nil, nil, fmt.Errorf("instructions must be a string")
		}
		req.Messages = append(req.Messages, dto.Message{Role: "system", Content: instructions})
	}
	if common.GetJsonType(source.Input) == "string" {
		var input string
		if err := common.Unmarshal(source.Input, &input); err != nil {
			return nil, nil, err
		}
		req.Messages = append(req.Messages, dto.Message{Role: "user", Content: input})
	} else {
		var items []struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if common.Unmarshal(source.Input, &items) != nil || len(items) == 0 {
			return nil, nil, fmt.Errorf("input must be a string or text messages")
		}
		for _, item := range items {
			if item.Type != "" && item.Type != "message" {
				return nil, nil, fmt.Errorf("unsupported Responses input item: %s", item.Type)
			}
			content, err := textContent(item.Content, true)
			if err != nil {
				return nil, nil, err
			}
			req.Messages = append(req.Messages, dto.Message{Role: item.Role, Content: content})
		}
	}
	var metadata any = map[string]any{}
	if hasValue(source.Metadata) {
		if err := common.Unmarshal(source.Metadata, &metadata); err != nil {
			return nil, nil, err
		}
	}
	return req, metadata, nil
}

func textContent(raw json.RawMessage, responses bool) (string, error) {
	if common.GetJsonType(raw) == "string" {
		var text string
		err := common.Unmarshal(raw, &text)
		return text, err
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if common.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return "", invalid("message content must be text")
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Type != "text" && !(responses && (part.Type == "input_text" || part.Type == "output_text")) {
			return "", invalid("only text content is supported for this model")
		}
		text.WriteString(part.Text)
	}
	return text.String(), nil
}
