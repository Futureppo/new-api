package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func intPtr(v int) *int          { return &v }
func floatPtr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool       { return &v }

func TestCodecFormat(t *testing.T) {
	c := New()
	if c.Format() != ir.FormatAnthropic {
		t.Fatalf("Format() = %q, want %q", c.Format(), ir.FormatAnthropic)
	}
}

func TestDecodeRequestComprehensive(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat)

	raw := `{
		"model": "claude-sonnet-4-5",
		"max_tokens": 4096,
		"stream": true,
		"temperature": 0.7,
		"top_p": 0.9,
		"top_k": 40,
		"stop_sequences": ["END"],
		"service_tier": "auto",
		"metadata": {"user_id": "usr_123", "trace": "abc"},
		"system": [
			{"type": "text", "text": "You are concise.", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
		],
		"thinking": {"type": "enabled", "budget_tokens": 2048},
		"output_config": {"effort": "high"},
		"tools": [
			{
				"name": "get_weather",
				"description": "Lookup weather",
				"input_schema": {"type": "object", "properties": {"city": {"type": "string"}}},
				"cache_control": {"type": "ephemeral"}
			},
			{
				"type": "web_search_20250305",
				"name": "web_search",
				"max_uses": 5
			}
		],
		"tool_choice": {"type": "any", "disable_parallel_tool_use": true},
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Inspect these files", "cache_control": {"type": "ephemeral"}},
					{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "iVBORw=="}},
					{"type": "image", "source": {"type": "url", "url": "https://example.com/a.jpg"}},
					{"type": "document", "title": "notes.txt", "source": {"type": "text", "media_type": "text/plain", "data": "hello doc"}},
					{"type": "document", "title": "report.pdf", "source": {"type": "base64", "media_type": "application/pdf", "data": "JVBERi0="}}
				]
			},
			{
				"role": "assistant",
				"content": [
					{"type": "thinking", "thinking": "checking weather", "signature": "sig_1"},
					{"type": "redacted_thinking", "data": "opaque_redacted_1"},
					{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {"city": "Tokyo"}}
				]
			},
			{
				"role": "user",
				"content": [
					{
						"type": "tool_result",
						"tool_use_id": "toolu_1",
						"is_error": false,
						"cache_control": {"type": "ephemeral"},
						"content": [
							{"type": "text", "text": "Sunny 22C"},
							{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AA=="}}
						]
					},
					{"type": "text", "text": "Now summarize"}
				]
			}
		]
	}`

	req, err := c.DecodeRequest([]byte(raw), ctx)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}

	if req.Model != "claude-sonnet-4-5" || !req.Stream {
		t.Fatalf("model/stream mismatch: model=%q stream=%v", req.Model, req.Stream)
	}
	if req.Generation.MaxTokens == nil || *req.Generation.MaxTokens != 4096 {
		t.Fatalf("max_tokens mismatch: %v", req.Generation.MaxTokens)
	}
	if req.Generation.User != "usr_123" || req.Generation.ServiceTier != "auto" {
		t.Fatalf("user/service_tier mismatch: user=%q tier=%q", req.Generation.User, req.Generation.ServiceTier)
	}
	if len(req.System) != 1 || req.System[0].Text != "You are concise." || req.System[0].CacheControl == nil || req.System[0].CacheControl.TTL != "1h" {
		t.Fatalf("system mismatch: %+v", req.System)
	}
	if req.Reasoning == nil || req.Reasoning.Mode != "enabled" || req.Reasoning.BudgetTokens == nil || *req.Reasoning.BudgetTokens != 2048 || req.Reasoning.Effort != "high" {
		t.Fatalf("reasoning mismatch: %+v", req.Reasoning)
	}

	// Tools: standard function + native web_search
	if len(req.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(req.Tools))
	}
	if req.Tools[0].Type != "function" || req.Tools[0].Name != "get_weather" || req.Tools[0].CacheControl == nil {
		t.Fatalf("tool[0] mismatch: %+v", req.Tools[0])
	}
	if req.Tools[1].Type != "web_search_20250305" || req.Tools[1].OriginFormat != ir.FormatAnthropic || len(req.Tools[1].NativeRaw) == 0 {
		t.Fatalf("tool[1] native mismatch: %+v", req.Tools[1])
	}

	// ToolChoice + ParallelToolCalls
	if req.ToolChoice == nil || req.ToolChoice.Mode != "required" {
		t.Fatalf("tool_choice mismatch: %+v", req.ToolChoice)
	}
	if req.ParallelToolCalls == nil || *req.ParallelToolCalls != false {
		t.Fatalf("expected ParallelToolCalls=false, got %v", req.ParallelToolCalls)
	}

	// Messages: user(5 parts) -> assistant(thinking + redacted_thinking + tool_use) -> tool(tool_result) -> user(text)
	if len(req.Messages) != 4 {
		t.Fatalf("expected 4 IR messages after mixed tool_result+text split, got %d: %+v", len(req.Messages), req.Messages)
	}
	if len(req.Messages[0].Parts) != 5 {
		t.Fatalf("expected 5 parts in user msg 0, got %d", len(req.Messages[0].Parts))
	}
	if req.Messages[0].Parts[1].Image.Data != "iVBORw==" || req.Messages[0].Parts[2].Image.URL != "https://example.com/a.jpg" {
		t.Fatalf("image decoding mismatch: %+v %+v", req.Messages[0].Parts[1].Image, req.Messages[0].Parts[2].Image)
	}
	if req.Messages[0].Parts[3].File.Filename != "notes.txt" || req.Messages[0].Parts[3].File.Text != "hello doc" {
		t.Fatalf("text document decoding mismatch: %+v", req.Messages[0].Parts[3].File)
	}

	asst := req.Messages[1]
	if asst.Role != ir.RoleAssistant || len(asst.Parts) != 3 {
		t.Fatalf("assistant message mismatch: %+v", asst)
	}
	if asst.Parts[0].Reasoning.OriginFormat != ir.FormatAnthropic || asst.Parts[0].Reasoning.Signature != "sig_1" {
		t.Fatalf("thinking part mismatch: %+v", asst.Parts[0].Reasoning)
	}
	if asst.Parts[1].Reasoning.OriginFormat != ir.FormatAnthropic || asst.Parts[1].Reasoning.RedactedData != "opaque_redacted_1" {
		t.Fatalf("redacted_thinking part mismatch: %+v", asst.Parts[1].Reasoning)
	}
	if asst.Parts[2].ToolCall.Arguments != `{"city":"Tokyo"}` {
		t.Fatalf("tool_use arguments mismatch: %q", asst.Parts[2].ToolCall.Arguments)
	}

	toolMsg := req.Messages[2]
	if toolMsg.Role != ir.RoleTool || len(toolMsg.Parts) != 1 || toolMsg.Parts[0].Type != ir.PartToolResult {
		t.Fatalf("expected RoleTool with PartToolResult, got %+v", toolMsg)
	}
	tr := toolMsg.Parts[0].ToolResult
	if tr.ToolCallID != "toolu_1" || len(tr.Parts) != 2 || tr.Content != "Sunny 22C" {
		t.Fatalf("tool_result mismatch: %+v", tr)
	}
	if toolMsg.Parts[0].CacheControl == nil || toolMsg.Parts[0].CacheControl.Type != "ephemeral" {
		t.Fatalf("tool_result cache_control lost")
	}

	followUser := req.Messages[3]
	if followUser.Role != ir.RoleUser || len(followUser.Parts) != 1 || followUser.Parts[0].Text != "Now summarize" {
		t.Fatalf("trailing user part mismatch: %+v", followUser)
	}
}

func TestEncodeRequestMessageMergingToolFoldingAndInvariants(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic)
	ctx.Options["default_max_tokens"] = 4096

	req := &ir.Request{
		Model: "claude-sonnet-4-5",
		System: []ir.ContentPart{
			{Type: ir.PartText, Text: "Base system"},
		},
		Messages: []ir.Message{
			{
				Role:  ir.RoleDeveloper,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Dev rule", CacheControl: &ir.CacheControl{Type: "ephemeral"}}},
			},
			// Starts with assistant -> must prepend synthetic user message!
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "call_1", Name: "fn_a", Arguments: `{"x":1}`}},
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "call_2", Name: "fn_b", Arguments: `not valid json`}},
				},
			},
			// Adjacent RoleTool + RoleTool + RoleUser -> MUST fold into ONE user message!
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "call_1", Content: "res_1"}},
				},
			},
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "call_2", Content: "err_2", IsError: true}},
				},
			},
			{
				Role: ir.RoleUser,
				Parts: []ir.ContentPart{
					{Type: ir.PartText, Text: "Follow up"},
				},
			},
			// Empty assistant message -> must become [{"type":"text","text":"..."}]
			{
				Role:  ir.RoleAssistant,
				Parts: nil,
			},
		},
		Tools: []ir.ToolDefinition{
			{
				Type:       "function",
				Name:       "fn_a",
				Parameters: json.RawMessage(`{"properties":{"x":{"type":"integer"}}}`), // missing "type":"object"
			},
			{
				Type:         "web_search_20250305",
				Name:         "web_search",
				OriginFormat: ir.FormatAnthropic,
				NativeRaw:    json.RawMessage(`{"type":"web_search_20250305","name":"web_search","max_uses":3}`),
			},
			{
				Type:         "file_search",
				Name:         "file_search",
				OriginFormat: ir.FormatOpenAIResponses,
				NativeRaw:    json.RawMessage(`{"type":"file_search"}`),
			},
		},
		ToolChoice: &ir.ToolChoice{
			Mode:            "none",
			DisableParallel: boolPtr(true), // MUST be omitted when type=="none"
		},
		Generation: ir.GenerationConfig{
			Temperature: floatPtr(1.5), // MUST clamp to 1.0
		},
	}

	raw, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}

	// Default max_tokens from ctx option
	if int(parsed["max_tokens"].(float64)) != 4096 {
		t.Fatalf("max_tokens = %v, want 4096", parsed["max_tokens"])
	}

	// Clamped temperature
	if parsed["temperature"].(float64) != 1.0 {
		t.Fatalf("temperature = %v, want 1.0", parsed["temperature"])
	}

	// System array (2 blocks, second has cache_control)
	sysBlocks, ok := parsed["system"].([]any)
	if !ok || len(sysBlocks) != 2 {
		t.Fatalf("expected 2 system blocks, got %#v", parsed["system"])
	}

	// Tool choice: type=="none" MUST omit disable_parallel_tool_use
	tcMap := parsed["tool_choice"].(map[string]any)
	if tcMap["type"] != "none" {
		t.Fatalf("tool_choice.type = %v, want none", tcMap["type"])
	}
	if _, exists := tcMap["disable_parallel_tool_use"]; exists {
		t.Fatalf("disable_parallel_tool_use MUST be omitted when tool_choice.type == none")
	}

	// Tools: fn_a schema has type=object, Anthropic native kept, OpenAI foreign dropped
	tools := parsed["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools (1 standard + 1 anthropic native), got %d", len(tools))
	}
	fnSchema := tools[0].(map[string]any)["input_schema"].(map[string]any)
	if fnSchema["type"] != "object" {
		t.Fatalf("input_schema.type = %v, want object", fnSchema["type"])
	}
	if tools[1].(map[string]any)["type"] != "web_search_20250305" {
		t.Fatalf("expected anthropic native tool passthrough, got %#v", tools[1])
	}

	// Messages: [synthetic user, assistant(2 tool_use), merged user(2 tool_result + text), assistant("...")]
	msgs := parsed["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d: %s", len(msgs), string(raw))
	}
	wantRoles := []string{"user", "assistant", "user", "assistant"}
	for i, want := range wantRoles {
		if got := msgs[i].(map[string]any)["role"]; got != want {
			t.Fatalf("messages[%d].role = %v, want %q", i, got, want)
		}
	}

	// Synthetic first user content
	firstContent := msgs[0].(map[string]any)["content"].([]any)
	if firstContent[0].(map[string]any)["text"] != "..." {
		t.Fatalf("expected synthetic placeholder text, got %#v", firstContent)
	}

	// Invalid JSON arguments on call_2 normalized to {}
	asstContent := msgs[1].(map[string]any)["content"].([]any)
	call2Input := asstContent[1].(map[string]any)["input"].(map[string]any)
	if len(call2Input) != 0 {
		t.Fatalf("expected invalid tool args to become empty {}, got %#v", call2Input)
	}

	// Folded user message has 3 blocks: tool_result, tool_result(is_error=true), text
	foldedContent := msgs[2].(map[string]any)["content"].([]any)
	if len(foldedContent) != 3 {
		t.Fatalf("expected 3 folded blocks, got %d: %#v", len(foldedContent), foldedContent)
	}
	if foldedContent[0].(map[string]any)["type"] != "tool_result" ||
		foldedContent[1].(map[string]any)["type"] != "tool_result" ||
		foldedContent[1].(map[string]any)["is_error"] != true ||
		foldedContent[2].(map[string]any)["type"] != "text" {
		t.Fatalf("unexpected folded user content: %#v", foldedContent)
	}

	// Empty assistant message replaced with placeholder text
	emptyAsstContent := msgs[3].(map[string]any)["content"].([]any)
	if len(emptyAsstContent) != 1 || emptyAsstContent[0].(map[string]any)["text"] != "..." {
		t.Fatalf("expected placeholder on empty assistant message, got %#v", emptyAsstContent)
	}
}

func TestEncodeRequestForeignThinkingStrippingAndThinkingConstraints(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatAnthropic)

	req := &ir.Request{
		Model: "claude-sonnet-4-5",
		Generation: ir.GenerationConfig{
			MaxTokens:   intPtr(4096),
			Temperature: floatPtr(0.5),
			TopP:        floatPtr(0.8),
			TopK:        intPtr(20),
		},
		Reasoning: &ir.ReasoningConfig{
			Mode:         "enabled",
			BudgetTokens: intPtr(6000), // >= max_tokens (4096) -> MUST bump max_tokens above 6000!
			Effort:       "high",
		},
		Messages: []ir.Message{
			{
				Role:  ir.RoleUser,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Solve this"}},
			},
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					// Foreign OpenAI Responses encrypted reasoning -> MUST be dropped!
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "o3 thought",
							RedactedData: "gAAAA_openai_encrypted",
							OriginFormat: ir.FormatOpenAIResponses,
						},
					},
					// Foreign DeepSeek unsigned reasoning -> MUST be dropped!
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "deepseek reasoning",
							OriginFormat: ir.FormatOpenAIChat,
						},
					},
					// Unsigned Anthropic reasoning -> MUST be dropped!
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "missing signature",
							OriginFormat: ir.FormatAnthropic,
						},
					},
					// Valid signed Anthropic thinking -> MUST be kept!
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "valid anthropic thought",
							Signature:    "sig_anthropic_valid",
							OriginFormat: ir.FormatAnthropic,
						},
					},
					// Valid Anthropic redacted_thinking -> MUST be kept!
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							RedactedData: "anthropic_redacted_blob",
							OriginFormat: ir.FormatAnthropic,
						},
					},
					{Type: ir.PartText, Text: "Final answer"},
				},
			},
		},
	}

	raw, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}

	// max_tokens bumped strictly above budget_tokens (6000)
	maxTok := int(parsed["max_tokens"].(float64))
	if maxTok <= 6000 {
		t.Fatalf("expected max_tokens > 6000, got %d", maxTok)
	}

	// temperature adjusted to 1.0 and top_p/top_k dropped for enabled thinking
	if parsed["temperature"].(float64) != 1.0 {
		t.Fatalf("expected temperature=1.0 with thinking enabled, got %v", parsed["temperature"])
	}
	if _, ok := parsed["top_p"]; ok {
		t.Fatalf("expected top_p to be dropped with thinking enabled")
	}
	if _, ok := parsed["top_k"]; ok {
		t.Fatalf("expected top_k to be dropped with thinking enabled")
	}

	// Assistant message should only have valid thinking + valid redacted_thinking + text (3 blocks)
	msgs := parsed["messages"].([]any)
	asstContent := msgs[1].(map[string]any)["content"].([]any)
	if len(asstContent) != 3 {
		t.Fatalf("expected 3 content blocks after stripping foreign/unsigned reasoning, got %d: %#v", len(asstContent), asstContent)
	}
	if asstContent[0].(map[string]any)["type"] != "thinking" || asstContent[0].(map[string]any)["signature"] != "sig_anthropic_valid" {
		t.Fatalf("block[0] mismatch: %#v", asstContent[0])
	}
	if asstContent[1].(map[string]any)["type"] != "redacted_thinking" || asstContent[1].(map[string]any)["data"] != "anthropic_redacted_blob" {
		t.Fatalf("block[1] mismatch: %#v", asstContent[1])
	}
	if asstContent[2].(map[string]any)["type"] != "text" || asstContent[2].(map[string]any)["text"] != "Final answer" {
		t.Fatalf("block[2] mismatch: %#v", asstContent[2])
	}

	// Verify 3 dropped_foreign_reasoning warnings were recorded
	droppedCount := 0
	for _, w := range ctx.Warnings {
		if w.Code == "dropped_foreign_reasoning" {
			droppedCount++
		}
	}
	if droppedCount != 3 {
		t.Fatalf("expected 3 dropped_foreign_reasoning warnings, got %d (%+v)", droppedCount, ctx.Warnings)
	}
}

func TestResponseDecodeAndEncodeExclusiveUsageAnd5m1hSplit(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatAnthropic)

	rawResp := `{
		"id": "msg_01XYZ",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-5",
		"content": [
			{"type": "thinking", "thinking": "step by step", "signature": "sig_resp_1"},
			{"type": "redacted_thinking", "data": "redacted_resp_1"},
			{
				"type": "text",
				"text": "The answer is 42.",
				"citations": [
					{"type": "web_search_result_location", "url": "https://example.com", "title": "Guide", "cited_text": "42"}
				]
			},
			{"type": "tool_use", "id": "toolu_abc", "name": "calc", "input": {"expr": "6*7"}}
		],
		"stop_reason": "tool_use",
		"stop_sequence": null,
		"usage": {
			"input_tokens": 120,
			"output_tokens": 80,
			"cache_read_input_tokens": 500,
			"cache_creation_input_tokens": 300,
			"cache_creation": {
				"ephemeral_5m_input_tokens": 100,
				"ephemeral_1h_input_tokens": 200
			},
			"server_tool_use": {
				"web_search_requests": 2
			}
		}
	}`

	resp, err := c.DecodeResponse([]byte(rawResp), ctx)
	if err != nil {
		t.Fatalf("DecodeResponse error: %v", err)
	}

	if resp.ID != "msg_01XYZ" || resp.Model != "claude-sonnet-4-5" {
		t.Fatalf("id/model mismatch: %+v", resp)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].FinishReason != ir.FinishToolCalls {
		t.Fatalf("choice/finish mismatch: %+v", resp.Choices)
	}

	parts := resp.Choices[0].Message.Parts
	if len(parts) != 5 { // thinking, redacted_thinking, text, citation, tool_use
		t.Fatalf("expected 5 parts, got %d: %+v", len(parts), parts)
	}
	if parts[0].Reasoning.OriginFormat != ir.FormatAnthropic || parts[0].Reasoning.Signature != "sig_resp_1" {
		t.Fatalf("thinking part mismatch: %+v", parts[0].Reasoning)
	}
	if parts[3].Type != ir.PartCitation || parts[3].Citation.URL != "https://example.com" {
		t.Fatalf("citation mismatch: %+v", parts[3])
	}

	u := resp.Usage
	if u == nil || !u.InputExclusive {
		t.Fatalf("expected Usage with InputExclusive=true, got %+v", u)
	}
	if u.InputTokens != 120 || u.OutputTokens != 80 || u.CachedReadTokens != 500 || u.CacheCreationTokens != 300 {
		t.Fatalf("token counts mismatch: %+v", u)
	}
	if u.CacheCreation5mTokens != 100 || u.CacheCreation1hTokens != 200 {
		t.Fatalf("5m/1h split mismatch: 5m=%d 1h=%d", u.CacheCreation5mTokens, u.CacheCreation1hTokens)
	}
	if u.WebSearchRequests != 2 {
		t.Fatalf("web_search_requests = %d, want 2", u.WebSearchRequests)
	}
	if u.ExclusiveInputTokens() != 120 || u.InclusiveInputTokens() != 920 || u.TotalTokens != 1000 {
		t.Fatalf("exclusive/inclusive/total mismatch: excl=%d incl=%d total=%d",
			u.ExclusiveInputTokens(), u.InclusiveInputTokens(), u.TotalTokens)
	}

	// Verify untagged cache creation defaults to 5m via ir.NormalizeCacheCreationSplit
	rawDefault5m := `{"id":"msg_2","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":250}}`
	resp5m, err := c.DecodeResponse([]byte(rawDefault5m), ctx)
	if err != nil {
		t.Fatalf("DecodeResponse default 5m error: %v", err)
	}
	if resp5m.Usage.CacheCreation5mTokens != 250 || resp5m.Usage.CacheCreation1hTokens != 0 {
		t.Fatalf("expected untagged cache creation to default to 5m=250 1h=0, got 5m=%d 1h=%d",
			resp5m.Usage.CacheCreation5mTokens, resp5m.Usage.CacheCreation1hTokens)
	}

	// EncodeResponse from an OpenAI-inclusive Usage (InputExclusive=false)
	openAIResp := &ir.Response{
		ID:    "chatcmpl-999",
		Model: "claude-sonnet-4-5",
		Choices: []ir.Choice{{
			FinishReason: ir.FinishStop,
			Message: ir.Message{
				Role:  ir.RoleAssistant,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Hello!"}},
			},
		}},
		Usage: &ir.Usage{
			InputExclusive:      false,
			InputTokens:         1000, // inclusive of 400 cached read + 100 cache write -> exclusive = 500
			OutputTokens:        50,
			CachedReadTokens:    400,
			CacheCreationTokens: 100,
		},
	}

	encoded, err := c.EncodeResponse(openAIResp, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeResponse error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal encoded response: %v", err)
	}
	if !strings.HasPrefix(wire["id"].(string), "msg_") {
		t.Fatalf("expected id prefixed with msg_, got %q", wire["id"])
	}
	if wire["type"] != "message" || wire["role"] != "assistant" || wire["stop_reason"] != "end_turn" {
		t.Fatalf("envelope mismatch: %#v", wire)
	}
	if stopSeq, exists := wire["stop_sequence"]; !exists || stopSeq != nil {
		t.Fatalf("expected explicit stop_sequence: null, exists=%v val=%#v", exists, stopSeq)
	}
	usageMap := wire["usage"].(map[string]any)
	if int(usageMap["input_tokens"].(float64)) != 500 ||
		int(usageMap["output_tokens"].(float64)) != 50 ||
		int(usageMap["cache_read_input_tokens"].(float64)) != 400 ||
		int(usageMap["cache_creation_input_tokens"].(float64)) != 100 {
		t.Fatalf("encoded usage mismatch: %#v", usageMap)
	}
}

func TestStreamFullLifecycleEncodeAndDecode(t *testing.T) {
	c := New()

	// 1. Test EncodeStreamEvent + FlushStream full lifecycle:
	//    message_start -> thinking (+signature) -> text -> tool_use -> message_delta -> message_stop
	encCtx := ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatAnthropic)
	encCtx.Model = "claude-sonnet-4-5"

	inputEvents := []ir.StreamEvent{
		{Type: ir.EventStreamStart, ResponseID: "chatcmpl-stream1", Model: "claude-sonnet-4-5"},
		{Type: ir.EventUsage, Usage: &ir.Usage{
			InputExclusive:      false,
			InputTokens:         600,
			CachedReadTokens:    200,
			CacheCreationTokens: 100,
		}},
		{Type: ir.EventReasoningDelta, Reasoning: "First I think.", OriginFormat: ir.FormatAnthropic},
		{Type: ir.EventReasoningSignatureDelta, Signature: "sig_stream_1", OriginFormat: ir.FormatAnthropic},
		{Type: ir.EventTextDelta, Text: "Calling tool now."},
		{Type: ir.EventToolCallStart, ToolCallIndex: 0, ToolCallID: "toolu_stream_1", ToolName: "get_weather"},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "toolu_stream_1", ToolArguments: `{"city":`},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "toolu_stream_1", ToolArguments: `"Osaka"}`},
		{Type: ir.EventFinish, FinishReason: ir.FinishToolCalls},
		// Trailing usage arriving AFTER EventFinish (as in OpenAI Chat stream_options.include_usage)
		{Type: ir.EventUsage, Usage: &ir.Usage{OutputTokens: 45}},
	}

	var frames []ir.SSEFrame
	for _, ev := range inputEvents {
		out, err := c.EncodeStreamEvent(ev, encCtx)
		if err != nil {
			t.Fatalf("EncodeStreamEvent(%s) error: %v", ev.Type, err)
		}
		frames = append(frames, out...)
	}
	tail, err := c.FlushStream(encCtx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	frames = append(frames, tail...)

	// Calling FlushStream a second time must be a no-op
	if dup, err := c.FlushStream(encCtx); err != nil || len(dup) != 0 {
		t.Fatalf("expected second FlushStream to be no-op, got %d frames err=%v", len(dup), err)
	}

	wantEventSeq := []string{
		"message_start",
		"content_block_start", // index 0: thinking
		"content_block_delta", // index 0: thinking_delta
		"content_block_delta", // index 0: signature_delta
		"content_block_stop",  // index 0
		"content_block_start", // index 1: text
		"content_block_delta", // index 1: text_delta
		"content_block_stop",  // index 1
		"content_block_start", // index 2: tool_use
		"content_block_delta", // index 2: input_json_delta
		"content_block_delta", // index 2: input_json_delta
		"content_block_stop",  // index 2
		"message_delta",
		"message_stop",
	}
	if len(frames) != len(wantEventSeq) {
		var got []string
		for _, f := range frames {
			got = append(got, f.Event)
		}
		t.Fatalf("frame count mismatch: got %d (%v), want %d (%v)", len(frames), got, len(wantEventSeq), wantEventSeq)
	}
	for i, wantEv := range wantEventSeq {
		if frames[i].Event != wantEv {
			t.Fatalf("frame[%d].Event = %q, want %q (data=%s)", i, frames[i].Event, wantEv, string(frames[i].Data))
		}
	}

	// Verify message_start has msg_ prefix
	var msgStart map[string]any
	if err := json.Unmarshal(frames[0].Data, &msgStart); err != nil {
		t.Fatalf("unmarshal message_start: %v", err)
	}
	if !strings.HasPrefix(msgStart["message"].(map[string]any)["id"].(string), "msg_") {
		t.Fatalf("expected msg_ prefix in message_start, got %#v", msgStart)
	}

	// Verify message_delta carries stop_reason="tool_use" and merged exclusive usage
	var msgDelta map[string]any
	if err := json.Unmarshal(frames[len(frames)-2].Data, &msgDelta); err != nil {
		t.Fatalf("unmarshal message_delta: %v", err)
	}
	if msgDelta["delta"].(map[string]any)["stop_reason"] != "tool_use" {
		t.Fatalf("message_delta stop_reason = %v, want tool_use", msgDelta["delta"])
	}
	du := msgDelta["usage"].(map[string]any)
	// Exclusive input = 600 - 200 - 100 = 300; output = 45
	if int(du["input_tokens"].(float64)) != 300 ||
		int(du["output_tokens"].(float64)) != 45 ||
		int(du["cache_read_input_tokens"].(float64)) != 200 ||
		int(du["cache_creation_input_tokens"].(float64)) != 100 {
		t.Fatalf("message_delta merged usage mismatch: %#v", du)
	}

	// 2. Round-trip: feed the emitted Anthropic SSE frames back into DecodeStreamEvent
	//    and verify all semantic events + block indices + tool args + exclusive usage!
	decCtx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	var decoded []ir.StreamEvent
	for _, f := range frames {
		evs, err := c.DecodeStreamEvent(f.Event, f.Data, decCtx)
		if err != nil {
			t.Fatalf("DecodeStreamEvent(%s) error: %v", f.Event, err)
		}
		for _, ev := range evs {
			if ev.Usage != nil {
				decCtx.MergeUsage(ev.Usage)
			}
			decoded = append(decoded, ev)
		}
	}

	// Also verify ping and error decoding
	if pingEvs, err := c.DecodeStreamEvent("ping", []byte(`{"type":"ping"}`), decCtx); err != nil || len(pingEvs) != 0 {
		t.Fatalf("expected ping to yield nil events, got %+v err=%v", pingEvs, err)
	}
	errEvs, err := c.DecodeStreamEvent("error", []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`), decCtx)
	if err != nil || len(errEvs) != 1 || errEvs[0].Type != ir.EventError || errEvs[0].ErrorType != "overloaded_error" {
		t.Fatalf("error event decode mismatch: %+v err=%v", errEvs, err)
	}

	var gotThinking, gotSig, gotText, gotToolArgs strings.Builder
	var gotToolID, gotToolName string
	var gotFinish ir.FinishReason
	sawStreamEnd := false
	for _, ev := range decoded {
		switch ev.Type {
		case ir.EventReasoningDelta:
			gotThinking.WriteString(ev.Reasoning)
		case ir.EventReasoningSignatureDelta:
			gotSig.WriteString(ev.Signature)
		case ir.EventTextDelta:
			gotText.WriteString(ev.Text)
		case ir.EventToolCallStart:
			gotToolID = ev.ToolCallID
			gotToolName = ev.ToolName
		case ir.EventToolCallDelta:
			if ev.ToolCallID != "toolu_stream_1" {
				t.Fatalf("expected ToolCallDelta to carry correlated ToolCallID toolu_stream_1, got %q", ev.ToolCallID)
			}
			gotToolArgs.WriteString(ev.ToolArguments)
		case ir.EventFinish:
			gotFinish = ev.FinishReason
		case ir.EventStreamEnd:
			sawStreamEnd = true
		}
	}

	if gotThinking.String() != "First I think." || gotSig.String() != "sig_stream_1" {
		t.Fatalf("decoded thinking/sig mismatch: %q / %q", gotThinking.String(), gotSig.String())
	}
	if gotText.String() != "Calling tool now." {
		t.Fatalf("decoded text mismatch: %q", gotText.String())
	}
	if gotToolID != "toolu_stream_1" || gotToolName != "get_weather" || gotToolArgs.String() != `{"city":"Osaka"}` {
		t.Fatalf("decoded tool call mismatch: id=%q name=%q args=%q", gotToolID, gotToolName, gotToolArgs.String())
	}
	if gotFinish != ir.FinishToolCalls || !sawStreamEnd {
		t.Fatalf("decoded finish/end mismatch: finish=%q ended=%v", gotFinish, sawStreamEnd)
	}
	if decCtx.LastUsage == nil || !decCtx.LastUsage.InputExclusive || decCtx.LastUsage.InputTokens != 300 || decCtx.LastUsage.OutputTokens != 45 || decCtx.LastUsage.TotalTokens != 645 {
		t.Fatalf("decoded merged usage mismatch: %+v", decCtx.LastUsage)
	}
}

func TestAdaptiveThinkingToolChoiceDataURLAndPipelineStreamRoundTrip(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatAnthropic)

	// 1. Adaptive thinking + display + string system + tool_choice "tool"
	rawAdaptive := `{
		"model": "claude-opus-4-7",
		"max_tokens": 2048,
		"system": "Be helpful.",
		"thinking": {"type": "adaptive", "display": "summarized"},
		"output_config": {"effort": "max"},
		"tools": [{"name": "search", "input_schema": {"type": "object", "properties": {}}}],
		"tool_choice": {"type": "tool", "name": "search", "disable_parallel_tool_use": true},
		"messages": [{"role": "user", "content": "Hi"}]
	}`
	req, err := c.DecodeRequest([]byte(rawAdaptive), ctx)
	if err != nil {
		t.Fatalf("DecodeRequest adaptive error: %v", err)
	}
	if req.Reasoning == nil || req.Reasoning.Mode != "adaptive" || req.Reasoning.Display != "summarized" || req.Reasoning.Effort != "max" {
		t.Fatalf("adaptive reasoning mismatch: %+v", req.Reasoning)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "tool" || req.ToolChoice.ToolName != "search" {
		t.Fatalf("tool_choice tool mismatch: %+v", req.ToolChoice)
	}

	// Add a data-URL image part (OpenAI Chat style) and re-encode
	req.Messages[0].Parts = append(req.Messages[0].Parts, ir.ContentPart{
		Type:  ir.PartImage,
		Image: &ir.ImageSource{URL: "data:image/jpeg;base64,/9j/4AAQ"},
	})
	encBytes, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest adaptive error: %v", err)
	}
	var encMap map[string]any
	if err := json.Unmarshal(encBytes, &encMap); err != nil {
		t.Fatalf("unmarshal adaptive encoded request: %v", err)
	}
	if encMap["system"] != "Be helpful." {
		t.Fatalf("expected plain string system, got %#v", encMap["system"])
	}
	th := encMap["thinking"].(map[string]any)
	if th["type"] != "adaptive" || th["display"] != "summarized" {
		t.Fatalf("encoded adaptive thinking mismatch: %#v", th)
	}
	oc := encMap["output_config"].(map[string]any)
	if oc["effort"] != "max" {
		t.Fatalf("encoded output_config mismatch: %#v", oc)
	}
	tc := encMap["tool_choice"].(map[string]any)
	if tc["type"] != "tool" || tc["name"] != "search" || tc["disable_parallel_tool_use"] != true {
		t.Fatalf("encoded tool_choice mismatch: %#v", tc)
	}
	msgContent := encMap["messages"].([]any)[0].(map[string]any)["content"].([]any)
	imgSrc := msgContent[1].(map[string]any)["source"].(map[string]any)
	if imgSrc["type"] != "base64" || imgSrc["media_type"] != "image/jpeg" || imgSrc["data"] != "/9j/4AAQ" {
		t.Fatalf("expected data URL to unwrap to base64 image source, got %#v", imgSrc)
	}

	// 2. End-to-end ir.Pipeline StreamTranslator (Anthropic -> Anthropic) including redacted_thinking & 5m/1h cache usage
	reg := ir.NewRegistry()
	reg.Register(c)
	pipe, err := ir.NewPipeline(ir.PipelineConfig{
		Registry:     reg,
		SourceFormat: ir.FormatAnthropic,
		TargetFormat: ir.FormatAnthropic,
	})
	if err != nil {
		t.Fatalf("NewPipeline error: %v", err)
	}

	sseInput := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_pipe_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":50,"output_tokens":1,"cache_read_input_tokens":100,"cache_creation_input_tokens":80,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":50}}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"redacted_xyz"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Done!"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: ping`,
		`data: {"type":"ping"}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"stop_sequence","stop_sequence":"STOP"},"usage":{"output_tokens":20}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
		``,
	}, "\n")

	translator := pipe.NewStreamTranslator("claude-sonnet-4-5")
	var emitted []ir.SSEFrame
	if err := translator.TranslateReader(strings.NewReader(sseInput), func(f ir.SSEFrame) error {
		emitted = append(emitted, f)
		return nil
	}); err != nil {
		t.Fatalf("TranslateReader error: %v", err)
	}

	usage := translator.Usage()
	if usage == nil || !usage.InputExclusive || usage.InputTokens != 50 || usage.OutputTokens != 20 ||
		usage.CachedReadTokens != 100 || usage.CacheCreationTokens != 80 ||
		usage.CacheCreation5mTokens != 30 || usage.CacheCreation1hTokens != 50 ||
		usage.TotalTokens != 250 {
		t.Fatalf("pipeline stream usage mismatch: %+v", usage)
	}

	// Verify final message_delta preserved stop_sequence and message_stop is last
	if len(emitted) < 2 || emitted[len(emitted)-1].Event != "message_stop" || emitted[len(emitted)-2].Event != "message_delta" {
		t.Fatalf("unexpected trailing emitted frames: %+v", emitted)
	}
	var lastDelta map[string]any
	if err := json.Unmarshal(emitted[len(emitted)-2].Data, &lastDelta); err != nil {
		t.Fatalf("unmarshal last message_delta: %v", err)
	}
	if lastDelta["delta"].(map[string]any)["stop_reason"] != "stop_sequence" {
		t.Fatalf("expected stop_sequence preserved, got %#v", lastDelta["delta"])
	}
}
