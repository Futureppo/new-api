package ir

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTurnBridgeTenantIsolationAndNoUnscopedFallback(t *testing.T) {
	tb := NewTurnBridge(time.Minute, 1024)

	// Tenant A saves tool call "call_shared" with its own thoughtSignature and ResponsesItemID.
	stateA := NewBridgeState()
	stateA.RecordToolCall(ToolCallBridgeEntry{
		ToolCallID:       "call_shared",
		ResponsesItemID:  "fc_tenant_a",
		ToolName:         "secret_tool_a",
		ThoughtSignature: "sig_tenant_a",
		OriginFormat:     FormatGoogleGenAI,
	})
	tb.Save("u:1:t:10:c:99", stateA)

	// Tenant B on the SAME channel with the SAME tool call ID must NOT receive Tenant A's metadata.
	reqB := &Request{
		Messages: []Message{{
			Role: RoleAssistant,
			Parts: []ContentPart{{
				Type: PartToolCall,
				ToolCall: &ToolCall{
					ID:   "call_shared",
					Name: "tool_b",
				},
			}},
		}},
	}
	loadedB := tb.LoadForRequest("u:2:t:20:c:99", reqB)
	if entry, ok := loadedB.ToolCalls["call_shared"]; ok {
		t.Fatalf("cross-tenant leak detected! Tenant B received Tenant A's entry: %+v", entry)
	}

	// Unscoped lookup must also NOT leak Tenant A's entry.
	loadedEmpty := tb.LoadForRequest("", reqB)
	if entry, ok := loadedEmpty.ToolCalls["call_shared"]; ok {
		t.Fatalf("unscoped lookup leaked Tenant A's entry: %+v", entry)
	}

	// Tenant A's own scope gets exact entry back.
	loadedA := tb.LoadForRequest("u:1:t:10:c:99", reqB)
	gotA, ok := loadedA.ToolCalls["call_shared"]
	if !ok || gotA.ThoughtSignature != "sig_tenant_a" || gotA.ResponsesItemID != "fc_tenant_a" {
		t.Fatalf("Tenant A failed to load its own bridge entry: %+v (ok=%v)", gotA, ok)
	}

	// Overwriting same scope with a conflicting entry must replace stale fields rather than keeping stale ones.
	stateA2 := NewBridgeState()
	stateA2.RecordToolCall(ToolCallBridgeEntry{
		ToolCallID:      "call_shared",
		ResponsesItemID: "fc_tenant_a_new",
		ToolName:        "new_tool_a",
		OriginFormat:    FormatOpenAIResponses,
	})
	tb.Save("u:1:t:10:c:99", stateA2)

	loadedA2 := tb.LoadForRequest("u:1:t:10:c:99", reqB)
	gotA2 := loadedA2.ToolCalls["call_shared"]
	if gotA2.ResponsesItemID != "fc_tenant_a_new" || gotA2.ToolName != "new_tool_a" || gotA2.ThoughtSignature != "" {
		t.Fatalf("expected conflicting overwrite to clear stale signature and update fields, got: %+v", gotA2)
	}

	// Concurrent sharded stress test.
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			scope := fmt.Sprintf("u:%d:t:%d:c:1", worker, worker)
			for i := 0; i < 100; i++ {
				id := fmt.Sprintf("call_%d_%d", worker, i)
				st := NewBridgeState()
				st.RecordToolCall(ToolCallBridgeEntry{
					ToolCallID:      id,
					ResponsesItemID: "fc_" + id,
					ToolName:        "fn",
					OriginFormat:    FormatOpenAIResponses,
				})
				tb.Save(scope, st)
				req := &Request{
					Messages: []Message{{
						Role:  RoleAssistant,
						Parts: []ContentPart{{Type: PartToolCall, ToolCall: &ToolCall{ID: id}}},
					}},
				}
				_ = tb.LoadForRequest(scope, req)
			}
		}(g)
	}
	wg.Wait()
}

func TestSSEReaderMultiLineMaxBufCap(t *testing.T) {
	// 128-byte cap with 10 small data lines totaling > 128 bytes must return buffer size error.
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("data: " + strings.Repeat("x", 30) + "\n")
	}
	b.WriteString("\n")

	reader := NewSSEReaderWithMaxBuf(strings.NewReader(b.String()), 128)
	_, _, err := reader.Next()
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum buffer size") {
		t.Fatalf("expected buffer size cap error for oversized multi-line SSE event, got: %v", err)
	}
}

func TestApplyJSONTransformsPreservesInt64AndEmptyDefaultAndVertexGeminiShim(t *testing.T) {
	// 64-bit integer beyond float64 2^53 precision must survive untouched.
	raw := []byte(`{"model":"","seed":9007199254740993,"contents":[{"role":"model","parts":[{"functionCall":{"id":"fc_1","name":"fn","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"fc_1","name":"fn","response":{"ok":true}}}]}]}`)

	out, err := ApplyJSONTransforms(raw, []JSONTransform{
		SetDefaultField("model", "gemini-2.5-pro"),
		StripGeminiFunctionResponseID(),
	}, nil)
	if err != nil {
		t.Fatalf("ApplyJSONTransforms error: %v", err)
	}

	s := string(out)
	if !strings.Contains(s, `"seed":9007199254740993`) {
		t.Fatalf("64-bit integer precision lost in ApplyJSONTransforms: %s", s)
	}
	if !strings.Contains(s, `"model":"gemini-2.5-pro"`) {
		t.Fatalf("SetDefaultField failed to fill empty string model: %s", s)
	}

	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	contents := parsed["contents"].([]any)
	fc := contents[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	fr := contents[1].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if _, hasID := fc["id"]; hasID {
		t.Fatalf("expected functionCall.id to be stripped, got: %v", fc)
	}
	if _, hasID := fr["id"]; hasID {
		t.Fatalf("expected functionResponse.id to be stripped, got: %v", fr)
	}
}

func TestStreamContextToolCallIsolationAndMonotonicUsage(t *testing.T) {
	ctx := NewStreamContext(FormatAnthropic, FormatOpenAIResponses)

	tc1, _ := ctx.RegisterToolCall(0, "call_1", "fc_1", "fn_a", "function", "")
	if tc1.Index != 0 {
		t.Fatalf("expected index 0, got %d", tc1.Index)
	}
	// Parallel tool call with colliding upstream index 0 but different ID must get a new slot.
	tc2, _ := ctx.RegisterToolCall(0, "call_2", "fc_2", "fn_b", "function", "")
	if tc2.Index == tc1.Index || tc1.ID != "call_1" || tc2.ID != "call_2" {
		t.Fatalf("parallel tool collision overwrote tc1: tc1=%+v tc2=%+v", tc1, tc2)
	}
	// Verify OriginFormat uses SourceFormat (not TargetFormat).
	if entry, ok := ctx.BridgeState.ToolCalls["call_1"]; !ok || entry.OriginFormat != FormatAnthropic {
		t.Fatalf("expected OriginFormat=%s, got %+v", FormatAnthropic, entry)
	}

	// Lookup with an unknown non-empty ID must return nil (never cross-contaminate).
	if got := ctx.LookupToolCall(0, "call_unknown"); got != nil {
		t.Fatalf("expected nil for unknown non-empty ToolCallID, got %+v", got)
	}

	// Wildcard lookup skips closed tool calls.
	tc2.Closed = true
	if got := ctx.LookupToolCall(-1, ""); got != tc1 {
		t.Fatalf("expected wildcard lookup to skip closed tc2 and return open tc1, got %+v", got)
	}
	tc1.Closed = true
	if got := ctx.LookupToolCall(-1, ""); got != nil {
		t.Fatalf("expected wildcard lookup to return nil when all tools are closed, got %+v", got)
	}

	// MergeUsage monotonicity & TotalTokens / ReasoningTokens invariant.
	ctx.MergeUsage(&Usage{InputTokens: 100, OutputTokens: 20, ReasoningTokens: 15, TotalTokens: 120})
	ctx.MergeUsage(&Usage{InputTokens: 50, OutputTokens: 35, ReasoningTokens: 40, TotalTokens: 60})
	if ctx.LastUsage.InputTokens != 100 || ctx.LastUsage.OutputTokens != 40 {
		t.Fatalf("expected monotonic input=100 output=40 (clamped >= reasoning=40), got %+v", ctx.LastUsage)
	}
	if ctx.LastUsage.ReasoningTokens > ctx.LastUsage.OutputTokens {
		t.Fatalf("expected ReasoningTokens <= OutputTokens, got %+v", ctx.LastUsage)
	}
	if ctx.LastUsage.TotalTokens < ctx.LastUsage.InclusiveInputTokens()+ctx.LastUsage.OutputTokens {
		t.Fatalf("expected TotalTokens >= input+output, got %+v", ctx.LastUsage)
	}
}

func TestSynthesizeToolCallIDUniquenessAndLength(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := SynthesizeToolCallID("call")
		if len(id) == 0 || len(id) > 24 {
			t.Fatalf("unexpected synthesized tool call id length %d (%q)", len(id), id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate synthesized tool call id: %q", id)
		}
		seen[id] = struct{}{}
	}
}
