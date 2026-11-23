package openaichat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func boolPtr(v bool) *bool        { return &v }

func TestCodecFormat(t *testing.T) {
	c := New()
	if c.Format() != ir.FormatOpenAIChat {
		t.Fatalf("expected FormatOpenAIChat, got %q", c.Format())
	}
}

func TestRequestRoundTripFull(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat)

	rawReq := `{
		"model": "gpt-4.1",
		"messages": [
			{"role": "system", "content": "You are a concise assistant."},
			{"role": "developer", "content": "Always obey safety policies."},
			{"role": "user", "content": "Hello"},
			{"role": "system", "content": "Mid-conversation reminder"},
			{"role": "user", "content": "Continue"}
		],
		"max_tokens": 200,
		"max_completion_tokens": 0,
		"temperature": 0.7,
		"top_p": 0.9,
		"stop": "STOP_HERE",
		"frequency_penalty": 0.1,
		"presence_penalty": 0.2,
		"seed": 42,
		"n": 2,
		"logprobs": true,
		"top_logprobs": 5,
		"logit_bias": {"50256": -100},
		"user": "user-123",
		"service_tier": "auto",
		"verbosity": "medium",
		"modalities": ["text"],
		"store": true,
		"prompt_cache_key": "cache-key-1",
		"prompt_cache_retention": "24h",
		"reasoning_effort": "high",
		"response_format": {
			"type": "json_schema",
			"json_schema": {
				"name": "answer_schema",
				"description": "Structured answer",
				"strict": true,
				"schema": {"type": "object", "properties": {"answer": {"type": "string"}}}
			}
		},
		"stream": true,
		"stream_options": {
			"include_usage": true,
			"include_obfuscation": false
		},
		"metadata": {"trace_id": "abc"},
		"extra_body": {"custom_flag": true},
		"prompt_cache_options": {"ttl": "5m"},
		"moderation": "auto",
		"web_search_options": {"search_context_size": "medium"}
	}`

	req, err := c.DecodeRequest([]byte(rawReq), ctx)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}

	if req.Model != "gpt-4.1" {
		t.Fatalf("model mismatch: %q", req.Model)
	}
	if !req.Stream || req.StreamIncludeUsage == nil || !*req.StreamIncludeUsage {
		t.Fatalf("stream/include_usage mismatch: stream=%v include=%v", req.Stream, req.StreamIncludeUsage)
	}
	// Leading system + developer extracted to req.System; mid-conversation system stays in req.Messages.
	if len(req.System) != 2 {
		t.Fatalf("expected 2 system parts, got %d: %+v", len(req.System), req.System)
	}
	if req.System[0].Text != "You are a concise assistant." {
		t.Fatalf("unexpected system[0]: %+v", req.System[0])
	}
	if req.System[1].Text != "Always obey safety policies." || req.System[1].ProviderMetadata["role"] != "developer" {
		t.Fatalf("unexpected developer system[1]: %+v", req.System[1])
	}
	if len(req.Messages) != 3 || req.Messages[1].Role != ir.RoleSystem {
		t.Fatalf("expected 3 messages with mid-conversation RoleSystem, got %+v", req.Messages)
	}

	// max_completion_tokens (0) takes precedence over max_tokens (200) and preserves explicit 0.
	if req.Generation.MaxTokens == nil || *req.Generation.MaxTokens != 0 {
		t.Fatalf("expected explicit MaxTokens=0 from max_completion_tokens, got %v", req.Generation.MaxTokens)
	}
	if req.Generation.Temperature == nil || *req.Generation.Temperature != 0.7 {
		t.Fatalf("temperature mismatch: %v", req.Generation.Temperature)
	}
	if len(req.Generation.StopSequences) != 1 || req.Generation.StopSequences[0] != "STOP_HERE" {
		t.Fatalf("stop mismatch: %+v", req.Generation.StopSequences)
	}
	if req.Generation.PromptCacheKey != "cache-key-1" || req.Generation.PromptCacheRetention != "24h" {
		t.Fatalf("prompt cache fields mismatch: %+v", req.Generation)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Fatalf("reasoning mismatch: %+v", req.Reasoning)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" || req.ResponseFormat.Name != "answer_schema" {
		t.Fatalf("response_format mismatch: %+v", req.ResponseFormat)
	}
	for _, extKey := range []string{"extra_body", "prompt_cache_options", "moderation", "web_search_options", extStreamIncludeObfuscation} {
		if _, ok := req.ProviderExtensions[extKey]; !ok {
			t.Fatalf("expected ProviderExtensions[%q] to be preserved", extKey)
		}
	}

	// Re-encode with use_max_completion_tokens=true and verify round-trip fields.
	ctx.Options["use_max_completion_tokens"] = true
	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}

	if v, ok := out["max_completion_tokens"].(float64); !ok || v != 0 {
		t.Fatalf("expected max_completion_tokens=0, got %#v", out["max_completion_tokens"])
	}
	if _, exists := out["max_tokens"]; exists {
		t.Fatalf("did not expect max_tokens when use_max_completion_tokens is enabled")
	}

	msgs := out["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 encoded messages, got %d: %s", len(msgs), string(encoded))
	}
	if msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["role"] != "developer" {
		t.Fatalf("expected leading system then developer roles, got %v, %v", msgs[0], msgs[1])
	}
	if msgs[3].(map[string]any)["role"] != "system" {
		t.Fatalf("expected mid-conversation system message preserved at index 3, got %v", msgs[3])
	}

	streamOpts := out["stream_options"].(map[string]any)
	if streamOpts["include_usage"] != true || streamOpts["include_obfuscation"] != false {
		t.Fatalf("stream_options mismatch: %#v", streamOpts)
	}
	if out["moderation"] != "auto" {
		t.Fatalf("expected moderation extension preserved, got %#v", out["moderation"])
	}
	if _, ok := out["web_search_options"]; !ok {
		t.Fatalf("expected web_search_options preserved")
	}

	// Verify disable_stream_options suppresses stream_options.
	ctxDisable := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat)
	ctxDisable.Options["disable_stream_options"] = true
	encodedDisabled, err := c.EncodeRequest(req, ctxDisable)
	if err != nil {
		t.Fatalf("EncodeRequest(disable_stream_options) error: %v", err)
	}
	var outDisabled map[string]any
	_ = json.Unmarshal(encodedDisabled, &outDisabled)
	if _, exists := outDisabled["stream_options"]; exists {
		t.Fatalf("expected stream_options omitted when disable_stream_options=true")
	}
}

func TestRequestReasoningObjectAndStreamIncludeUsageFalse(t *testing.T) {
	c := New()
	raw := `{
		"model": "deepseek-r1",
		"messages": [{"role": "user", "content": "Solve 2+2"}],
		"reasoning": {"effort": "medium", "max_tokens": 4096, "enabled": true},
		"stream": true,
		"stream_options": {"include_usage": false}
	}`
	req, err := c.DecodeRequest([]byte(raw), nil)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "medium" || req.Reasoning.BudgetTokens == nil || *req.Reasoning.BudgetTokens != 4096 || req.Reasoning.Mode != "enabled" {
		t.Fatalf("unexpected reasoning config: %+v", req.Reasoning)
	}
	if req.StreamIncludeUsage == nil || *req.StreamIncludeUsage {
		t.Fatalf("expected StreamIncludeUsage=false, got %v", req.StreamIncludeUsage)
	}

	encoded, err := c.EncodeRequest(req, nil)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(encoded, &out)
	if out["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort=medium, got %#v", out["reasoning_effort"])
	}
	if _, exists := out["stream_options"]; exists {
		t.Fatalf("expected stream_options omitted when StreamIncludeUsage=false, got %#v", out["stream_options"])
	}
}

func TestRequestMultimodalAndToolsRoundTrip(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat)

	raw := `{
		"model": "gpt-4o",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "Inspect these attachments"},
					{"type": "image_url", "image_url": {"url": "data:image/png;base64,iVBORw0KGgo=", "detail": "high"}},
					{"type": "image_url", "image_url": {"url": "https://example.com/cat.jpg", "detail": "low"}},
					{"type": "input_audio", "input_audio": {"data": "UklGRg==", "format": "wav"}},
					{"type": "file", "file": {"file_id": "file-1", "filename": "spec.pdf", "file_data": "data:application/pdf;base64,JVBERi0="}}
				]
			},
			{
				"role": "assistant",
				"reasoning_content": "I should call the weather tool.",
				"tool_calls": [
					{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}}
				]
			},
			{
				"role": "tool",
				"tool_call_id": "call_1",
				"name": "get_weather",
				"content": "{\"temp\":22}"
			}
		],
		"tools": [
			{
				"type": "function",
				"function": {
					"name": "get_weather",
					"description": "Get city weather",
					"strict": true,
					"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
				}
			},
			{
				"type": "custom",
				"custom": {"name": "code_exec", "description": "Run code"}
			}
		],
		"tool_choice": {"type": "function", "function": {"name": "get_weather"}},
		"parallel_tool_calls": false
	}`

	req, err := c.DecodeRequest([]byte(raw), ctx)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}

	// Validate multimodal decoding.
	userParts := req.Messages[0].Parts
	if len(userParts) != 5 {
		t.Fatalf("expected 5 user parts, got %d", len(userParts))
	}
	if userParts[1].Type != ir.PartImage || userParts[1].Image.Data != "iVBORw0KGgo=" || userParts[1].Image.MediaType != "image/png" || userParts[1].Image.Detail != "high" || userParts[1].Image.URL != "" {
		t.Fatalf("unexpected data URL image part: %+v", userParts[1].Image)
	}
	if userParts[2].Type != ir.PartImage || userParts[2].Image.URL != "https://example.com/cat.jpg" || userParts[2].Image.Detail != "low" {
		t.Fatalf("unexpected remote URL image part: %+v", userParts[2].Image)
	}
	if userParts[3].Type != ir.PartAudio || userParts[3].Audio.Format != "wav" || userParts[3].Audio.MediaType != "audio/wav" {
		t.Fatalf("unexpected audio part: %+v", userParts[3].Audio)
	}
	if userParts[4].Type != ir.PartFile || userParts[4].File.FileID != "file-1" || userParts[4].File.MediaType != "application/pdf" || userParts[4].File.Data != "JVBERi0=" {
		t.Fatalf("unexpected file part: %+v", userParts[4].File)
	}

	// Validate assistant reasoning + tool_calls.
	asstParts := req.Messages[1].Parts
	if len(asstParts) != 2 || asstParts[0].Type != ir.PartReasoning || asstParts[0].Reasoning.Text != "I should call the weather tool." {
		t.Fatalf("unexpected assistant reasoning: %+v", asstParts)
	}
	if asstParts[1].Type != ir.PartToolCall || asstParts[1].ToolCall.ID != "call_1" || asstParts[1].ToolCall.Name != "get_weather" {
		t.Fatalf("unexpected assistant tool call: %+v", asstParts[1].ToolCall)
	}

	// Validate tool result.
	toolParts := req.Messages[2].Parts
	if req.Messages[2].Role != ir.RoleTool || len(toolParts) != 1 || toolParts[0].ToolResult.ToolCallID != "call_1" || toolParts[0].ToolResult.Content != `{"temp":22}` {
		t.Fatalf("unexpected tool result message: %+v", req.Messages[2])
	}

	// Validate tools & tool_choice.
	if len(req.Tools) != 2 || req.Tools[0].Name != "get_weather" || req.Tools[1].Type != "custom" || len(req.Tools[1].NativeRaw) == 0 {
		t.Fatalf("unexpected tools: %+v", req.Tools)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "tool" || req.ToolChoice.ToolName != "get_weather" {
		t.Fatalf("unexpected tool_choice: %+v", req.ToolChoice)
	}
	if req.ParallelToolCalls == nil || *req.ParallelToolCalls {
		t.Fatalf("expected parallel_tool_calls=false")
	}

	// Round-trip encode and decode again.
	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	req2, err := c.DecodeRequest(encoded, ctx)
	if err != nil {
		t.Fatalf("second DecodeRequest error: %v", err)
	}
	if len(req2.Messages) != 3 || len(req2.Messages[0].Parts) != 5 {
		t.Fatalf("round-trip message parts mismatch: %+v", req2.Messages)
	}
	if req2.Messages[0].Parts[1].Image.Data != "iVBORw0KGgo=" || req2.Messages[0].Parts[1].Image.MediaType != "image/png" {
		t.Fatalf("round-trip base64 image mismatch: %+v", req2.Messages[0].Parts[1].Image)
	}
	if req2.Messages[1].Parts[0].Reasoning.Text != "I should call the weather tool." {
		t.Fatalf("round-trip reasoning_content mismatch: %+v", req2.Messages[1].Parts[0])
	}
	if len(req2.Tools) != 2 || req2.Tools[1].Type != "custom" {
		t.Fatalf("round-trip tools mismatch: %+v", req2.Tools)
	}
}

func TestRequestEncodeVisionToolResultsEmitsSyntheticUserMessage(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat)

	req := &ir.Request{
		Model: "gpt-4o",
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "call_1", Name: "screenshot", Arguments: "{}"}},
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "call_2", Name: "get_title", Arguments: "{}"}},
				},
			},
			{
				Role: ir.RoleUser,
				Parts: []ir.ContentPart{
					{
						Type: ir.PartToolResult,
						ToolResult: &ir.ToolResult{
							ToolCallID: "call_1",
							Parts: []ir.ContentPart{
								{Type: ir.PartText, Text: "Captured page"},
								{Type: ir.PartImage, Image: &ir.ImageSource{MediaType: "image/png", Data: "AQID"}},
							},
						},
					},
					{
						Type: ir.PartToolResult,
						ToolResult: &ir.ToolResult{
							ToolCallID: "call_2",
							Content:    "Dashboard",
						},
					},
				},
			},
		},
	}

	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}

	var out struct {
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal encoded: %v", err)
	}

	// Expected order: assistant -> tool(call_1) -> tool(call_2) -> synthetic user(image_url).
	if len(out.Messages) != 4 {
		t.Fatalf("expected 4 messages (assistant, tool, tool, synthetic user), got %d: %s", len(out.Messages), string(encoded))
	}
	if out.Messages[1].Role != "tool" || out.Messages[1].ToolCallID != "call_1" || string(out.Messages[1].Content) != `"Captured page"` {
		t.Fatalf("unexpected tool msg 1: %+v", out.Messages[1])
	}
	if out.Messages[2].Role != "tool" || out.Messages[2].ToolCallID != "call_2" || string(out.Messages[2].Content) != `"Dashboard"` {
		t.Fatalf("unexpected tool msg 2: %+v", out.Messages[2])
	}
	if out.Messages[3].Role != "user" || !strings.Contains(string(out.Messages[3].Content), "data:image/png;base64,AQID") {
		t.Fatalf("expected synthetic user message with image_url after tool messages, got %+v", out.Messages[3])
	}
}

func TestRequestEncodeWarnings(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat)

	req := &ir.Request{
		Model: "gpt-4o",
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "Step 1",
							Signature:    "anthropic_sig_abc",
							RedactedData: "redacted_blob",
							OriginFormat: ir.FormatAnthropic,
						},
					},
					{
						Type: ir.PartToolCall,
						ToolCall: &ir.ToolCall{
							ID:               "call_g1",
							Name:             "lookup",
							Arguments:        "{}",
							ThoughtSignature: "gemini_sig_xyz",
						},
					},
				},
			},
			{
				Role: ir.RoleUser,
				Parts: []ir.ContentPart{
					{Type: ir.PartAudio, Audio: &ir.AudioSource{URL: "https://example.com/a.mp3"}},
				},
			},
		},
		Tools: []ir.ToolDefinition{
			{Type: "web_search_20250305", Name: "web_search", OriginFormat: ir.FormatAnthropic, NativeRaw: json.RawMessage(`{"type":"web_search_20250305"}`)},
		},
		Generation: ir.GenerationConfig{
			TopK:               intPtr(40),
			Temperature:        floatPtr(0.5),
			Truncation:         "auto",
			PreviousResponseID: "resp_123",
		},
		Reasoning: &ir.ReasoningConfig{
			BudgetTokens: intPtr(6000),
		},
		Include: []string{"reasoning.encrypted_content"},
	}

	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}

	var out map[string]any
	_ = json.Unmarshal(encoded, &out)
	if out["reasoning_effort"] != "medium" {
		t.Fatalf("expected budget_tokens=6000 mapped to reasoning_effort=medium, got %#v", out["reasoning_effort"])
	}

	warningFields := make(map[string]bool)
	for _, w := range ctx.Warnings {
		warningFields[w.Field] = true
	}
	expectedFields := []string{
		"generation.top_k",
		"generation.truncation",
		"generation.previous_response_id",
		"include",
		"reasoning.signature",
		"reasoning.redacted_data",
		"tool_call.thought_signature",
		"audio.url",
		"tools.web_search",
		"reasoning.budget_tokens",
	}
	for _, f := range expectedFields {
		if !warningFields[f] {
			t.Fatalf("expected warning for field %q, got warnings: %+v", f, ctx.Warnings)
		}
	}
}

func TestResponseRoundTripAndUsageSemantics(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat)

	rawResp := `{
		"id": "chatcmpl-999",
		"object": "chat.completion",
		"created": 1700000100,
		"model": "gpt-4.1",
		"system_fingerprint": "fp_abc",
		"service_tier": "default",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"reasoning_content": "Checking sources...",
					"content": "Paris is the capital of France.",
					"annotations": [
						{
							"type": "url_citation",
							"url_citation": {
								"url": "https://en.wikipedia.org/wiki/Paris",
								"title": "Paris",
								"start_index": 0,
								"end_index": 5
							}
						}
					],
					"tool_calls": [
						{
							"id": "call_geo",
							"type": "function",
							"function": {"name": "verify", "arguments": "{\"city\":\"Paris\"}"}
						}
					]
				},
				"finish_reason": "tool_calls"
			}
		],
		"usage": {
			"prompt_tokens": 120,
			"completion_tokens": 45,
			"total_tokens": 165,
			"prompt_cache_hit_tokens": 30,
			"completion_tokens_details": {"reasoning_tokens": 15}
		}
	}`

	resp, err := c.DecodeResponse([]byte(rawResp), ctx)
	if err != nil {
		t.Fatalf("DecodeResponse error: %v", err)
	}

	if resp.ID != "chatcmpl-999" || resp.Model != "gpt-4.1" || resp.Created != 1700000100 || resp.SystemFingerprint != "fp_abc" || resp.ServiceTier != "default" {
		t.Fatalf("response envelope mismatch: %+v", resp)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].FinishReason != ir.FinishToolCalls {
		t.Fatalf("unexpected choices: %+v", resp.Choices)
	}
	// Parts: reasoning, text, citation, tool_call.
	parts := resp.Choices[0].Message.Parts
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts (reasoning, text, citation, tool_call), got %d: %+v", len(parts), parts)
	}
	if parts[0].Type != ir.PartReasoning || parts[0].Reasoning.Text != "Checking sources..." {
		t.Fatalf("unexpected reasoning part: %+v", parts[0])
	}
	if parts[1].Type != ir.PartText || parts[1].Text != "Paris is the capital of France." {
		t.Fatalf("unexpected text part: %+v", parts[1])
	}
	if parts[2].Type != ir.PartCitation || parts[2].Citation.URL != "https://en.wikipedia.org/wiki/Paris" {
		t.Fatalf("unexpected citation part: %+v", parts[2])
	}
	if parts[3].Type != ir.PartToolCall || parts[3].ToolCall.Name != "verify" {
		t.Fatalf("unexpected tool call part: %+v", parts[3])
	}

	// Usage: DeepSeek prompt_cache_hit_tokens fallback + InputExclusive=false.
	if resp.Usage == nil || resp.Usage.InputExclusive || resp.Usage.InputTokens != 120 || resp.Usage.CachedReadTokens != 30 || resp.Usage.ReasoningTokens != 15 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}

	// Encode back and verify round-trip.
	encoded, err := c.EncodeResponse(resp, ctx)
	if err != nil {
		t.Fatalf("EncodeResponse error: %v", err)
	}
	resp2, err := c.DecodeResponse(encoded, ctx)
	if err != nil {
		t.Fatalf("second DecodeResponse error: %v", err)
	}
	if resp2.ID != "chatcmpl-999" || resp2.Choices[0].FinishReason != ir.FinishToolCalls || resp2.Usage.CachedReadTokens != 30 || resp2.Usage.ReasoningTokens != 15 {
		t.Fatalf("round-trip response mismatch: %+v", resp2)
	}

	// Verify Anthropic exclusive usage encodes to inclusive prompt_tokens + auto-generated chatcmpl ID.
	anthResp := &ir.Response{
		Model: "claude-sonnet-4-5",
		Choices: []ir.Choice{{
			Index:        0,
			Message:      ir.Message{Role: ir.RoleAssistant, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Hi"}}},
			FinishReason: ir.FinishStop,
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
	anthEncoded, err := c.EncodeResponse(anthResp, ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("EncodeResponse(anthropic) error: %v", err)
	}
	var anthWire struct {
		ID     string         `json:"id"`
		Object string         `json:"object"`
		Usage  *chatUsageWire `json:"usage"`
	}
	if err := json.Unmarshal(anthEncoded, &anthWire); err != nil {
		t.Fatalf("unmarshal anthropic encoded: %v", err)
	}
	if !strings.HasPrefix(anthWire.ID, "chatcmpl-") || anthWire.Object != "chat.completion" {
		t.Fatalf("unexpected id/object: id=%q object=%q", anthWire.ID, anthWire.Object)
	}
	if anthWire.Usage.PromptTokens != 160 || anthWire.Usage.CompletionTokens != 50 || anthWire.Usage.TotalTokens != 210 {
		t.Fatalf("expected inclusive usage 160/50/210, got %+v", anthWire.Usage)
	}
	if anthWire.Usage.PromptTokensDetails == nil || anthWire.Usage.PromptTokensDetails.CachedTokens != 40 || anthWire.Usage.PromptTokensDetails.CachedCreationTokens != 20 {
		t.Fatalf("unexpected prompt_tokens_details: %+v", anthWire.Usage.PromptTokensDetails)
	}
}

func TestStreamRoundTripPipelineWithReasoningAndParallelTools(t *testing.T) {
	reg := ir.NewRegistry()
	reg.Register(New())

	pipe, err := ir.NewPipeline(ir.PipelineConfig{
		Registry:     reg,
		SourceFormat: ir.FormatOpenAIChat,
		TargetFormat: ir.FormatOpenAIChat,
	})
	if err != nil {
		t.Fatalf("NewPipeline error: %v", err)
	}

	sseInput := strings.Join([]string{
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"reasoning_content":"Let me check both cities."},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Checking..."},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\"NYC\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"\"LA\"}"}}]},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		``,
		`data: {"id":"chatcmpl-stream-1","object":"chat.completion.chunk","created":1700000200,"model":"gpt-4o","choices":[],"usage":{"prompt_tokens":80,"completion_tokens":35,"total_tokens":115,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":12}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	translator := pipe.NewStreamTranslator("fallback-model")
	var frames []ir.SSEFrame
	if err := translator.TranslateReader(strings.NewReader(sseInput), func(f ir.SSEFrame) error {
		frames = append(frames, f)
		return nil
	}); err != nil {
		t.Fatalf("TranslateReader error: %v", err)
	}

	if len(frames) < 9 {
		t.Fatalf("expected at least 9 SSE frames, got %d", len(frames))
	}
	if string(frames[len(frames)-1].Data) != "[DONE]" {
		t.Fatalf("expected final frame [DONE], got %q", string(frames[len(frames)-1].Data))
	}

	// Re-aggregate emitted frames to verify exact reconstruction.
	var textBuf, reasoningBuf strings.Builder
	toolNames := make(map[int]string)
	toolIDs := make(map[int]string)
	toolArgs := make(map[int]*strings.Builder)
	var finishReason string
	var gotUsage *chatUsageWire
	var sawInitialRole bool

	for _, f := range frames[:len(frames)-1] {
		var ch chatStreamChunkWire
		if err := json.Unmarshal(f.Data, &ch); err != nil {
			t.Fatalf("unmarshal emitted chunk %q: %v", string(f.Data), err)
		}
		if ch.ID != "chatcmpl-stream-1" || ch.Model != "gpt-4o" || ch.Object != "chat.completion.chunk" || int64(ch.Created) != 1700000200 {
			t.Fatalf("unexpected chunk envelope: %+v", ch)
		}
		if ch.Usage != nil {
			gotUsage = ch.Usage
			// Verify choices marshaled as [] not null.
			if !strings.Contains(string(f.Data), `"choices":[]`) {
				t.Fatalf("expected empty choices array in usage chunk, got %s", string(f.Data))
			}
		}
		for _, c0 := range ch.Choices {
			if c0.Delta.Role == "assistant" {
				sawInitialRole = true
			}
			if c0.Delta.Content != nil {
				textBuf.WriteString(*c0.Delta.Content)
			}
			if c0.Delta.ReasoningContent != nil {
				reasoningBuf.WriteString(*c0.Delta.ReasoningContent)
			}
			for _, tc := range c0.Delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}
				if tc.ID != "" {
					toolIDs[idx] = tc.ID
				}
				if tc.Function != nil {
					if tc.Function.Name != "" {
						toolNames[idx] = tc.Function.Name
					}
					if toolArgs[idx] == nil {
						toolArgs[idx] = &strings.Builder{}
					}
					toolArgs[idx].WriteString(tc.Function.Arguments)
				}
			}
			if c0.FinishReason != nil {
				finishReason = *c0.FinishReason
			}
		}
	}

	if !sawInitialRole {
		t.Fatalf("expected initial role assistant chunk")
	}
	if reasoningBuf.String() != "Let me check both cities." {
		t.Fatalf("reasoning mismatch: %q", reasoningBuf.String())
	}
	if textBuf.String() != "Checking..." {
		t.Fatalf("text mismatch: %q", textBuf.String())
	}
	if toolIDs[0] != "call_a" || toolNames[0] != "get_weather" || toolArgs[0].String() != `{"city":"NYC"}` {
		t.Fatalf("tool 0 mismatch: id=%q name=%q args=%q", toolIDs[0], toolNames[0], toolArgs[0].String())
	}
	if toolIDs[1] != "call_b" || toolNames[1] != "get_weather" || toolArgs[1].String() != `{"city":"LA"}` {
		t.Fatalf("tool 1 mismatch: id=%q name=%q args=%q", toolIDs[1], toolNames[1], toolArgs[1].String())
	}
	if finishReason != "tool_calls" {
		t.Fatalf("expected finish_reason=tool_calls, got %q", finishReason)
	}
	if gotUsage == nil || gotUsage.PromptTokens != 80 || gotUsage.CompletionTokens != 35 || gotUsage.TotalTokens != 115 {
		t.Fatalf("unexpected usage chunk: %+v", gotUsage)
	}
	if translator.Usage() == nil || translator.Usage().CachedReadTokens != 20 || translator.Usage().ReasoningTokens != 12 {
		t.Fatalf("unexpected translator usage: %+v", translator.Usage())
	}
}

func TestStreamBufferedUsageFlushAndError(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	ctx.Model = "claude-sonnet-4-5"

	// Early usage (Anthropic message_start style) should be buffered until finish/flush.
	frames, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type: ir.EventUsage,
		Usage: &ir.Usage{
			InputTokens:    100,
			InputExclusive: true,
		},
	}, ctx)
	if err != nil || len(frames) != 0 {
		t.Fatalf("expected early usage to be buffered, got frames=%d err=%v", len(frames), err)
	}

	// Start directly with two tool calls sharing ToolCallIndex=0 but distinct IDs.
	f1, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:          ir.EventToolCallStart,
		ToolCallIndex: 0,
		ToolCallID:    "toolu_1",
		ToolName:      "search",
	}, ctx)
	if err != nil || len(f1) != 1 || !strings.Contains(string(f1[0].Data), `"role":"assistant"`) || !strings.Contains(string(f1[0].Data), `"index":0`) {
		t.Fatalf("unexpected first tool start frame: %v %s", err, string(f1[0].Data))
	}
	f2, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:          ir.EventToolCallStart,
		ToolCallIndex: 0, // intentionally colliding index with distinct ID
		ToolCallID:    "toolu_2",
		ToolName:      "fetch",
	}, ctx)
	if err != nil || len(f2) != 1 || !strings.Contains(string(f2[0].Data), `"index":1`) {
		t.Fatalf("expected second distinct tool call to get index 1, got %v %s", err, string(f2[0].Data))
	}

	// Late usage delta before FlushStream (no explicit EventFinish).
	frames, err = c.EncodeStreamEvent(ir.StreamEvent{
		Type: ir.EventUsage,
		Usage: &ir.Usage{
			OutputTokens:     25,
			CachedReadTokens: 10,
		},
	}, ctx)
	if err != nil || len(frames) != 0 {
		t.Fatalf("expected pre-finish usage to remain buffered, got %d frames", len(frames))
	}

	// FlushStream should emit: empty "{}" args for the 2 zero-arg tools -> finish(tool_calls) -> merged usage(prompt=110, completion=25) -> [DONE].
	flushed, err := c.FlushStream(ctx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	if len(flushed) != 5 {
		t.Fatalf("expected 5 flush frames (2x '{}' args, finish, usage, [DONE]), got %d", len(flushed))
	}
	if !strings.Contains(string(flushed[0].Data), `"arguments":"{}"`) || !strings.Contains(string(flushed[1].Data), `"arguments":"{}"`) {
		t.Fatalf("expected zero-arg tools to emit '{}' before finish, got %s | %s", string(flushed[0].Data), string(flushed[1].Data))
	}
	if !strings.Contains(string(flushed[2].Data), `"finish_reason":"tool_calls"`) {
		t.Fatalf("expected tool_calls finish chunk, got %s", string(flushed[2].Data))
	}
	if !strings.Contains(string(flushed[3].Data), `"prompt_tokens":110`) || !strings.Contains(string(flushed[3].Data), `"completion_tokens":25`) {
		t.Fatalf("expected merged inclusive usage chunk, got %s", string(flushed[3].Data))
	}
	if string(flushed[4].Data) != "[DONE]" {
		t.Fatalf("expected [DONE], got %q", string(flushed[4].Data))
	}

	// Second FlushStream must be idempotent.
	flushed2, err := c.FlushStream(ctx)
	if err != nil || len(flushed2) != 0 {
		t.Fatalf("expected second FlushStream to emit nothing, got %d frames err=%v", len(flushed2), err)
	}

	// Verify include_usage=false suppresses final usage chunk.
	ctxNoUsage := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	ctxNoUsage.Options["include_usage"] = false
	_, _ = c.EncodeStreamEvent(ir.StreamEvent{Type: ir.EventTextDelta, Text: "ok"}, ctxNoUsage)
	_, _ = c.EncodeStreamEvent(ir.StreamEvent{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 10, OutputTokens: 5}}, ctxNoUsage)
	flushedNoUsage, err := c.FlushStream(ctxNoUsage)
	if err != nil || len(flushedNoUsage) != 2 {
		t.Fatalf("expected 2 frames (finish + [DONE]) when include_usage=false, got %d err=%v", len(flushedNoUsage), err)
	}

	// Embedded stream error decode & encode.
	errEvents, err := c.DecodeStreamEvent("", []byte(`{"error":{"message":"rate limit","type":"rate_limit_error","code":429}}`), nil)
	if err != nil || len(errEvents) != 1 || errEvents[0].Type != ir.EventError || errEvents[0].ErrorCode != "429" {
		t.Fatalf("unexpected decoded error event: %+v err=%v", errEvents, err)
	}
	errFrames, err := c.EncodeStreamEvent(errEvents[0], nil)
	if err != nil || len(errFrames) != 1 || !strings.Contains(string(errFrames[0].Data), `"rate limit"`) {
		t.Fatalf("unexpected encoded error frame: %+v err=%v", errFrames, err)
	}

	// Stream reasoning alias + refusal delta + [DONE] direct decode.
	decCtx := ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatAnthropic)
	evs, err := c.DecodeStreamEvent("", []byte(`{"id":"chatcmpl-alias","model":"deepseek-r1","choices":[{"index":0,"delta":{"reasoning":"pondering","refusal":"cannot comply"}}]}`), decCtx)
	if err != nil {
		t.Fatalf("DecodeStreamEvent(reasoning/refusal) error: %v", err)
	}
	if len(evs) != 3 || evs[0].Type != ir.EventStreamStart || evs[1].Type != ir.EventReasoningDelta || evs[1].Reasoning != "pondering" || evs[2].Type != ir.EventRefusalDelta || evs[2].Refusal != "cannot comply" {
		t.Fatalf("unexpected reasoning/refusal stream events: %+v", evs)
	}
	refFrame, err := c.EncodeStreamEvent(evs[2], ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat))
	if err != nil || len(refFrame) != 1 || !strings.Contains(string(refFrame[0].Data), `"refusal":"cannot comply"`) {
		t.Fatalf("unexpected encoded refusal frame: %+v err=%v", refFrame, err)
	}
	doneEvs, err := c.DecodeStreamEvent("", []byte("[DONE]"), decCtx)
	if err != nil || len(doneEvs) != 1 || doneEvs[0].Type != ir.EventStreamEnd {
		t.Fatalf("expected EventStreamEnd for [DONE], got %+v err=%v", doneEvs, err)
	}
}

func TestRequestCacheControlStopArrayAndDisableParallel(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat)

	raw := `{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": [{"type": "text", "text": "Cached sys", "cache_control": {"type": "ephemeral", "ttl": "5m"}}]},
			{"role": "user", "content": "Hi"}
		],
		"stop": ["END", "HALT"],
		"tool_choice": "required"
	}`
	req, err := c.DecodeRequest([]byte(raw), ctx)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if len(req.System) != 1 || req.System[0].CacheControl == nil || req.System[0].CacheControl.Type != "ephemeral" || req.System[0].CacheControl.TTL != "5m" {
		t.Fatalf("expected system cache_control preserved, got %+v", req.System)
	}
	if len(req.Generation.StopSequences) != 2 || req.Generation.StopSequences[0] != "END" || req.Generation.StopSequences[1] != "HALT" {
		t.Fatalf("stop array mismatch: %+v", req.Generation.StopSequences)
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "required" {
		t.Fatalf("tool_choice mismatch: %+v", req.ToolChoice)
	}

	// Test DisableParallel fallback when ParallelToolCalls is nil.
	req.ToolChoice = &ir.ToolChoice{Mode: "any", DisableParallel: boolPtr(true)}
	encoded, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if out["tool_choice"] != "required" {
		t.Fatalf("expected Anthropic mode 'any' mapped to 'required', got %#v", out["tool_choice"])
	}
	if out["parallel_tool_calls"] != false {
		t.Fatalf("expected DisableParallel=true mapped to parallel_tool_calls=false, got %#v", out["parallel_tool_calls"])
	}
	if !strings.Contains(string(encoded), `"cache_control":{"type":"ephemeral","ttl":"5m"}`) {
		t.Fatalf("expected system part cache_control preserved in encoded JSON: %s", string(encoded))
	}
}

func TestResponseRefusalReasoningAliasAndError(t *testing.T) {
	c := New()

	// Upstream error payload returns error.
	if _, err := c.DecodeResponse([]byte(`{"error":{"message":"invalid key","type":"authentication_error"}}`), nil); err == nil {
		t.Fatalf("expected error for upstream error response")
	}

	// Reasoning alias + refusal round-trip.
	raw := `{
		"id": "chatcmpl-ref-1",
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"reasoning": "Checking policy",
				"content": null,
				"refusal": "I cannot help with that."
			},
			"finish_reason": "content_filter"
		}]
	}`
	resp, err := c.DecodeResponse([]byte(raw), nil)
	if err != nil {
		t.Fatalf("DecodeResponse error: %v", err)
	}
	if resp.Choices[0].FinishReason != ir.FinishContentFilter {
		t.Fatalf("expected FinishContentFilter, got %q", resp.Choices[0].FinishReason)
	}
	if len(resp.Choices[0].Message.Parts) != 2 || resp.Choices[0].Message.Parts[0].Reasoning.Text != "Checking policy" || resp.Choices[0].Message.Parts[1].Refusal != "I cannot help with that." {
		t.Fatalf("unexpected refusal/reasoning parts: %+v", resp.Choices[0].Message.Parts)
	}
	encoded, err := c.EncodeResponse(resp, nil)
	if err != nil {
		t.Fatalf("EncodeResponse error: %v", err)
	}
	if !strings.Contains(string(encoded), `"content":null`) || !strings.Contains(string(encoded), `"refusal":"I cannot help with that."`) || !strings.Contains(string(encoded), `"reasoning_content":"Checking policy"`) {
		t.Fatalf("unexpected encoded refusal response: %s", string(encoded))
	}
}
