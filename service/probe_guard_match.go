package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/tidwall/gjson"
)

const probePingPrompt = "Call the probe_ping function with ok=true to acknowledge readiness. You must use the tool."
const maxProbeTextBytes = 64 * 1024

type probeTextRuleCache struct {
	raw   string
	rules map[string]string
}

var cachedProbeTextRules atomic.Pointer[probeTextRuleCache]

type ProbeGuardMatch struct {
	RuleID string `json:"rule_id"`
	Rule   string `json:"rule"`
}

// Preview and the relay use exactly the same normalization and complete-text comparison.
func MatchProbeGuardText(text, rules string) *ProbeGuardMatch {
	if len(text) > maxProbeTextBytes {
		return nil
	}
	cache := cachedProbeTextRules.Load()
	if cache == nil || cache.raw != rules {
		parsed, err := setting.ParseProbeGuardTextRules(rules)
		if err != nil {
			return nil
		}
		cache = &probeTextRuleCache{raw: rules, rules: parsed}
		cachedProbeTextRules.Store(cache)
	}
	normalized := setting.NormalizeProbeGuardText(text)
	if rule, found := cache.rules[normalized]; found {
		return &ProbeGuardMatch{RuleID: fmt.Sprintf("text:%x", sha256.Sum256([]byte(normalized))), Rule: rule}
	}
	return nil
}

// MatchProbeGuardRequest only examines original, single-turn requests. Tool definitions
// are allowed; actual tool messages, hidden continuation and non-text input are not.
func MatchProbeGuardRequest(request dto.Request, cfg setting.ProbeGuardSetting) *ProbeGuardMatch {
	text, ok := singleTurnProbeText(request)
	if !ok {
		return nil
	}
	if match := MatchProbeGuardText(text, cfg.TextRules); match != nil {
		return match
	}
	if cfg.ProbePingEnabled && setting.NormalizeProbeGuardText(text) == setting.NormalizeProbeGuardText(probePingPrompt) && matchesProbePingTool(request) {
		return &ProbeGuardMatch{RuleID: "tool:probe_ping", Rule: probePingPrompt}
	}
	return nil
}

func singleTurnProbeText(request dto.Request) (string, bool) {
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if r == nil || r.Prompt != nil || r.Input != nil || r.Prefix != nil || r.Suffix != nil {
			return "", false
		}
		var content any
		users := 0
		for _, message := range r.Messages {
			if hasProbeRawValue(message.ToolCalls) || hasProbeRawValue(message.FunctionCall) || message.ToolCallId != "" || hasProbeRawValue(message.Audio) {
				return "", false
			}
			switch message.Role {
			case "system", "developer":
			case "user":
				users++
				content = message.Content
			default:
				return "", false
			}
			if users > 1 {
				return "", false
			}
		}
		if users == 1 {
			return pureProbeText(content, "text")
		}
	case *dto.ClaudeRequest:
		if r == nil || r.Prompt != "" || hasProbeRawValue(r.Container) || len(r.Messages) != 1 || r.Messages[0].Role != "user" {
			return "", false
		}
		return pureProbeText(r.Messages[0].Content, "text")
	case *dto.OpenAIResponsesRequest:
		if r == nil || r.PreviousResponseID != "" || hasProbeRawValue(r.Conversation) || hasProbeRawValue(r.Prompt) {
			return "", false
		}
		input := gjson.ParseBytes(r.Input)
		if input.Type == gjson.String {
			return boundedProbeText(input.Str)
		}
		if !input.IsArray() {
			return "", false
		}
		users, valid := 0, true
		var content gjson.Result
		input.ForEach(func(_, message gjson.Result) bool {
			kind := message.Get("type").String()
			if !message.IsObject() || (kind != "" && kind != "message") || message.Get("tool_calls").Exists() || message.Get("function_call").Exists() {
				valid = false
				return false
			}
			switch message.Get("role").String() {
			case "system", "developer":
			case "user":
				users++
				content = message.Get("content")
			default:
				valid = false
			}
			return valid && users <= 1
		})
		if !valid || users != 1 {
			return "", false
		}
		if content.Type == gjson.String {
			return boundedProbeText(content.Str)
		}
		if !content.IsArray() {
			return "", false
		}
		var parts []string
		size := 0
		content.ForEach(func(_, block gjson.Result) bool {
			text := block.Get("text")
			if block.Get("type").String() != "input_text" || text.Type != gjson.String {
				valid = false
				return false
			}
			size += len(text.Str) + 1
			if size > maxProbeTextBytes {
				valid = false
				return false
			}
			parts = append(parts, text.Str)
			return true
		})
		if valid {
			return boundedProbeText(strings.Join(parts, "\n"))
		}
	}
	return "", false
}

func boundedProbeText(text string) (string, bool) {
	return text, text != "" && len(text) <= maxProbeTextBytes
}

func hasProbeRawValue(value json.RawMessage) bool {
	value = json.RawMessage(strings.TrimSpace(string(value)))
	return len(value) > 0 && string(value) != "null" && string(value) != "[]"
}

func pureProbeText(content any, textType string) (string, bool) {
	if text, ok := content.(string); ok {
		return boundedProbeText(text)
	}
	blocks, ok := content.([]any)
	if !ok || len(blocks) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(blocks))
	size := 0
	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok || block["type"] != textType {
			return "", false
		}
		text, ok := block["text"].(string)
		if !ok {
			return "", false
		}
		size += len(text) + 1
		if size > maxProbeTextBytes {
			return "", false
		}
		parts = append(parts, text)
	}
	return boundedProbeText(strings.Join(parts, "\n"))
}

func matchesProbePingTool(request dto.Request) bool {
	var name string
	var schema, choice any
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if len(r.Tools) != 1 || r.Tools[0].Type != "function" || hasProbeRawValue(r.Functions) {
			return false
		}
		name, schema, choice = r.Tools[0].Function.Name, r.Tools[0].Function.Parameters, r.ToolChoice
	case *dto.OpenAIResponsesRequest:
		var tools []struct {
			Type       string `json:"type"`
			Name       string `json:"name"`
			Parameters any    `json:"parameters"`
		}
		if common.Unmarshal(r.Tools, &tools) != nil || len(tools) != 1 || tools[0].Type != "function" || common.Unmarshal(r.ToolChoice, &choice) != nil {
			return false
		}
		name, schema = tools[0].Name, tools[0].Parameters
	case *dto.ClaudeRequest:
		tools, ok := r.Tools.([]any)
		if !ok || len(tools) != 1 || hasProbeRawValue(r.McpServers) {
			return false
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			return false
		}
		name, _ = tool["name"].(string)
		schema, choice = tool["input_schema"], r.ToolChoice
		if kind, exists := tool["type"]; exists && kind != "custom" {
			return false
		}
	default:
		return false
	}
	return name == "probe_ping" && isProbePingSchema(schema) && requiresProbePing(choice)
}

func isProbePingSchema(value any) bool {
	schema, ok := value.(map[string]any)
	if !ok || schema["type"] != "object" {
		return false
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 1 {
		return false
	}
	property, ok := properties["ok"].(map[string]any)
	if !ok || property["type"] != "boolean" {
		return false
	}
	required, ok := schema["required"].([]any)
	return ok && len(required) == 1 && required[0] == "ok"
}

func requiresProbePing(choice any) bool {
	if choice == "required" {
		return true
	}
	object, ok := choice.(map[string]any)
	if !ok {
		return false
	}
	switch object["type"] {
	case "any": // Anthropic: the only tool must be used.
		return true
	case "tool":
		return object["name"] == "probe_ping"
	case "function":
		if object["name"] == "probe_ping" {
			return true
		}
		function, ok := object["function"].(map[string]any)
		return ok && function["name"] == "probe_ping"
	}
	return false
}
