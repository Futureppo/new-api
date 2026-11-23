package openaichat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func TestZeroArgToolCallStreamsEmptyJSONObject(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	ctx.Model = "claude-sonnet-4-5"

	var frames []ir.SSEFrame
	for _, ev := range []ir.StreamEvent{
		{Type: ir.EventToolCallStart, ToolCallIndex: 0, ToolCallID: "toolu_01", ToolName: "get_time", ToolKind: "function"},
		{Type: ir.EventContentBlockEnd, BlockIndex: 0, BlockType: ir.PartToolCall, ToolCallIndex: 0, ToolCallID: "toolu_01"},
		{Type: ir.EventFinish, FinishReason: ir.FinishToolCalls},
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

	var argsBuf strings.Builder
	for _, f := range frames {
		if len(f.Data) == 0 || string(f.Data) == "[DONE]" {
			continue
		}
		var chunk chatStreamChunkWire
		if err := json.Unmarshal(f.Data, &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			for _, tc := range ch.Delta.ToolCalls {
				if tc.Function != nil {
					argsBuf.WriteString(tc.Function.Arguments)
				}
			}
		}
	}
	if argsBuf.String() != "{}" {
		t.Fatalf("expected zero-arg streamed tool call to produce valid JSON {}, got %q", argsBuf.String())
	}
}

func TestEventErrorTerminatesStreamWithoutFakeFinishOrDone(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	ctx.Model = "claude-sonnet-4-5"

	frames, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:         ir.EventError,
		ErrorType:    "overloaded_error",
		ErrorMessage: "Overloaded",
	}, ctx)
	if err != nil {
		t.Fatalf("EncodeStreamEvent error: %v", err)
	}
	if len(frames) != 1 || !strings.Contains(string(frames[0].Data), `"Overloaded"`) {
		t.Fatalf("expected 1 error frame, got %d", len(frames))
	}
	if !ctx.Ended || !ctx.FinishEmitted {
		t.Fatalf("expected EventError to terminate stream context, ended=%v finish=%v", ctx.Ended, ctx.FinishEmitted)
	}

	tail, err := c.FlushStream(ctx)
	if err != nil {
		t.Fatalf("FlushStream error: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("expected no trailing finish or [DONE] frames after EventError, got %d frames", len(tail))
	}

	// Never-started stream must also emit nothing on FlushStream.
	emptyCtx := ir.NewStreamContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	emptyTail, err := c.FlushStream(emptyCtx)
	if err != nil || len(emptyTail) != 0 {
		t.Fatalf("expected empty flush on never-started stream, got %d frames (err=%v)", len(emptyTail), err)
	}
}

func TestCacheControlStrippedByDefaultAndPreservedWhenEnabled(t *testing.T) {
	c := New()
	req := &ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{
			{
				Role: ir.RoleUser,
				Parts: []ir.ContentPart{{
					Type:         ir.PartText,
					Text:         "cached prompt",
					CacheControl: &ir.CacheControl{Type: "ephemeral"},
				}},
			},
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{{
					Type:         ir.PartToolResult,
					ToolResult:   &ir.ToolResult{ToolCallID: "call_1", Content: "ok"},
					CacheControl: &ir.CacheControl{Type: "ephemeral"},
				}},
			},
		},
	}

	// Cross-protocol default (Anthropic -> OpenAI Chat): cache_control MUST be stripped.
	ctxDefault := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	rawDefault, err := c.EncodeRequest(req, ctxDefault)
	if err != nil {
		t.Fatalf("EncodeRequest default error: %v", err)
	}
	if strings.Contains(string(rawDefault), "cache_control") {
		t.Fatalf("expected cache_control stripped for non-OpenRouter OpenAI Chat, got: %s", string(rawDefault))
	}

	// OpenRouter / preserve_cache_control=true: cache_control preserved on user AND tool messages.
	ctxPreserve := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat)
	ctxPreserve.Options["preserve_cache_control"] = true
	rawPreserve, err := c.EncodeRequest(req, ctxPreserve)
	if err != nil {
		t.Fatalf("EncodeRequest preserve error: %v", err)
	}
	if strings.Count(string(rawPreserve), `"cache_control"`) < 2 {
		t.Fatalf("expected cache_control preserved on user and tool messages when enabled, got: %s", string(rawPreserve))
	}
}

func TestReasoningDisabledBeatsResidualEffortAndMaxClampsToHigh(t *testing.T) {
	c := New()
	enabled := false
	reqDisabled := &ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{{
			Role:  ir.RoleUser,
			Parts: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}},
		}},
		Reasoning: &ir.ReasoningConfig{
			Enabled: &enabled,
			Mode:    "disabled",
			Effort:  "medium",
		},
	}
	rawDisabled, err := c.EncodeRequest(reqDisabled, ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("EncodeRequest disabled error: %v", err)
	}
	var mDisabled map[string]any
	if err := json.Unmarshal(rawDisabled, &mDisabled); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mDisabled["reasoning_effort"] != "none" {
		t.Fatalf("expected disabled reasoning with residual effort to encode as \"none\", got %v", mDisabled["reasoning_effort"])
	}

	reqMax := &ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{{
			Role:  ir.RoleUser,
			Parts: []ir.ContentPart{{Type: ir.PartText, Text: "hi"}},
		}},
		Reasoning: &ir.ReasoningConfig{Effort: "max"},
	}
	rawMax, err := c.EncodeRequest(reqMax, ir.NewConversionContext(ir.FormatAnthropic, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("EncodeRequest max error: %v", err)
	}
	var mMax map[string]any
	if err := json.Unmarshal(rawMax, &mMax); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if mMax["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort \"max\" clamped to \"high\", got %v", mMax["reasoning_effort"])
	}
}

func TestCrossProtocolCustomToolsAndLegacyFunctionsFIFO(t *testing.T) {
	c := New()

	// Cross-protocol custom tool & custom tool_choice must be preserved as type:"custom".
	req := &ir.Request{
		Model: "gpt-5",
		Messages: []ir.Message{{
			Role:  ir.RoleUser,
			Parts: []ir.ContentPart{{Type: ir.PartText, Text: "run sql"}},
		}},
		Tools: []ir.ToolDefinition{{
			Type:        "custom",
			Name:        "sql_runner",
			Description: "Runs SQL",
			ProviderMetadata: map[string]any{
				"format": map[string]any{"type": "text"},
			},
		}},
		ToolChoice: &ir.ToolChoice{
			Mode:     "tool",
			ToolName: "sql_runner",
			ToolType: "custom",
		},
	}
	raw, err := c.EncodeRequest(req, ir.NewConversionContext(ir.FormatOpenAIResponses, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("EncodeRequest custom tool error: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tools := wire["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["type"] != "custom" {
		t.Fatalf("expected custom tool preserved, got: %v", tools)
	}
	tc := wire["tool_choice"].(map[string]any)
	if tc["type"] != "custom" {
		t.Fatalf("expected custom tool_choice type=\"custom\", got: %v", tc)
	}

	// Legacy functions + function_call + role:"function" must synthesize correlated non-empty IDs.
	legacyJSON := `{
		"model": "gpt-4",
		"functions": [{"name": "get_weather", "description": "Weather", "parameters": {"type": "object"}}],
		"function_call": {"name": "get_weather"},
		"messages": [
			{"role": "user", "content": "Weather?"},
			{"role": "assistant", "content": null, "function_call": {"name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}},
			{"role": "function", "name": "get_weather", "content": "Sunny"}
		]
	}`
	decoded, err := c.DecodeRequest([]byte(legacyJSON), ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("DecodeRequest legacy error: %v", err)
	}
	if len(decoded.Tools) != 1 || decoded.Tools[0].Name != "get_weather" {
		t.Fatalf("expected legacy functions decoded into Tools, got: %+v", decoded.Tools)
	}
	if decoded.ToolChoice == nil || decoded.ToolChoice.Mode != "tool" || decoded.ToolChoice.ToolName != "get_weather" {
		t.Fatalf("expected legacy function_call decoded into ToolChoice, got: %+v", decoded.ToolChoice)
	}
	callID := decoded.Messages[1].Parts[0].ToolCall.ID
	resultID := decoded.Messages[2].Parts[0].ToolResult.ToolCallID
	if callID == "" || callID != resultID {
		t.Fatalf("expected correlated non-empty IDs between legacy function_call and role:function, got call=%q result=%q", callID, resultID)
	}
}

func TestMultiChoiceFinishAndMaxCompletionTokensRoundTrip(t *testing.T) {
	c := New()

	// Multi-choice streaming must emit finish_reason for BOTH choice 0 and choice 1.
	ctx := ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatOpenAIChat)
	ctx.Model = "gemini-2.5-pro"
	var frames []ir.SSEFrame
	for _, ev := range []ir.StreamEvent{
		{Type: ir.EventTextDelta, ChoiceIndex: 0, Text: "A"},
		{Type: ir.EventTextDelta, ChoiceIndex: 1, Text: "B"},
		{Type: ir.EventFinish, ChoiceIndex: 0, FinishReason: ir.FinishStop},
		{Type: ir.EventFinish, ChoiceIndex: 1, FinishReason: ir.FinishLength},
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

	seenFinish := map[int]string{}
	for _, f := range frames {
		if len(f.Data) == 0 || string(f.Data) == "[DONE]" {
			continue
		}
		var chunk chatStreamChunkWire
		if json.Unmarshal(f.Data, &chunk) != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				seenFinish[ch.Index] = *ch.FinishReason
			}
		}
	}
	if seenFinish[0] != "stop" || seenFinish[1] != "length" {
		t.Fatalf("expected both choice finishes (0=stop, 1=length), got %+v", seenFinish)
	}

	// max_completion_tokens round-trip preserved even without explicit provider option.
	rawIn := []byte(`{"model":"gpt-5","max_completion_tokens":321,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":""}},{"type":"text","text":"hi"}]}]}`)
	dec, err := c.DecodeRequest(rawIn, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if len(dec.Messages[0].Parts) != 1 || dec.Messages[0].Parts[0].Type != ir.PartText {
		t.Fatalf("expected empty image_url to be skipped, got parts: %+v", dec.Messages[0].Parts)
	}
	reEnc, err := c.EncodeRequest(dec, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	if !strings.Contains(string(reEnc), `"max_completion_tokens":321`) {
		t.Fatalf("expected max_completion_tokens preserved on round-trip, got: %s", string(reEnc))
	}

	// Non-streaming PartImage fallback in EncodeResponse.
	respOut, err := c.EncodeResponse(&ir.Response{
		ID:    "resp_img",
		Model: "gemini-2.5-flash-image",
		Choices: []ir.Choice{{
			Index: 0,
			Message: ir.Message{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{{
					Type:  ir.PartImage,
					Image: &ir.ImageSource{MediaType: "image/png", Data: "AQID"},
				}},
			},
			FinishReason: ir.FinishStop,
		}},
	}, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("EncodeResponse image error: %v", err)
	}
	if !strings.Contains(string(respOut), "data:image/png;base64,AQID") {
		t.Fatalf("expected PartImage serialized as markdown data URI, got: %s", string(respOut))
	}
}
