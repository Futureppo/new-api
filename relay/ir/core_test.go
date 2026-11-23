package ir

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTurnBridgeSaveLoadAndTTLExpiry(t *testing.T) {
	now := time.Unix(1700000000, 0)
	tb := NewTurnBridge(10*time.Minute, 2)
	tb.now = func() time.Time { return now }

	state1 := NewBridgeState()
	state1.RecordToolCall(ToolCallBridgeEntry{
		ToolCallID:       "call_1",
		ResponsesItemID:  "fc_1",
		ToolName:         "get_weather",
		ThoughtSignature: "sig_real_1",
		OriginFormat:     FormatGoogleGenAI,
	})
	tb.Save("chan:1", state1)

	req := &Request{
		Model: "gemini-3-pro",
		Messages: []Message{
			{
				Role: RoleAssistant,
				Parts: []ContentPart{
					{Type: PartToolCall, ToolCall: &ToolCall{ID: "call_1", Name: "get_weather", Arguments: `{}`}},
				},
			},
			{
				Role: RoleTool,
				Parts: []ContentPart{
					{Type: PartToolResult, ToolResult: &ToolResult{ToolCallID: "call_1", Content: "sunny"}},
				},
			},
		},
	}

	loaded := tb.LoadForRequest("chan:1", req)
	loaded.InjectIntoRequest(req)
	if req.Messages[0].Parts[0].ToolCall.ItemID != "fc_1" {
		t.Fatalf("expected injected ItemID fc_1, got %q", req.Messages[0].Parts[0].ToolCall.ItemID)
	}
	if req.Messages[0].Parts[0].ToolCall.ThoughtSignature != "sig_real_1" {
		t.Fatalf("expected injected ThoughtSignature sig_real_1, got %q", req.Messages[0].Parts[0].ToolCall.ThoughtSignature)
	}
	if req.Messages[1].Parts[0].ToolResult.Name != "get_weather" {
		t.Fatalf("expected injected ToolResult Name get_weather, got %q", req.Messages[1].Parts[0].ToolResult.Name)
	}

	// Non-destructive read: second load still succeeds for multi-turn history replay.
	loaded2 := tb.LoadForRequest("chan:1", req)
	if _, ok := loaded2.ToolCalls["call_1"]; !ok {
		t.Fatalf("expected TurnBridge LoadForRequest to be non-destructive")
	}

	// Capacity eviction (maxSize=2)
	s2 := NewBridgeState()
	s2.RecordToolCall(ToolCallBridgeEntry{ToolCallID: "call_2", ResponsesItemID: "fc_2"})
	s3 := NewBridgeState()
	s3.RecordToolCall(ToolCallBridgeEntry{ToolCallID: "call_3", ResponsesItemID: "fc_3"})
	tb.Save("chan:1", s2)
	tb.Save("chan:1", s3)
	if tb.Len() != 2 {
		t.Fatalf("expected LRU eviction to cap size at 2, got %d", tb.Len())
	}

	// TTL expiration
	now = now.Add(11 * time.Minute)
	expired := tb.LoadForRequest("chan:1", &Request{
		Messages: []Message{{Role: RoleTool, Parts: []ContentPart{{Type: PartToolResult, ToolResult: &ToolResult{ToolCallID: "call_3"}}}}},
	})
	if len(expired.ToolCalls) != 0 {
		t.Fatalf("expected expired entry not to be returned, got %+v", expired.ToolCalls)
	}
}

func TestSSEReaderAndFormatter(t *testing.T) {
	raw := ": keep-alive\n\n" +
		"event: message_start\r\n" +
		"data: {\"type\":\"message_start\"}\r\n\r\n" +
		"data: {\"line\":1}\n" +
		"data: {\"line\":2}\n\n" +
		"data: [DONE]\n\n"

	r := NewSSEReader(strings.NewReader(raw))

	ev1, d1, err := r.Next()
	if err != nil || ev1 != "message_start" || string(d1) != `{"type":"message_start"}` {
		t.Fatalf("frame 1 mismatch: ev=%q data=%q err=%v", ev1, string(d1), err)
	}

	ev2, d2, err := r.Next()
	if err != nil || ev2 != "" || string(d2) != "{\"line\":1}\n{\"line\":2}" {
		t.Fatalf("frame 2 mismatch: ev=%q data=%q err=%v", ev2, string(d2), err)
	}

	ev3, d3, err := r.Next()
	if err != nil || ev3 != "" || string(d3) != "[DONE]" {
		t.Fatalf("frame 3 mismatch: ev=%q data=%q err=%v", ev3, string(d3), err)
	}

	formatted := string(FormatSSEFrame(SSEFrame{Event: "content_block_delta", Data: []byte(`{"delta":"hi"}`)}))
	expected := "event: content_block_delta\ndata: {\"delta\":\"hi\"}\n\n"
	if formatted != expected {
		t.Fatalf("formatted SSE mismatch: got %q want %q", formatted, expected)
	}
}

func TestShimTransforms(t *testing.T) {
	ctx := NewConversionContext(FormatOpenAIChat, FormatAnthropic)
	in := []byte(`{"model":"claude-sonnet-4-5","stream":true,"max_tokens":1024}`)
	out, err := ApplyJSONTransforms(in, BedrockClaudeShim.PostIRRequestTransforms, ctx)
	if err != nil {
		t.Fatalf("ApplyJSONTransforms error: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if _, exists := m["model"]; exists {
		t.Fatalf("expected model stripped for Bedrock")
	}
	if _, exists := m["stream"]; exists {
		t.Fatalf("expected stream stripped for Bedrock")
	}
	if m["anthropic_version"] != "bedrock-2023-05-31" {
		t.Fatalf("expected bedrock anthropic_version, got %v", m["anthropic_version"])
	}

	gemIn := []byte(`{"contents":[{"role":"user","parts":[{"functionResponse":{"id":"call_1","name":"fn","response":{"ok":true}}}]}]}`)
	gemOut, err := ApplyJSONTransforms(gemIn, VertexGeminiShim.PostIRRequestTransforms, ctx)
	if err != nil {
		t.Fatalf("VertexGeminiShim error: %v", err)
	}
	if strings.Contains(string(gemOut), `"id"`) {
		t.Fatalf("expected functionResponse.id stripped, got %s", string(gemOut))
	}
}

func TestUsageToDTOUsageSemantics(t *testing.T) {
	anthropicUsage := &Usage{
		InputTokens:           100,
		OutputTokens:          50,
		InputExclusive:        true,
		CachedReadTokens:      40,
		CacheCreationTokens:   20,
		CacheCreation5mTokens: 15,
		CacheCreation1hTokens: 5,
	}
	dtoU := anthropicUsage.ToDTOUsage()
	if dtoU.UsageSemantic != "anthropic" {
		t.Fatalf("expected anthropic usage semantic, got %q", dtoU.UsageSemantic)
	}
	if dtoU.PromptTokens != 100 || dtoU.TotalTokens != 210 {
		t.Fatalf("unexpected anthropic totals: prompt=%d total=%d", dtoU.PromptTokens, dtoU.TotalTokens)
	}
	if dtoU.PromptTokensDetails.GetCacheCreationTokens() != 20 || dtoU.ClaudeCacheCreation5mTokens != 15 || dtoU.ClaudeCacheCreation1hTokens != 5 {
		t.Fatalf("unexpected cache creation details: %+v", dtoU)
	}

	openaiUsage := &Usage{
		InputTokens:      200,
		OutputTokens:     80,
		InputExclusive:   false,
		CachedReadTokens: 60,
		ReasoningTokens:  30,
	}
	dtoO := openaiUsage.ToDTOUsage()
	if dtoO.UsageSemantic != "openai" || dtoO.TotalTokens != 280 || dtoO.CompletionTokenDetails.ReasoningTokens != 30 {
		t.Fatalf("unexpected openai usage: %+v", dtoO)
	}
}
