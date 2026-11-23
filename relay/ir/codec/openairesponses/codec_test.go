package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/ir"
)

func TestCodecFormat(t *testing.T) {
	c := New()
	if c.Format() != ir.FormatOpenAIResponses {
		t.Fatalf("expected format %q, got %q", ir.FormatOpenAIResponses, c.Format())
	}
}

func TestDecodeEncodeRequestSimpleStringInputAndParams(t *testing.T) {
	c := New()
	raw := []byte(`{
		"model": "gpt-5",
		"instructions": "You are a concise assistant.",
		"input": "Hello world",
		"max_output_tokens": 512,
		"temperature": 0.5,
		"top_p": 0.9,
		"user": "usr_42",
		"store": false,
		"truncation": "auto",
		"previous_response_id": "resp_prev_1",
		"service_tier": "priority",
		"stream": true,
		"include": ["reasoning.encrypted_content"],
		"metadata": {"env": "test"},
		"prompt_cache_key": "cache-k1",
		"prompt_cache_retention": "24h",
		"reasoning": {"effort": "high", "summary": "detailed"},
		"text": {
			"verbosity": "low",
			"format": {
				"type": "json_schema",
				"name": "answer_schema",
				"description": "Structured answer",
				"schema": {"type": "object", "properties": {"ok": {"type": "boolean"}}},
				"strict": true
			}
		}
	}`)

	ctx := ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatOpenAIResponses)
	req, err := c.DecodeRequest(raw, ctx)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}

	if req.Model != "gpt-5" || !req.Stream {
		t.Fatalf("unexpected model/stream: %+v", req)
	}
	if got := ir.JoinTextParts(req.System, "\n"); got != "You are a concise assistant." {
		t.Fatalf("unexpected instructions: %q", got)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != ir.RoleUser || req.Messages[0].Parts[0].Text != "Hello world" {
		t.Fatalf("unexpected decoded messages: %+v", req.Messages)
	}
	if req.Generation.MaxTokens == nil || *req.Generation.MaxTokens != 512 {
		t.Fatalf("unexpected max_output_tokens: %v", req.Generation.MaxTokens)
	}
	if req.Generation.Store == nil || *req.Generation.Store != false {
		t.Fatalf("unexpected store: %v", req.Generation.Store)
	}
	if req.Generation.PreviousResponseID != "resp_prev_1" || req.Generation.Truncation != "auto" || req.Generation.ServiceTier != "priority" {
		t.Fatalf("unexpected generation config: %+v", req.Generation)
	}
	if req.Generation.PromptCacheKey != "cache-k1" || req.Generation.PromptCacheRetention != "24h" {
		t.Fatalf("unexpected cache config: %+v", req.Generation)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" || req.Reasoning.Summary != "detailed" {
		t.Fatalf("unexpected reasoning config: %+v", req.Reasoning)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.Name != "answer_schema" || req.ResponseFormat.Strict == nil || !*req.ResponseFormat.Strict {
		t.Fatalf("unexpected response format: %+v", req.ResponseFormat)
	}
	if req.Generation.Verbosity != "low" {
		t.Fatalf("expected verbosity low, got %q", req.Generation.Verbosity)
	}

	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	var out map[string]any
	if err := common.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}
	if out["instructions"] != "You are a concise assistant." {
		t.Fatalf("expected instructions preserved, got %v", out["instructions"])
	}
	if out["max_output_tokens"] != float64(512) || out["store"] != false || out["prompt_cache_key"] != "cache-k1" {
		t.Fatalf("encoded request fields mismatch: %+v", out)
	}
	textObj, _ := out["text"].(map[string]any)
	if textObj["verbosity"] != "low" {
		t.Fatalf("expected text.verbosity=low, got %+v", textObj)
	}
	fmtObj, _ := textObj["format"].(map[string]any)
	if fmtObj["type"] != "json_schema" || fmtObj["name"] != "answer_schema" {
		t.Fatalf("unexpected text.format: %+v", fmtObj)
	}
}

func TestDecodeEncodeRequestComplexItemsToolsAndMultimodal(t *testing.T) {
	c := New()
	raw := []byte(`{
		"model": "gpt-5",
		"input": [
			{"role": "developer", "content": "Follow JSON rules"},
			{
				"type": "message",
				"role": "user",
				"content": [
					{"type": "input_text", "text": "Analyze this image and PDF"},
					{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo=", "detail": "high"},
					{"type": "input_image", "image_url": "https://example.com/pic.jpg", "detail": "low"},
					{"type": "input_file", "filename": "doc.pdf", "file_data": "data:application/pdf;base64,JVBERi0="}
				]
			},
			{
				"type": "reasoning",
				"id": "rs_001",
				"summary": [{"type": "summary_text", "text": "Plan step 1"}],
				"content": [{"type": "reasoning_text", "text": "Detailed internal thought"}],
				"encrypted_content": "enc_blob_responses_1"
			},
			{
				"type": "message",
				"role": "assistant",
				"content": [
					{"type": "output_text", "text": "Calling weather tool"},
					{"type": "refusal", "refusal": "partial refusal"}
				]
			},
			{
				"type": "function_call",
				"id": "fc_001",
				"call_id": "call_weather_1",
				"name": "get_weather",
				"arguments": "{\"city\":\"Tokyo\"}"
			},
			{
				"type": "custom_tool_call",
				"id": "ctc_002",
				"call_id": "call_sql_2",
				"name": "run_sql",
				"input": "SELECT 1"
			},
			{
				"type": "function_call_output",
				"call_id": "call_weather_1",
				"output": [
					{"type": "input_text", "text": "Sunny 24C"},
					{"type": "input_image", "image_url": "data:image/jpeg;base64,/9j/4AAQ", "detail": "auto"}
				]
			},
			{
				"type": "custom_tool_call_output",
				"call_id": "call_sql_2",
				"output": "1"
			},
			{
				"type": "item_reference",
				"id": "item_ref_99"
			}
		],
		"tools": [
			{
				"type": "function",
				"name": "get_weather",
				"description": "Get city weather",
				"parameters": {"type": "object", "properties": {"city": {"type": "string"}}},
				"strict": true
			},
			{
				"type": "custom",
				"name": "run_sql",
				"description": "Execute SQL",
				"format": {"type": "text"}
			},
			{
				"type": "web_search_preview",
				"search_context_size": "medium"
			}
		],
		"tool_choice": {"type": "function", "name": "get_weather"},
		"parallel_tool_calls": true
	}`)

	ctx := ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatOpenAIResponses)
	req, err := c.DecodeRequest(raw, ctx)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}

	// Expected messages: developer, user, assistant (coalesced reasoning + msg + 2 tool calls), tool1, tool2
	if len(req.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != ir.RoleDeveloper {
		t.Fatalf("expected message[0] developer, got %s", req.Messages[0].Role)
	}

	userParts := req.Messages[1].Parts
	if len(userParts) != 4 {
		t.Fatalf("expected 4 user parts, got %d", len(userParts))
	}
	if userParts[1].Type != ir.PartImage || userParts[1].Image.Data != "iVBORw0KGgo=" || userParts[1].Image.MediaType != "image/png" || userParts[1].Image.Detail != "high" {
		t.Fatalf("unexpected base64 image part: %+v", userParts[1].Image)
	}
	if userParts[2].Type != ir.PartImage || userParts[2].Image.URL != "https://example.com/pic.jpg" {
		t.Fatalf("unexpected url image part: %+v", userParts[2].Image)
	}
	if userParts[3].Type != ir.PartFile || userParts[3].File.Filename != "doc.pdf" || userParts[3].File.MediaType != "application/pdf" || userParts[3].File.Data != "JVBERi0=" {
		t.Fatalf("unexpected file part: %+v", userParts[3].File)
	}

	assistantParts := req.Messages[2].Parts
	if len(assistantParts) != 5 {
		t.Fatalf("expected 5 assistant parts (reasoning, text, refusal, fn_call, custom_call), got %d", len(assistantParts))
	}
	if assistantParts[0].Type != ir.PartReasoning || assistantParts[0].Reasoning.Summary != "Plan step 1" || assistantParts[0].Reasoning.Text != "Detailed internal thought" || assistantParts[0].Reasoning.RedactedData != "enc_blob_responses_1" || assistantParts[0].Reasoning.OriginFormat != ir.FormatOpenAIResponses || assistantParts[0].Reasoning.ItemID != "rs_001" {
		t.Fatalf("unexpected reasoning part: %+v", assistantParts[0].Reasoning)
	}
	if assistantParts[3].ToolCall.ID != "call_weather_1" || assistantParts[3].ToolCall.ItemID != "fc_001" || assistantParts[3].ToolCall.Kind != "function" {
		t.Fatalf("unexpected function_call part: %+v", assistantParts[3].ToolCall)
	}
	if assistantParts[4].ToolCall.ID != "call_sql_2" || assistantParts[4].ToolCall.ItemID != "ctc_002" || assistantParts[4].ToolCall.Kind != "custom" || assistantParts[4].ToolCall.Arguments != "SELECT 1" {
		t.Fatalf("unexpected custom_tool_call part: %+v", assistantParts[4].ToolCall)
	}

	tool1 := req.Messages[3].Parts[0].ToolResult
	if tool1.ToolCallID != "call_weather_1" || len(tool1.Parts) != 2 || tool1.Parts[1].Type != ir.PartImage {
		t.Fatalf("unexpected multimodal tool result: %+v", tool1)
	}

	if len(req.PassthroughItems) != 1 || req.PassthroughItems[0].Kind != "item_reference" || req.PassthroughItems[0].OriginFormat != ir.FormatOpenAIResponses {
		t.Fatalf("unexpected passthrough items: %+v", req.PassthroughItems)
	}

	if len(req.Tools) != 3 || req.Tools[0].Type != "function" || req.Tools[1].Type != "custom" || req.Tools[2].Type != "web_search_preview" {
		t.Fatalf("unexpected decoded tools: %+v", req.Tools)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "tool" || req.ToolChoice.ToolName != "get_weather" {
		t.Fatalf("unexpected tool_choice: %+v", req.ToolChoice)
	}

	// Re-encode and verify structural preservation.
	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	var out struct {
		Input      []map[string]any `json:"input"`
		Tools      []map[string]any `json:"tools"`
		ToolChoice map[string]any   `json:"tool_choice"`
	}
	if err := common.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}

	if len(out.Input) != 9 {
		t.Fatalf("expected 9 input items on round-trip, got %d: %s", len(out.Input), string(encoded))
	}
	if out.Input[2]["type"] != "reasoning" || out.Input[2]["encrypted_content"] != "enc_blob_responses_1" || out.Input[2]["id"] != "rs_001" {
		t.Fatalf("expected native reasoning preserved, got %+v", out.Input[2])
	}
	if out.Input[4]["type"] != "function_call" || out.Input[4]["id"] != "fc_001" || out.Input[4]["call_id"] != "call_weather_1" {
		t.Fatalf("expected function_call with fc_001 + call_weather_1, got %+v", out.Input[4])
	}
	if out.Input[5]["type"] != "custom_tool_call" || out.Input[5]["input"] != "SELECT 1" {
		t.Fatalf("expected custom_tool_call preserved, got %+v", out.Input[5])
	}
	if _, isSlice := out.Input[6]["output"].([]any); !isSlice {
		t.Fatalf("expected multimodal function_call_output array, got %T: %+v", out.Input[6]["output"], out.Input[6]["output"])
	}
	if out.Input[7]["type"] != "custom_tool_call_output" {
		t.Fatalf("expected custom_tool_call_output preserved, got %+v", out.Input[7])
	}
	if out.Input[8]["type"] != "item_reference" {
		t.Fatalf("expected passthrough item_reference preserved, got %+v", out.Input[8])
	}
	if len(out.Tools) != 3 || out.Tools[2]["type"] != "web_search_preview" || out.Tools[2]["search_context_size"] != "medium" {
		t.Fatalf("expected native tool passed through verbatim, got %+v", out.Tools)
	}
	if out.ToolChoice["type"] != "function" || out.ToolChoice["name"] != "get_weather" {
		t.Fatalf("unexpected encoded tool_choice: %+v", out.ToolChoice)
	}
}

func TestEncodeRequestCrossProviderReasoningDropAndToolCallIDSynthesis(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIResponses)

	req := &ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{
			{
				Role:  ir.RoleUser,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Compute 2+2 and look up weather"}},
			},
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "Anthropic private thinking",
							Signature:    "anth_sig_xyz",
							RedactedData: "anth_redacted_blob",
							OriginFormat: ir.FormatAnthropic,
						},
					},
					{Type: ir.PartText, Text: "Checking now."},
					{
						Type: ir.PartToolCall,
						ToolCall: &ir.ToolCall{
							ID:        "toolu_01ABC",
							Name:      "get_weather",
							Arguments: `{"city":"Paris"}`,
						},
					},
					{
						Type: ir.PartToolCall,
						ToolCall: &ir.ToolCall{
							ID:        "call_bridged_9",
							ItemID:    "fc_real_from_bridge",
							Name:      "get_time",
							Arguments: `{"tz":"UTC"}`,
						},
					},
				},
			},
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "toolu_01ABC", Content: "18C"}},
					{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "call_bridged_9", Content: "12:00"}},
				},
			},
		},
		Tools: []ir.ToolDefinition{
			{Type: "computer_20250124", NativeRaw: json.RawMessage(`{"type":"computer_20250124"}`), OriginFormat: ir.FormatAnthropic},
		},
	}

	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	rawStr := string(encoded)

	if strings.Contains(rawStr, "anth_sig_xyz") || strings.Contains(rawStr, "anth_redacted_blob") {
		t.Fatalf("foreign Anthropic signature/redacted_data leaked into Responses payload: %s", rawStr)
	}
	if strings.Contains(rawStr, `"type":"reasoning"`) {
		t.Fatalf("expected cross-provider unsigned reasoning item to be skipped, got: %s", rawStr)
	}
	if strings.Contains(rawStr, "computer_20250124") {
		t.Fatalf("expected foreign Anthropic native tool to be dropped, got: %s", rawStr)
	}

	// Verify warnings were recorded for dropped opaque reasoning, skipped synthetic reasoning, and foreign tool.
	warningCodes := map[string]bool{}
	for _, w := range ctx.Warnings {
		warningCodes[w.Code] = true
	}
	if !warningCodes["dropped_foreign_reasoning_opaque"] || !warningCodes["skipped_synthetic_reasoning_item"] || !warningCodes["dropped_foreign_native_tool"] {
		t.Fatalf("expected explicit warnings, got: %+v", ctx.Warnings)
	}

	var parsed struct {
		Input []map[string]any `json:"input"`
	}
	if err := common.Unmarshal(encoded, &parsed); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}
	// Expected: user msg, assistant msg, fc_toolu_01ABC, fc_real_from_bridge, output1, output2
	if len(parsed.Input) != 6 {
		t.Fatalf("expected 6 input items, got %d: %s", len(parsed.Input), rawStr)
	}
	if parsed.Input[2]["id"] != "fc_toolu_01ABC" || parsed.Input[2]["call_id"] != "toolu_01ABC" {
		t.Fatalf("expected synthesized fc_ id for unbridged tool call, got %+v", parsed.Input[2])
	}
	if parsed.Input[3]["id"] != "fc_real_from_bridge" || parsed.Input[3]["call_id"] != "call_bridged_9" {
		t.Fatalf("expected bridged ItemID preserved, got %+v", parsed.Input[3])
	}
}

func TestDecodeAndEncodeResponse(t *testing.T) {
	c := New()
	raw := []byte(`{
		"id": "resp_abc123",
		"object": "response",
		"created_at": 1700000100,
		"status": "completed",
		"model": "gpt-5",
		"service_tier": "default",
		"output": [
			{
				"id": "rs_1",
				"type": "reasoning",
				"status": "completed",
				"summary": [{"type": "summary_text", "text": "Checked sources."}],
				"encrypted_content": "enc_resp_99"
			},
			{
				"id": "ws_1",
				"type": "web_search_call",
				"status": "completed"
			},
			{
				"id": "fs_1",
				"type": "file_search_call",
				"status": "completed"
			},
			{
				"id": "ig_1",
				"type": "image_generation_call",
				"status": "completed"
			},
			{
				"id": "msg_1",
				"type": "message",
				"status": "completed",
				"role": "assistant",
				"content": [
					{
						"type": "output_text",
						"text": "Found result.",
						"annotations": [
							{"type": "url_citation", "url": "https://example.com", "title": "Example", "start_index": 0, "end_index": 13}
						]
					},
					{
						"type": "refusal",
						"refusal": "Cannot reveal private data."
					}
				]
			},
			{
				"id": "fc_42",
				"type": "function_call",
				"call_id": "call_42",
				"name": "lookup",
				"arguments": "{\"q\":\"test\"}",
				"status": "completed"
			}
		],
		"usage": {
			"input_tokens": 120,
			"output_tokens": 60,
			"total_tokens": 180,
			"input_tokens_details": {"cached_tokens": 30, "cache_write_tokens": 10},
			"output_tokens_details": {"reasoning_tokens": 25}
		}
	}`)

	ctx := ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatOpenAIResponses)
	resp, err := c.DecodeResponse(raw, ctx)
	if err != nil {
		t.Fatalf("DecodeResponse error: %v", err)
	}

	if resp.ID != "resp_abc123" || resp.Model != "gpt-5" || resp.Created != 1700000100 {
		t.Fatalf("unexpected envelope: %+v", resp)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].FinishReason != ir.FinishToolCalls {
		t.Fatalf("expected FinishToolCalls, got %+v", resp.Choices)
	}
	parts := resp.Choices[0].Message.Parts
	// reasoning, text, citation, refusal, tool_call
	if len(parts) != 5 {
		t.Fatalf("expected 5 parts, got %d: %+v", len(parts), parts)
	}
	if parts[0].Reasoning.RedactedData != "enc_resp_99" || parts[0].Reasoning.ItemID != "rs_1" {
		t.Fatalf("unexpected decoded reasoning: %+v", parts[0].Reasoning)
	}
	if parts[2].Type != ir.PartCitation || parts[2].Citation.URL != "https://example.com" {
		t.Fatalf("unexpected citation part: %+v", parts[2])
	}
	if parts[4].ToolCall.ID != "call_42" || parts[4].ToolCall.ItemID != "fc_42" {
		t.Fatalf("unexpected tool call part: %+v", parts[4].ToolCall)
	}
	if entry, ok := ctx.BridgeState.ToolCalls["call_42"]; !ok || entry.ResponsesItemID != "fc_42" {
		t.Fatalf("expected BridgeState to record call_42 -> fc_42, got %+v", ctx.BridgeState.ToolCalls)
	}

	if resp.Usage == nil || resp.Usage.InputExclusive != false || resp.Usage.InputTokens != 120 || resp.Usage.OutputTokens != 60 || resp.Usage.CachedReadTokens != 30 || resp.Usage.CacheCreationTokens != 10 || resp.Usage.ReasoningTokens != 25 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
	if resp.Usage.WebSearchRequests != 1 || resp.Usage.FileSearchCalls != 1 || !resp.Usage.HasImageGenCall {
		t.Fatalf("unexpected built-in tool usage counters: %+v", resp.Usage)
	}
	if len(resp.PassthroughItems) != 3 {
		t.Fatalf("expected 3 passthrough built-in tool call items, got %d", len(resp.PassthroughItems))
	}

	// Test incomplete statuses
	incompleteMax := []byte(`{"id":"resp_inc1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"cut"}]}]}`)
	rInc1, err := c.DecodeResponse(incompleteMax, nil)
	if err != nil || rInc1.Choices[0].FinishReason != ir.FinishLength {
		t.Fatalf("expected FinishLength, got %+v err=%v", rInc1, err)
	}

	incompleteFilter := []byte(`{"id":"resp_inc2","status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`)
	rInc2, err := c.DecodeResponse(incompleteFilter, nil)
	if err != nil || rInc2.Choices[0].FinishReason != ir.FinishContentFilter {
		t.Fatalf("expected FinishContentFilter, got %+v err=%v", rInc2, err)
	}

	// Test EncodeResponse with Anthropic-exclusive usage converting to OpenAI-inclusive usage
	irResp := &ir.Response{
		ID:      "msg_01anth",
		Model:   "claude-sonnet-4-5",
		Created: 1700000200,
		Choices: []ir.Choice{{
			Index: 0,
			Message: ir.Message{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{Type: ir.PartText, Text: "Hello from Claude"},
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "toolu_99", Name: "calc", Arguments: `{"x":1}`}},
				},
			},
			FinishReason: ir.FinishLength,
		}},
		Usage: &ir.Usage{
			InputTokens:         100,
			OutputTokens:        50,
			InputExclusive:      true,
			CachedReadTokens:    40,
			CacheCreationTokens: 20,
			ReasoningTokens:     10,
		},
	}
	encCtx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIResponses)
	encBytes, err := c.EncodeResponse(irResp, encCtx)
	if err != nil {
		t.Fatalf("EncodeResponse error: %v", err)
	}
	var out struct {
		ID                string                          `json:"id"`
		Object            string                          `json:"object"`
		Status            string                          `json:"status"`
		IncompleteDetails *responsesIncompleteDetailsWire `json:"incomplete_details"`
		Output            []map[string]any                `json:"output"`
		Usage             *responsesUsageWire             `json:"usage"`
	}
	if err := common.Unmarshal(encBytes, &out); err != nil {
		t.Fatalf("unmarshal encoded response: %v", err)
	}
	if !strings.HasPrefix(out.ID, "resp_") || out.Object != "response" || out.Status != "incomplete" || out.IncompleteDetails == nil || out.IncompleteDetails.Reason != "max_output_tokens" {
		t.Fatalf("unexpected encoded response envelope: %+v", out)
	}
	if len(out.Output) != 2 || !strings.HasPrefix(out.Output[0]["id"].(string), "msg_") || out.Output[1]["id"] != "fc_toolu_99" {
		t.Fatalf("unexpected encoded output items: %+v", out.Output)
	}
	if out.Usage == nil || out.Usage.InputTokens != 160 || out.Usage.OutputTokens != 50 || out.Usage.TotalTokens != 210 || out.Usage.InputTokensDetails.CachedTokens != 40 || out.Usage.OutputTokensDetails.ReasoningTokens != 10 {
		t.Fatalf("expected inclusive OpenAI usage (160/50/210), got %+v", out.Usage)
	}
}

func TestDecodeStreamEventsAndBridgeDeduplication(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatOpenAIResponses, ir.FormatOpenAIChat)

	streamChunks := []struct {
		event string
		data  string
	}{
		{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_s1","model":"gpt-5","created_at":1700000300,"status":"in_progress"}}`},
		{"response.in_progress", `{"type":"response.in_progress","sequence_number":1,"response":{"id":"resp_s1","model":"gpt-5","created_at":1700000300,"status":"in_progress"}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":2,"output_index":0,"item":{"id":"rs_s1","type":"reasoning","status":"in_progress","summary":[]}}`},
		{"response.reasoning_summary_text.delta", `{"type":"response.reasoning_summary_text.delta","sequence_number":3,"item_id":"rs_s1","output_index":0,"summary_index":0,"delta":"Thinking..."}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":4,"output_index":0,"item":{"id":"rs_s1","type":"reasoning","status":"completed","summary":[{"type":"summary_text","text":"Thinking..."}],"encrypted_content":"enc_stream_1"}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":5,"output_index":1,"item":{"id":"ws_dup","type":"web_search_call","status":"in_progress"}}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":6,"output_index":1,"item":{"id":"ws_dup","type":"web_search_call","status":"completed"}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":7,"output_index":2,"item":{"id":"msg_s1","type":"message","status":"in_progress","role":"assistant","content":[]}}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":8,"item_id":"msg_s1","output_index":2,"content_index":0,"delta":"Hello "}`},
		{"response.refusal.delta", `{"type":"response.refusal.delta","sequence_number":9,"item_id":"msg_s1","output_index":2,"content_index":1,"delta":"nope"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":10,"output_index":2,"item":{"id":"msg_s1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Hello "}]}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":11,"output_index":3,"item":{"id":"fc_stream_1","type":"function_call","call_id":"call_stream_1","name":"search","arguments":"","status":"in_progress"}}`},
		{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","sequence_number":12,"item_id":"fc_stream_1","output_index":3,"delta":"{\"q\":\"go\"}"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":13,"output_index":3,"item":{"id":"fc_stream_1","type":"function_call","call_id":"call_stream_1","name":"search","arguments":"{\"q\":\"go\"}","status":"completed"}}`},
		// Second tool call without prior argument deltas -> output_item.done should emit fallback delta!
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":14,"output_index":4,"item":{"id":"fc_stream_2","type":"function_call","call_id":"call_stream_2","name":"ping","arguments":"","status":"in_progress"}}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":15,"output_index":4,"item":{"id":"fc_stream_2","type":"function_call","call_id":"call_stream_2","name":"ping","arguments":"{\"host\":\"1.1.1.1\"}","status":"completed"}}`},
		{"response.completed", `{"type":"response.completed","sequence_number":16,"response":{"id":"resp_s1","model":"gpt-5","status":"completed","output":[{"id":"ws_dup","type":"web_search_call"}],"usage":{"input_tokens":50,"output_tokens":20,"total_tokens":70,"output_tokens_details":{"reasoning_tokens":8}}}}`},
	}

	var allEvents []ir.StreamEvent
	for _, chunk := range streamChunks {
		evs, err := c.DecodeStreamEvent(chunk.event, []byte(chunk.data), ctx)
		if err != nil {
			t.Fatalf("DecodeStreamEvent(%s) error: %v", chunk.event, err)
		}
		allEvents = append(allEvents, evs...)
	}

	// Count event types and validate key payloads
	counts := map[ir.StreamEventType]int{}
	var toolDeltas []ir.StreamEvent
	var finalUsage *ir.Usage
	var finalFinish ir.FinishReason
	for _, ev := range allEvents {
		counts[ev.Type]++
		if ev.Type == ir.EventToolCallDelta {
			toolDeltas = append(toolDeltas, ev)
		}
		if ev.Type == ir.EventUsage {
			finalUsage = ev.Usage
		}
		if ev.Type == ir.EventFinish {
			finalFinish = ev.FinishReason
		}
	}

	if counts[ir.EventStreamStart] != 1 {
		t.Fatalf("expected exactly 1 EventStreamStart (created+in_progress deduped), got %d", counts[ir.EventStreamStart])
	}
	if counts[ir.EventReasoningDelta] != 1 || counts[ir.EventReasoningSignatureDelta] != 1 || ctx.ReasoningRedacted != "enc_stream_1" {
		t.Fatalf("unexpected reasoning events: counts=%+v redacted=%q", counts, ctx.ReasoningRedacted)
	}
	if counts[ir.EventTextDelta] != 1 || counts[ir.EventRefusalDelta] != 1 {
		t.Fatalf("unexpected text/refusal counts: %+v", counts)
	}
	if counts[ir.EventToolCallStart] != 2 || len(toolDeltas) != 2 {
		t.Fatalf("expected 2 tool call starts and 2 tool deltas (including fallback), got starts=%d deltas=%+v", counts[ir.EventToolCallStart], toolDeltas)
	}
	if toolDeltas[0].ToolCallID != "call_stream_1" || toolDeltas[0].ToolCallItemID != "fc_stream_1" || toolDeltas[0].ToolArguments != `{"q":"go"}` {
		t.Fatalf("unexpected tool delta 0: %+v", toolDeltas[0])
	}
	if toolDeltas[1].ToolCallID != "call_stream_2" || toolDeltas[1].ToolCallItemID != "fc_stream_2" || toolDeltas[1].ToolArguments != `{"host":"1.1.1.1"}` {
		t.Fatalf("unexpected fallback tool delta 1: %+v", toolDeltas[1])
	}
	if entry, ok := ctx.BridgeState.ToolCalls["call_stream_1"]; !ok || entry.ResponsesItemID != "fc_stream_1" {
		t.Fatalf("expected BridgeState to record call_stream_1 -> fc_stream_1, got %+v", ctx.BridgeState.ToolCalls)
	}
	if finalUsage == nil || finalUsage.WebSearchRequests != 1 || finalUsage.ReasoningTokens != 8 {
		t.Fatalf("expected deduplicated web_search_call=1 and reasoning_tokens=8, got %+v", finalUsage)
	}
	if finalFinish != ir.FinishToolCalls || counts[ir.EventStreamEnd] != 1 {
		t.Fatalf("expected FinishToolCalls + EventStreamEnd, got finish=%s counts=%+v", finalFinish, counts)
	}

	// Test response.failed -> EventError
	errEvs, err := c.DecodeStreamEvent("response.failed", []byte(`{"type":"response.failed","response":{"status":"failed","error":{"type":"server_error","code":"500","message":"boom"}}}`), ir.NewStreamContext(ir.FormatOpenAIResponses, ir.FormatOpenAIChat))
	if err != nil || len(errEvs) != 1 || errEvs[0].Type != ir.EventError || errEvs[0].ErrorMessage != "boom" {
		t.Fatalf("expected EventError(boom), got %+v err=%v", errEvs, err)
	}
}

func TestEncodeStreamEventFullLifecycleAndMonotonicSeq(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIResponses)
	ctx.Model = "claude-sonnet-4-5"

	inputEvents := []ir.StreamEvent{
		// Deliberately omit EventStreamStart to verify lazy response.created + response.in_progress!
		{Type: ir.EventReasoningDelta, Reasoning: "Step 1. ", OriginFormat: ir.FormatAnthropic},
		{Type: ir.EventReasoningDelta, Reasoning: "Step 2.", OriginFormat: ir.FormatAnthropic},
		{Type: ir.EventReasoningSignatureDelta, Signature: "foreign_anth_sig", OriginFormat: ir.FormatAnthropic},
		{Type: ir.EventTextDelta, Text: "Hello "},
		{Type: ir.EventTextDelta, Text: "world!"},
		{Type: ir.EventToolCallStart, ToolCallIndex: 0, ToolCallID: "toolu_01", ToolName: "get_weather"},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "toolu_01", ToolArguments: `{"city":`},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "toolu_01", ToolArguments: `"NYC"}`},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 40, OutputTokens: 25, InputExclusive: true, CachedReadTokens: 10}},
		{Type: ir.EventFinish, FinishReason: ir.FinishToolCalls},
		{Type: ir.EventStreamEnd},
	}

	var frames []ir.SSEFrame
	for _, ev := range inputEvents {
		out, err := c.EncodeStreamEvent(ev, ctx)
		if err != nil {
			t.Fatalf("EncodeStreamEvent(%s) error: %v", ev.Type, err)
		}
		frames = append(frames, out...)
	}
	tail, err := c.FlushStream(ctx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("expected idempotent FlushStream after EventFinish, got %d frames", len(tail))
	}

	expectedOrder := []string{
		"response.created",
		"response.in_progress",
		"response.output_item.added",            // reasoning
		"response.reasoning_summary_part.added", // reasoning summary part
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done",
		"response.output_item.done",   // reasoning done
		"response.output_item.added",  // message
		"response.content_part.added", // output_text
		"response.output_text.delta",
		"response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done",
		"response.output_item.done",  // message done
		"response.output_item.added", // function_call
		"response.function_call_arguments.delta",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done", // function_call done
		"response.completed",
	}

	if len(frames) != len(expectedOrder) {
		gotTypes := make([]string, len(frames))
		for i, f := range frames {
			gotTypes[i] = f.Event
		}
		t.Fatalf("frame count mismatch: got %d (%v), want %d (%v)", len(frames), gotTypes, len(expectedOrder), expectedOrder)
	}

	for i, f := range frames {
		if f.Event != expectedOrder[i] {
			t.Fatalf("frame[%d] event mismatch: got %q want %q", i, f.Event, expectedOrder[i])
		}
		var payload map[string]any
		if err := common.Unmarshal(f.Data, &payload); err != nil {
			t.Fatalf("frame[%d] invalid JSON: %v", i, err)
		}
		if payload["type"] != f.Event {
			t.Fatalf("frame[%d] payload.type=%v != frame.Event=%q", i, payload["type"], f.Event)
		}
		seq, ok := payload["sequence_number"].(float64)
		if !ok || int(seq) != i {
			t.Fatalf("frame[%d] expected monotonic sequence_number=%d, got %v", i, i, payload["sequence_number"])
		}
		if strings.Contains(string(f.Data), "foreign_anth_sig") {
			t.Fatalf("foreign signature leaked in frame[%d]: %s", i, string(f.Data))
		}
	}

	if ctx.AccumulatedText.String() != "Hello world!" {
		t.Fatalf("expected ctx.AccumulatedText=%q, got %q", "Hello world!", ctx.AccumulatedText.String())
	}

	// Verify final response.completed has all 3 assembled output items + inclusive usage
	var completed struct {
		Response struct {
			ID     string           `json:"id"`
			Status string           `json:"status"`
			Output []map[string]any `json:"output"`
			Usage  struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := common.Unmarshal(frames[len(frames)-1].Data, &completed); err != nil {
		t.Fatalf("unmarshal response.completed: %v", err)
	}
	if !strings.HasPrefix(completed.Response.ID, "resp_") || completed.Response.Status != "completed" {
		t.Fatalf("unexpected completed envelope: %+v", completed.Response)
	}
	if len(completed.Response.Output) != 3 {
		t.Fatalf("expected 3 assembled output items, got %d: %+v", len(completed.Response.Output), completed.Response.Output)
	}
	if completed.Response.Output[0]["type"] != "reasoning" || completed.Response.Output[1]["type"] != "message" || completed.Response.Output[2]["type"] != "function_call" {
		t.Fatalf("unexpected output item ordering: %+v", completed.Response.Output)
	}
	if completed.Response.Output[2]["id"] != "fc_toolu_01" || completed.Response.Output[2]["call_id"] != "toolu_01" || completed.Response.Output[2]["arguments"] != `{"city":"NYC"}` {
		t.Fatalf("unexpected assembled function_call: %+v", completed.Response.Output[2])
	}
	if completed.Response.Usage.InputTokens != 50 || completed.Response.Usage.OutputTokens != 25 || completed.Response.Usage.TotalTokens != 75 {
		t.Fatalf("expected Anthropic-exclusive usage converted to inclusive 50/25/75, got %+v", completed.Response.Usage)
	}
}

func TestEncodeStreamIncompleteOnFinishLengthAndDeferredChatUsage(t *testing.T) {
	c := New()
	// Simulate OpenAI Chat upstream where EventFinish arrives before EventUsage, followed by FlushStream.
	ctx := ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatOpenAIResponses)
	ctx.Model = "gpt-5"

	var frames []ir.SSEFrame
	for _, ev := range []ir.StreamEvent{
		{Type: ir.EventTextDelta, Text: "Truncated output"},
		{Type: ir.EventFinish, FinishReason: ir.FinishLength},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 10, OutputTokens: 100, TotalTokens: 110}},
	} {
		out, err := c.EncodeStreamEvent(ev, ctx)
		if err != nil {
			t.Fatalf("EncodeStreamEvent error: %v", err)
		}
		frames = append(frames, out...)
	}
	tail, err := c.FlushStream(ctx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	frames = append(frames, tail...)

	last := frames[len(frames)-1]
	if last.Event != "response.incomplete" {
		t.Fatalf("expected terminal event response.incomplete, got %q", last.Event)
	}
	var payload struct {
		Response struct {
			Status            string                          `json:"status"`
			IncompleteDetails *responsesIncompleteDetailsWire `json:"incomplete_details"`
			Usage             *responsesUsageWire             `json:"usage"`
		} `json:"response"`
	}
	if err := common.Unmarshal(last.Data, &payload); err != nil {
		t.Fatalf("unmarshal response.incomplete: %v", err)
	}
	if payload.Response.Status != "incomplete" || payload.Response.IncompleteDetails == nil || payload.Response.IncompleteDetails.Reason != "max_output_tokens" {
		t.Fatalf("unexpected incomplete details: %+v", payload.Response)
	}
	if payload.Response.Usage == nil || payload.Response.Usage.TotalTokens != 110 {
		t.Fatalf("expected deferred OpenAI Chat usage preserved on response.incomplete, got %+v", payload.Response.Usage)
	}
}

func TestPipelineResponsesToResponsesTurnBridge(t *testing.T) {
	reg := ir.NewRegistry()
	reg.Register(New())
	tb := ir.NewTurnBridge(0, 100)

	p, err := ir.NewPipeline(ir.PipelineConfig{
		Registry:     reg,
		Bridge:       tb,
		BridgeScope:  "chan:resp",
		SourceFormat: ir.FormatOpenAIResponses,
		TargetFormat: ir.FormatOpenAIResponses,
	})
	if err != nil {
		t.Fatalf("NewPipeline error: %v", err)
	}

	// Turn 1: stream response with function_call (records call_1 -> fc_orig_1 in TurnBridge)
	st := p.NewStreamTranslator("gpt-5")
	sse := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_p1\",\"model\":\"gpt-5\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"sequence_number\":1,\"output_index\":0,\"item\":{\"id\":\"fc_orig_1\",\"type\":\"function_call\",\"call_id\":\"call_p1\",\"name\":\"fetch\",\"arguments\":\"\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"sequence_number\":2,\"item_id\":\"fc_orig_1\",\"output_index\":0,\"delta\":\"{\\\"url\\\":\\\"https://a.com\\\"}\"}\n\n" +
		"event: response.output_item.done\n" +
		"data: {\"type\":\"response.output_item.done\",\"sequence_number\":3,\"output_index\":0,\"item\":{\"id\":\"fc_orig_1\",\"type\":\"function_call\",\"call_id\":\"call_p1\",\"name\":\"fetch\",\"arguments\":\"{\\\"url\\\":\\\"https://a.com\\\"}\",\"status\":\"completed\"}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"sequence_number\":4,\"response\":{\"id\":\"resp_p1\",\"model\":\"gpt-5\",\"status\":\"completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":5,\"total_tokens\":15}}}\n\n"

	var emitted []ir.SSEFrame
	if err := st.TranslateReader(strings.NewReader(sse), func(frame ir.SSEFrame) error {
		emitted = append(emitted, frame)
		return nil
	}); err != nil {
		t.Fatalf("TranslateReader error: %v", err)
	}
	if len(emitted) == 0 || emitted[len(emitted)-1].Event != "response.completed" {
		t.Fatalf("expected stream ending in response.completed, got %d frames", len(emitted))
	}

	// Turn 2: client sends function_call without id (only call_id); TurnBridge injects fc_orig_1!
	nextReq := []byte(`{
		"model": "gpt-5",
		"input": [
			{"type": "function_call", "call_id": "call_p1", "name": "fetch", "arguments": "{\"url\":\"https://a.com\"}"},
			{"type": "function_call_output", "call_id": "call_p1", "output": "ok"}
		]
	}`)
	outReq, _, _, err := p.ConvertRequest(nextReq)
	if err != nil {
		t.Fatalf("ConvertRequest turn 2 error: %v", err)
	}
	if !strings.Contains(string(outReq), `"id":"fc_orig_1"`) {
		t.Fatalf("expected TurnBridge to inject fc_orig_1 into turn 2 request, got: %s", string(outReq))
	}
}

func TestCustomToolCallStreamingAndEncryptedOnlyReasoning(t *testing.T) {
	c := New()
	decCtx := ir.NewStreamContext(ir.FormatOpenAIResponses, ir.FormatOpenAIResponses)
	encCtx := ir.NewStreamContext(ir.FormatOpenAIResponses, ir.FormatOpenAIResponses)
	encCtx.Model = "gpt-5"

	chunks := []struct {
		event string
		data  string
	}{
		{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_c1","model":"gpt-5","status":"in_progress"}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"rs_enc_only","type":"reasoning","status":"in_progress","summary":[]}}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":2,"output_index":0,"item":{"id":"rs_enc_only","type":"reasoning","status":"completed","summary":[],"encrypted_content":"gAAAA_encrypted_only"}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":3,"output_index":1,"item":{"id":"ctc_1","type":"custom_tool_call","call_id":"call_ctc_1","name":"code_exec","input":"","status":"in_progress"}}`},
		{"response.custom_tool_call_input.delta", `{"type":"response.custom_tool_call_input.delta","sequence_number":4,"item_id":"ctc_1","output_index":1,"delta":"print(1)"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":5,"output_index":1,"item":{"id":"ctc_1","type":"custom_tool_call","call_id":"call_ctc_1","name":"code_exec","input":"print(1)","status":"completed"}}`},
		{"response.completed", `{"type":"response.completed","sequence_number":6,"response":{"id":"resp_c1","model":"gpt-5","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30}}}`},
	}

	var frames []ir.SSEFrame
	for _, ch := range chunks {
		evs, err := c.DecodeStreamEvent(ch.event, []byte(ch.data), decCtx)
		if err != nil {
			t.Fatalf("DecodeStreamEvent(%s) error: %v", ch.event, err)
		}
		for _, ev := range evs {
			out, err := c.EncodeStreamEvent(ev, encCtx)
			if err != nil {
				t.Fatalf("EncodeStreamEvent(%s) error: %v", ev.Type, err)
			}
			frames = append(frames, out...)
		}
	}

	// Verify monotonic sequence numbers and custom tool + encrypted reasoning presence
	var sawCustomDelta, sawCustomDone, sawEncrypted bool
	for i, f := range frames {
		var p map[string]any
		if err := common.Unmarshal(f.Data, &p); err != nil {
			t.Fatalf("frame[%d] invalid JSON: %v", i, err)
		}
		if int(p["sequence_number"].(float64)) != i {
			t.Fatalf("frame[%d] non-monotonic sequence_number: %v", i, p["sequence_number"])
		}
		switch f.Event {
		case "response.custom_tool_call_input.delta":
			sawCustomDelta = true
		case "response.custom_tool_call_input.done":
			sawCustomDone = true
		case "response.completed":
			if strings.Contains(string(f.Data), `"encrypted_content":"gAAAA_encrypted_only"`) {
				sawEncrypted = true
			}
		}
	}
	if !sawCustomDelta || !sawCustomDone || !sawEncrypted {
		t.Fatalf("missing expected frames: customDelta=%v customDone=%v encrypted=%v", sawCustomDelta, sawCustomDone, sawEncrypted)
	}
}

func TestFlushStreamOnAbruptEOFAndToolChoiceVariants(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatOpenAIResponses)
	ctx.Model = "gemini-2.5-pro"

	out1, err := c.EncodeStreamEvent(ir.StreamEvent{Type: ir.EventTextDelta, Text: "Partial text"}, ctx)
	if err != nil {
		t.Fatalf("EncodeStreamEvent error: %v", err)
	}
	out2, err := c.FlushStream(ctx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	all := append(out1, out2...)
	if len(all) == 0 || all[len(all)-1].Event != "response.completed" {
		t.Fatalf("expected FlushStream to emit response.completed, got %d frames", len(all))
	}
	for i, f := range all {
		var p map[string]any
		if err := common.Unmarshal(f.Data, &p); err != nil {
			t.Fatalf("invalid json: %v", err)
		}
		if int(p["sequence_number"].(float64)) != i {
			t.Fatalf("non-monotonic sequence_number at %d: %v", i, p["sequence_number"])
		}
	}

	// Verify tool_choice string modes ("auto", "none", "required") round-trip
	for _, mode := range []string{"auto", "none", "required"} {
		raw := []byte(`{"model":"gpt-5","input":"hi","tool_choice":"` + mode + `"}`)
		req, err := c.DecodeRequest(raw, nil)
		if err != nil || req.ToolChoice == nil || req.ToolChoice.Mode != mode {
			t.Fatalf("mode %q decode failed: req=%+v err=%v", mode, req, err)
		}
		enc, err := c.EncodeRequest(req, nil)
		if err != nil {
			t.Fatalf("mode %q encode failed: %v", mode, err)
		}
		var m map[string]any
		_ = common.Unmarshal(enc, &m)
		if m["tool_choice"] != mode {
			t.Fatalf("mode %q round-trip mismatch: got %v", mode, m["tool_choice"])
		}
	}
}
