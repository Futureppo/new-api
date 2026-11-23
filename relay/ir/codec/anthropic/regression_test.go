package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func TestMergedMessagesHoistToolResultAndThinkingFirst(t *testing.T) {
	c := New()
	req := &ir.Request{
		Model:      "claude-sonnet-4-5",
		Generation: ir.GenerationConfig{MaxTokens: intPtr(1024)},
		Messages: []ir.Message{
			{
				Role:  ir.RoleUser,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "extra user note"}},
			},
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{{
					Type:       ir.PartToolResult,
					ToolResult: &ir.ToolResult{ToolCallID: "toolu_1", Content: "result ok"},
				}},
			},
			{
				Role:  ir.RoleAssistant,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "answer part"}},
			},
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{{
					Type:      ir.PartReasoning,
					Reasoning: &ir.Reasoning{Text: "deep thought", Signature: "sig_1", OriginFormat: ir.FormatAnthropic},
				}},
			},
		},
	}

	raw, err := c.EncodeRequest(req, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	var wire struct {
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(wire.Messages) != 2 {
		t.Fatalf("expected 2 merged messages (user + assistant), got %d", len(wire.Messages))
	}
	if wire.Messages[0].Content[0]["type"] != "tool_result" {
		t.Fatalf("expected tool_result hoisted first in merged user message, got: %+v", wire.Messages[0].Content)
	}
	if wire.Messages[1].Content[0]["type"] != "thinking" {
		t.Fatalf("expected thinking hoisted first in merged assistant message, got: %+v", wire.Messages[1].Content)
	}
}

func TestAdaptiveAndOpus47AndBareReasoningEffort(t *testing.T) {
	c := New()
	temp := 0.7
	topP := 0.9
	topK := 40

	// 1. Adaptive mode must strip temperature, top_p, and top_k.
	reqAdaptive := &ir.Request{
		Model:    "claude-opus-4-6",
		Messages: []ir.Message{{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}}}},
		Generation: ir.GenerationConfig{
			MaxTokens:   intPtr(2048),
			Temperature: &temp,
			TopP:        &topP,
			TopK:        &topK,
		},
		Reasoning: &ir.ReasoningConfig{Mode: "adaptive", Effort: "high"},
	}
	rawAdaptive, err := c.EncodeRequest(reqAdaptive, ir.NewConversionContext(ir.FormatAnthropic, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeRequest adaptive error: %v", err)
	}
	var mAdaptive map[string]any
	_ = json.Unmarshal(rawAdaptive, &mAdaptive)
	if mAdaptive["temperature"] != 1.0 {
		t.Fatalf("expected temperature adjusted to 1.0 in adaptive thinking mode, got: %s", string(rawAdaptive))
	}
	for _, forbidden := range []string{"top_p", "top_k"} {
		if _, ok := mAdaptive[forbidden]; ok {
			t.Fatalf("expected %s stripped in adaptive thinking mode, got: %s", forbidden, string(rawAdaptive))
		}
	}

	// 2. Opus 4.7+ must convert thinking.type="enabled" to "adaptive" and strip sampling params.
	reqOpus47 := &ir.Request{
		Model:      "claude-opus-4-7",
		Messages:   []ir.Message{{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}}}},
		Generation: ir.GenerationConfig{MaxTokens: intPtr(2048), Temperature: &temp, TopP: &topP},
		Reasoning:  &ir.ReasoningConfig{Mode: "enabled", BudgetTokens: intPtr(4096)},
	}
	rawOpus47, err := c.EncodeRequest(reqOpus47, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeRequest opus-4-7 error: %v", err)
	}
	var mOpus47 map[string]any
	_ = json.Unmarshal(rawOpus47, &mOpus47)
	thinking47, _ := mOpus47["thinking"].(map[string]any)
	if thinking47 == nil || thinking47["type"] != "adaptive" {
		t.Fatalf("expected Opus 4.7 to encode adaptive thinking, got: %s", string(rawOpus47))
	}
	if _, hasTemp := mOpus47["temperature"]; hasTemp {
		t.Fatalf("expected Opus 4.7 to strip temperature, got: %s", string(rawOpus47))
	}

	// 3. Bare reasoning_effort (no Mode/BudgetTokens) from OpenAI Chat on Sonnet 4.5 must derive enabled thinking.
	reqBare := &ir.Request{
		Model:      "claude-sonnet-4-5",
		Generation: ir.GenerationConfig{MaxTokens: intPtr(2048)},
		Messages:   []ir.Message{{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}}}},
		Reasoning:  &ir.ReasoningConfig{Effort: "high"},
	}
	rawBare, err := c.EncodeRequest(reqBare, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeRequest bare effort error: %v", err)
	}
	var mBare map[string]any
	_ = json.Unmarshal(rawBare, &mBare)
	thinkingBare, _ := mBare["thinking"].(map[string]any)
	if thinkingBare == nil || thinkingBare["type"] != "enabled" {
		t.Fatalf("expected bare reasoning_effort on Sonnet 4.5 to derive enabled thinking, got: %s", string(rawBare))
	}
}

func TestMessageStartCarriesInitialUsageWithZeroOutputTokens(t *testing.T) {
	c := New()
	decCtx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatAnthropic)
	evs, err := c.DecodeStreamEvent("message_start", []byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4-5","usage":{"input_tokens":120,"cache_read_input_tokens":30,"cache_creation_input_tokens":10,"output_tokens":5}}}`), decCtx)
	if err != nil {
		t.Fatalf("DecodeStreamEvent message_start error: %v", err)
	}
	if len(evs) == 0 || evs[0].Type != ir.EventStreamStart || evs[0].Usage == nil {
		t.Fatalf("expected EventStreamStart to carry initial Usage, got %+v", evs)
	}

	encCtx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatAnthropic)
	encCtx.Model = "claude-sonnet-4-5"
	frames, err := c.EncodeStreamEvent(evs[0], encCtx)
	if err != nil {
		t.Fatalf("EncodeStreamEvent message_start error: %v", err)
	}
	if len(frames) != 1 || frames[0].Event != "message_start" {
		t.Fatalf("expected 1 message_start frame, got %+v", frames)
	}
	var msgStart struct {
		Message struct {
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				OutputTokens             int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(frames[0].Data, &msgStart); err != nil {
		t.Fatalf("unmarshal message_start: %v", err)
	}
	u := msgStart.Message.Usage
	if u.InputTokens != 120 || u.CacheReadInputTokens != 30 || u.CacheCreationInputTokens != 10 || u.OutputTokens != 0 {
		t.Fatalf("expected message_start usage (120/30/10, output=0), got %+v", u)
	}
}

func TestNoCacheControlOnThinkingToolChoiceGuardsAndLowercaseSchemaTypes(t *testing.T) {
	c := New()
	req := &ir.Request{
		Model:      "claude-sonnet-4-5",
		Generation: ir.GenerationConfig{MaxTokens: intPtr(4096)},
		Messages: []ir.Message{
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{
						Type:         ir.PartReasoning,
						Reasoning:    &ir.Reasoning{Text: "plan", Signature: "sig_abc", OriginFormat: ir.FormatAnthropic},
						CacheControl: &ir.CacheControl{Type: "ephemeral"},
					},
					{
						Type: ir.PartText,
						Text: "done",
					},
				},
			},
			{
				Role:  ir.RoleUser,
				Parts: []ir.ContentPart{{Type: ir.PartText, Text: "next"}},
			},
		},
		Tools: []ir.ToolDefinition{{
			Type:       "function",
			Name:       "lookup",
			Parameters: json.RawMessage(`{"type":"OBJECT","properties":{"query":{"type":"STRING"}}}`),
		}},
		ToolChoice: &ir.ToolChoice{Mode: "required"},
		Reasoning:  &ir.ReasoningConfig{Mode: "enabled", BudgetTokens: intPtr(2048)},
	}

	raw, err := c.EncodeRequest(req, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	var wire map[string]any
	_ = json.Unmarshal(raw, &wire)

	// Thinking block must NOT have cache_control.
	firstMsg := wire["messages"].([]any)[0].(map[string]any)
	firstBlock := firstMsg["content"].([]any)[0].(map[string]any)
	if firstBlock["type"] == "thinking" {
		if _, hasCC := firstBlock["cache_control"]; hasCC {
			t.Fatalf("thinking block must not carry cache_control: %+v", firstBlock)
		}
	}

	// Thinking enabled + forced tool_choice ("any") conflict must disable manual thinking so forced tool_choice succeeds.
	if _, hasThinking := wire["thinking"]; hasThinking {
		t.Fatalf("expected manual thinking disabled when tool_choice forces tool use (any), got: %s", string(raw))
	}

	// Uppercase Gemini schema types must be recursively lowercased.
	if strings.Contains(string(raw), `"OBJECT"`) || strings.Contains(string(raw), `"STRING"`) {
		t.Fatalf("expected uppercase Gemini schema types lowercased, got: %s", string(raw))
	}

	// ToolChoice without any tools must be omitted.
	reqNoTools := &ir.Request{
		Model:      "claude-sonnet-4-5",
		Generation: ir.GenerationConfig{MaxTokens: intPtr(1024)},
		Messages:   []ir.Message{{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}}}},
		ToolChoice: &ir.ToolChoice{Mode: "auto"},
	}
	rawNoTools, err := c.EncodeRequest(reqNoTools, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("EncodeRequest no tools error: %v", err)
	}
	if strings.Contains(string(rawNoTools), `"tool_choice"`) {
		t.Fatalf("expected tool_choice omitted when tools list is empty, got: %s", string(rawNoTools))
	}
}

func TestInterleavedParallelToolsAndBridgePersistenceOnStreamEnd(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatAnthropic)
	ctx.Model = "gpt-5"

	var frames []ir.SSEFrame
	events := []ir.StreamEvent{
		{Type: ir.EventToolCallStart, ToolCallIndex: 0, ToolCallID: "call_0", ToolName: "fn_0"},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "call_0", ToolArguments: `{"a":`},
		// Interleaved delta for tool 1 while tool 0 is still open!
		{Type: ir.EventToolCallStart, ToolCallIndex: 1, ToolCallID: "call_1", ToolName: "fn_1"},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 1, ToolCallID: "call_1", ToolArguments: `{"b":2}`},
		// Remaining delta for tool 0 arrives after tool 1's delta!
		{Type: ir.EventToolCallDelta, ToolCallIndex: 0, ToolCallID: "call_0", ToolArguments: `1}`},
		{Type: ir.EventFinish, FinishReason: ir.FinishToolCalls},
	}
	for _, ev := range events {
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

	// Reconstruct JSON per Anthropic content block index and verify strict sequential block lifecycle.
	openIdx := -1
	jsonByBlock := map[int]*strings.Builder{}
	for _, f := range frames {
		var m map[string]any
		if json.Unmarshal(f.Data, &m) != nil {
			continue
		}
		switch f.Event {
		case "content_block_start":
			idx := int(m["index"].(float64))
			if openIdx != -1 {
				t.Fatalf("opened block %d while block %d was still open", idx, openIdx)
			}
			openIdx = idx
			jsonByBlock[idx] = &strings.Builder{}
		case "content_block_delta":
			idx := int(m["index"].(float64))
			if idx != openIdx {
				t.Fatalf("delta emitted for closed/wrong block %d (open=%d)", idx, openIdx)
			}
			if d, ok := m["delta"].(map[string]any); ok {
				if pj, ok := d["partial_json"].(string); ok {
					jsonByBlock[idx].WriteString(pj)
				}
			}
		case "content_block_stop":
			idx := int(m["index"].(float64))
			if idx != openIdx {
				t.Fatalf("stopped wrong block %d (open=%d)", idx, openIdx)
			}
			openIdx = -1
		}
	}
	if len(jsonByBlock) != 2 || jsonByBlock[0].String() != `{"a":1}` || jsonByBlock[1].String() != `{"b":2}` {
		t.Fatalf("unexpected reconstructed parallel tool JSON: block0=%q block1=%q", jsonByBlock[0].String(), jsonByBlock[1].String())
	}
}
