package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func TestEventErrorKeepsTypeErrorAndTerminatesStream(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIResponses)
	ctx.Model = "claude-sonnet-4-5"

	frames, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:         ir.EventError,
		ErrorType:    "overloaded_error",
		ErrorCode:    "overloaded_error",
		ErrorMessage: "upstream overloaded",
	}, ctx)
	if err != nil {
		t.Fatalf("EncodeStreamEvent error: %v", err)
	}
	if len(frames) != 1 || frames[0].Event != "error" {
		t.Fatalf("expected 1 SSE event:error frame, got %+v", frames)
	}
	var payload map[string]any
	if err := json.Unmarshal(frames[0].Data, &payload); err != nil {
		t.Fatalf("unmarshal error frame: %v", err)
	}
	if payload["type"] != "error" {
		t.Fatalf("expected payload.type == \"error\" (not overwritten by ErrorType), got %v", payload["type"])
	}
	if payload["code"] != "overloaded_error" {
		t.Fatalf("expected payload.code == \"overloaded_error\", got %v", payload["code"])
	}

	tail, err := c.FlushStream(ctx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("expected FlushStream to emit 0 frames after EventError (no fake response.completed), got %d", len(tail))
	}
}

func TestParallelToolCallsSameIndexAndAnthropicMultiToolClose(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIResponses)
	ctx.Model = "claude-sonnet-4-5"

	var frames []ir.SSEFrame
	events := []ir.StreamEvent{
		{Type: ir.EventToolCallStart, BlockIndex: 0, ToolCallIndex: 0, ToolCallID: "toolu_1", ToolName: "fn_one", ToolKind: "function"},
		{Type: ir.EventToolCallDelta, BlockIndex: 0, ToolCallIndex: 0, ToolCallID: "toolu_1", ToolArguments: `{"a":1}`},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0},
		// Second tool call arrives with colliding ToolCallIndex=0 and distinct BlockIndex=1 & ToolCallID="toolu_2".
		{Type: ir.EventToolCallStart, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "toolu_2", ToolName: "fn_two", ToolKind: "function"},
		{Type: ir.EventToolCallDelta, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "toolu_2", ToolArguments: `{"b":2}`},
		// Anthropic content_block_stop only carries BlockIndex=1 (ToolCallID="", ToolCallIndex=0).
		{Type: ir.EventContentBlockEnd, BlockIndex: 1},
		// Stray delta with unknown ID after both tools closed must NOT contaminate toolu_2.
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "toolu_stray", ToolArguments: `{"bad":true}`},
		{Type: ir.EventFinish, FinishReason: ir.FinishToolCalls},
	}
	for _, ev := range events {
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
	frames = append(frames, tail...)

	var doneCount int
	var completed map[string]any
	for _, f := range frames {
		if f.Event == "response.output_item.done" {
			doneCount++
		}
		if f.Event == "response.completed" {
			_ = json.Unmarshal(f.Data, &completed)
		}
	}
	if doneCount != 2 {
		t.Fatalf("expected 2 output_item.done frames for 2 parallel tools, got %d", doneCount)
	}
	respObj := completed["response"].(map[string]any)
	output := respObj["output"].([]any)
	if len(output) != 2 {
		t.Fatalf("expected 2 function_call items in response.completed.output, got %d: %+v", len(output), output)
	}
	if output[0].(map[string]any)["call_id"] != "toolu_1" || output[1].(map[string]any)["call_id"] != "toolu_2" {
		t.Fatalf("parallel tool calls corrupted: %+v", output)
	}
	if output[1].(map[string]any)["arguments"] != `{"b":2}` {
		t.Fatalf("toolu_2 arguments contaminated: %v", output[1].(map[string]any)["arguments"])
	}
}

func TestNormalizeResponsesToolItemIDCap64AndCustomToolPrefix(t *testing.T) {
	longCallID := "call_" + strings.Repeat("a", 70)
	fcID := normalizeResponsesToolItemID("", longCallID, "function", 0)
	if !strings.HasPrefix(fcID, "fc_") || len(fcID) > 64 {
		t.Fatalf("expected fc_ prefix and <= 64 chars, got %q (len=%d)", fcID, len(fcID))
	}
	// Deterministic hashing.
	if fcID2 := normalizeResponsesToolItemID("", longCallID, "function", 0); fcID2 != fcID {
		t.Fatalf("expected deterministic normalized ID, got %q vs %q", fcID, fcID2)
	}

	ctcID := normalizeResponsesToolItemID("", longCallID, "custom", 0)
	if !strings.HasPrefix(ctcID, "ctc_") || len(ctcID) > 64 {
		t.Fatalf("expected ctc_ prefix and <= 64 chars for custom tool, got %q (len=%d)", ctcID, len(ctcID))
	}

	// Custom tool result must encode as custom_tool_call_output.
	c := New()
	raw, err := c.EncodeRequest(&ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{{
					Type:     ir.PartToolCall,
					ToolCall: &ir.ToolCall{ID: "call_custom_1", Name: "bash_exec", Kind: "custom", Arguments: "ls -la"},
				}},
			},
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{{
					Type:       ir.PartToolResult,
					ToolResult: &ir.ToolResult{ToolCallID: "call_custom_1", Content: "file.txt"},
				}},
			},
		},
	}, ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatOpenAIResponses))
	if err != nil {
		t.Fatalf("EncodeRequest custom tool error: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"custom_tool_call"`) || !strings.Contains(string(raw), `"type":"custom_tool_call_output"`) {
		t.Fatalf("expected custom_tool_call and custom_tool_call_output in encoded request, got: %s", string(raw))
	}
}

func TestMultiItemDecodeOutputItemDoneAndZeroIntermediateUsageAndIncompleteStatus(t *testing.T) {
	c := New()
	decCtx := ir.NewStreamContext(ir.FormatOpenAIResponses, ir.FormatOpenAIChat)

	// Item 1 streamed via delta; Item 2 delivered only via output_item.done (no delta).
	// Item 2 MUST NOT be suppressed by global AccumulatedText.
	chunks := []struct {
		event string
		data  string
	}{
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`},
		{"response.output_text.delta", `{"type":"response.output_text.delta","output_index":0,"item_id":"msg_1","delta":"First"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"First"}]}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":1,"item":{"id":"msg_2","type":"message","role":"assistant","content":[]}}`},
		{"response.output_item.done", `{"type":"response.output_item.done","output_index":1,"item":{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"Second"}]}}`},
	}
	var allText strings.Builder
	for _, ch := range chunks {
		evs, err := c.DecodeStreamEvent(ch.event, []byte(ch.data), decCtx)
		if err != nil {
			t.Fatalf("DecodeStreamEvent(%s) error: %v", ch.event, err)
		}
		for _, ev := range evs {
			if ev.Type == ir.EventTextDelta {
				allText.WriteString(ev.Text)
			}
		}
	}
	if allText.String() != "FirstSecond" {
		t.Fatalf("expected both item 1 and non-delta item 2 text (\"FirstSecond\"), got %q", allText.String())
	}

	// Zero intermediate usage before EventFinish must NOT trigger premature response.completed.
	encCtx := ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatOpenAIResponses)
	encCtx.Model = "gpt-5"
	var outFrames []ir.SSEFrame
	for _, ev := range []ir.StreamEvent{
		{Type: ir.EventTextDelta, Text: "Hi"},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 0, OutputTokens: 0, TotalTokens: 0}},
		{Type: ir.EventFinish, FinishReason: ir.FinishLength},
		{Type: ir.EventUsage, Usage: &ir.Usage{InputTokens: 12, OutputTokens: 34, TotalTokens: 46}},
	} {
		out, err := c.EncodeStreamEvent(ev, encCtx)
		if err != nil {
			t.Fatalf("EncodeStreamEvent error: %v", err)
		}
		outFrames = append(outFrames, out...)
	}
	tail, err := c.FlushStream(encCtx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	outFrames = append(outFrames, tail...)

	last := outFrames[len(outFrames)-1]
	if last.Event != "response.incomplete" {
		t.Fatalf("expected terminal event response.incomplete, got %q", last.Event)
	}
	var incompletePayload struct {
		Response struct {
			Status string           `json:"status"`
			Output []map[string]any `json:"output"`
			Usage  struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(last.Data, &incompletePayload); err != nil {
		t.Fatalf("unmarshal response.incomplete: %v", err)
	}
	if incompletePayload.Response.Usage.TotalTokens != 46 {
		t.Fatalf("expected deferred final usage (46) preserved despite zero intermediate usage, got %d", incompletePayload.Response.Usage.TotalTokens)
	}
	if len(incompletePayload.Response.Output) == 0 || incompletePayload.Response.Output[0]["status"] != "incomplete" {
		t.Fatalf("expected inner output item status \"incomplete\" on response.incomplete, got %+v", incompletePayload.Response.Output)
	}
}

func TestDecodeResponseTopLevelErrorAndBuiltinToolChoiceAndSchemaName(t *testing.T) {
	c := New()

	// Pure top-level error envelope in HTTP 200 body must return error (not FinishStop).
	_, err := c.DecodeResponse([]byte(`{"error":{"code":"rate_limit_exceeded","message":"too many requests"}}`), ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatOpenAIChat))
	if err == nil || !strings.Contains(err.Error(), "too many requests") {
		t.Fatalf("expected DecodeResponse to surface pure error envelope, got err=%v", err)
	}

	// Built-in tool_choice + Google responseSchema missing name + parameters: null omission.
	req := &ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{{
			Role:  ir.RoleUser,
			Parts: []ir.ContentPart{{Type: ir.PartText, Text: "search"}},
		}},
		Tools: []ir.ToolDefinition{{
			Type:       "function",
			Name:       "ping",
			Parameters: json.RawMessage(`null`),
		}},
		ToolChoice: &ir.ToolChoice{
			Mode:     "tool",
			ToolType: "web_search_preview",
		},
		ResponseFormat: &ir.ResponseFormat{
			Type:   "json_schema",
			Schema: json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
		},
	}
	raw, err := c.EncodeRequest(req, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatOpenAIResponses))
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, `"tool_choice":{"type":"web_search_preview"}`) {
		t.Fatalf("expected built-in tool_choice object, got: %s", s)
	}
	if !strings.Contains(s, `"name":"response"`) {
		t.Fatalf("expected default text.format.name=\"response\", got: %s", s)
	}
	if strings.Contains(s, `"parameters":null`) {
		t.Fatalf("expected parameters:null omitted, got: %s", s)
	}
}
