package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relay/ir"
)

const (
	metaKeyDecodePTBlocks = "_anthropic_dec_pt_blocks"
	metaKeyEncodePTActive = "_anthropic_enc_pt_active"
	metaKeyEncodePTIndex  = "_anthropic_enc_pt_index"
	metaKeyActiveRedacted = "_anthropic_active_is_redacted"
	metaKeyDeferredTools  = "_anthropic_deferred_tools"
)

type wireStreamMessageStart struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Role         string     `json:"role"`
	Model        string     `json:"model"`
	Content      []any      `json:"content"`
	StopReason   *string    `json:"stop_reason"`
	StopSequence *string    `json:"stop_sequence"`
	Usage        *wireUsage `json:"usage,omitempty"`
}

type wireStreamDelta struct {
	Type         string  `json:"type,omitempty"`
	Text         string  `json:"text,omitempty"`
	Thinking     string  `json:"thinking,omitempty"`
	Signature    string  `json:"signature,omitempty"`
	PartialJSON  string  `json:"partial_json,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

type wireStreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type wireStreamEnvelope struct {
	Type         string                  `json:"type"`
	Index        int                     `json:"index"`
	Message      *wireStreamMessageStart `json:"message,omitempty"`
	ContentBlock *wireContentBlock       `json:"content_block,omitempty"`
	Delta        *wireStreamDelta        `json:"delta,omitempty"`
	Usage        *wireUsage              `json:"usage,omitempty"`
	Error        *wireStreamError        `json:"error,omitempty"`
}

// Wire encoding structs for Anthropic SSE events.

type wireMessageStartEventEnc struct {
	Type    string                 `json:"type"`
	Message wireStreamMessageStart `json:"message"`
}

type wireContentBlockStartEventEnc struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock any    `json:"content_block"`
}

type wireContentBlockDeltaEventEnc struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta any    `json:"delta"`
}

type wireContentBlockStopEventEnc struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type wireTextDeltaEnc struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireThinkingDeltaEnc struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type wireSignatureDeltaEnc struct {
	Type      string `json:"type"`
	Signature string `json:"signature"`
}

type wireInputJSONDeltaEnc struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type wireMessageDeltaBodyEnc struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type wireMessageDeltaEventEnc struct {
	Type  string                  `json:"type"`
	Delta wireMessageDeltaBodyEnc `json:"delta"`
	Usage wireUsage               `json:"usage"`
}

type wireMessageStopEventEnc struct {
	Type string `json:"type"`
}

type wireErrorEventEnc struct {
	Type  string          `json:"type"`
	Error wireStreamError `json:"error"`
}

// DecodeStreamEvent parses one Anthropic SSE event into zero or more normalized IR StreamEvents.
func (*Codec) DecodeStreamEvent(sseEvent string, rawData []byte, ctx *ir.StreamContext) ([]ir.StreamEvent, error) {
	trimmed := bytes.TrimSpace(rawData)
	if len(trimmed) == 0 {
		return nil, nil
	}

	var env wireStreamEnvelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal stream event %q: %w", sseEvent, err)
	}

	evType := env.Type
	if evType == "" {
		evType = sseEvent
	}

	switch evType {
	case "ping":
		return nil, nil

	case "message_start":
		var id, model string
		var initialUsage *ir.Usage
		if env.Message != nil {
			id = env.Message.ID
			model = env.Message.Model
			if env.Message.Usage != nil {
				initialUsage = decodeUsage(env.Message.Usage, false)
			}
		}
		if ctx != nil {
			if id != "" {
				ctx.ResponseID = id
			}
			if model != "" {
				ctx.Model = model
			}
			ctx.Started = true
		}
		events := []ir.StreamEvent{{
			Type:       ir.EventStreamStart,
			ResponseID: id,
			Model:      model,
			Usage:      initialUsage,
		}}
		return events, nil

	case "content_block_start":
		if env.ContentBlock == nil {
			return nil, nil
		}
		cb := env.ContentBlock
		switch cb.Type {
		case "text":
			if ctx != nil {
				ctx.ActiveBlockIndex = env.Index
				ctx.ActiveBlockType = ir.PartText
				ctx.BlockOpen = true
			}
			events := []ir.StreamEvent{{
				Type:       ir.EventContentBlockStart,
				BlockIndex: env.Index,
				BlockType:  ir.PartText,
			}}
			if cb.Text != "" {
				events = append(events, ir.StreamEvent{
					Type:       ir.EventTextDelta,
					BlockIndex: env.Index,
					BlockType:  ir.PartText,
					Text:       cb.Text,
				})
			}
			return events, nil

		case "thinking":
			if ctx != nil {
				ctx.ActiveBlockIndex = env.Index
				ctx.ActiveBlockType = ir.PartReasoning
				ctx.BlockOpen = true
			}
			events := []ir.StreamEvent{{
				Type:         ir.EventContentBlockStart,
				BlockIndex:   env.Index,
				BlockType:    ir.PartReasoning,
				OriginFormat: ir.FormatAnthropic,
			}}
			if cb.Thinking != "" {
				events = append(events, ir.StreamEvent{
					Type:         ir.EventReasoningDelta,
					BlockIndex:   env.Index,
					BlockType:    ir.PartReasoning,
					Reasoning:    cb.Thinking,
					OriginFormat: ir.FormatAnthropic,
				})
			}
			if cb.Signature != "" {
				events = append(events, ir.StreamEvent{
					Type:         ir.EventReasoningSignatureDelta,
					BlockIndex:   env.Index,
					BlockType:    ir.PartReasoning,
					Signature:    cb.Signature,
					OriginFormat: ir.FormatAnthropic,
				})
			}
			return events, nil

		case "redacted_thinking":
			if ctx != nil {
				ctx.ActiveBlockIndex = env.Index
				ctx.ActiveBlockType = ir.PartReasoning
				ctx.BlockOpen = true
			}
			return []ir.StreamEvent{{
				Type:         ir.EventContentBlockStart,
				BlockIndex:   env.Index,
				BlockType:    ir.PartReasoning,
				RedactedData: cb.Data,
				OriginFormat: ir.FormatAnthropic,
			}}, nil

		case "tool_use":
			toolIdx := 0
			if ctx != nil {
				ctx.ActiveBlockIndex = env.Index
				ctx.ActiveBlockType = ir.PartToolCall
				ctx.BlockOpen = true
				tc, _ := ctx.RegisterToolCall(-1, cb.ID, "", cb.Name, "function", "")
				tc.BlockIndex = env.Index
				tc.Started = true
				toolIdx = tc.Index
			}
			events := []ir.StreamEvent{{
				Type:          ir.EventToolCallStart,
				BlockIndex:    env.Index,
				BlockType:     ir.PartToolCall,
				ToolCallID:    cb.ID,
				ToolName:      cb.Name,
				ToolCallIndex: toolIdx,
				ToolKind:      "function",
			}}
			// Emit initial arguments only when content_block_start carries a non-empty JSON object
			// (standard Anthropic streams send {} here and deliver arguments via input_json_delta).
			if args := nonEmptyInitialToolInput(cb.Input); args != "" {
				events = append(events, ir.StreamEvent{
					Type:          ir.EventToolCallDelta,
					BlockIndex:    env.Index,
					BlockType:     ir.PartToolCall,
					ToolCallID:    cb.ID,
					ToolName:      cb.Name,
					ToolCallIndex: toolIdx,
					ToolArguments: args,
				})
			}
			return events, nil

		default:
			markDecodePassthroughBlock(ctx, env.Index, true)
			return []ir.StreamEvent{{
				Type:             ir.EventPassthrough,
				BlockIndex:       env.Index,
				OriginFormat:     ir.FormatAnthropic,
				PassthroughEvent: evType,
				PassthroughData:  bytes.Clone(trimmed),
			}}, nil
		}

	case "content_block_delta":
		if env.Delta == nil {
			return nil, nil
		}
		if isDecodePassthroughBlock(ctx, env.Index) {
			return []ir.StreamEvent{{
				Type:             ir.EventPassthrough,
				BlockIndex:       env.Index,
				OriginFormat:     ir.FormatAnthropic,
				PassthroughEvent: evType,
				PassthroughData:  bytes.Clone(trimmed),
			}}, nil
		}
		d := env.Delta
		switch d.Type {
		case "text_delta":
			return []ir.StreamEvent{{
				Type:       ir.EventTextDelta,
				BlockIndex: env.Index,
				BlockType:  ir.PartText,
				Text:       d.Text,
			}}, nil

		case "thinking_delta":
			return []ir.StreamEvent{{
				Type:         ir.EventReasoningDelta,
				BlockIndex:   env.Index,
				BlockType:    ir.PartReasoning,
				Reasoning:    d.Thinking,
				OriginFormat: ir.FormatAnthropic,
			}}, nil

		case "signature_delta":
			return []ir.StreamEvent{{
				Type:         ir.EventReasoningSignatureDelta,
				BlockIndex:   env.Index,
				BlockType:    ir.PartReasoning,
				Signature:    d.Signature,
				OriginFormat: ir.FormatAnthropic,
			}}, nil

		case "input_json_delta":
			tc := findToolCallByBlockIndex(ctx, env.Index)
			if tc == nil {
				return nil, nil
			}
			return []ir.StreamEvent{{
				Type:          ir.EventToolCallDelta,
				BlockIndex:    env.Index,
				BlockType:     ir.PartToolCall,
				ToolCallID:    tc.ID,
				ToolName:      tc.Name,
				ToolCallIndex: tc.Index,
				ToolArguments: d.PartialJSON,
			}}, nil

		case "citations_delta":
			return []ir.StreamEvent{{
				Type:             ir.EventPassthrough,
				BlockIndex:       env.Index,
				OriginFormat:     ir.FormatAnthropic,
				PassthroughEvent: evType,
				PassthroughData:  bytes.Clone(trimmed),
			}}, nil

		default:
			return nil, nil
		}

	case "content_block_stop":
		if isDecodePassthroughBlock(ctx, env.Index) {
			markDecodePassthroughBlock(ctx, env.Index, false)
			return []ir.StreamEvent{{
				Type:             ir.EventPassthrough,
				BlockIndex:       env.Index,
				OriginFormat:     ir.FormatAnthropic,
				PassthroughEvent: evType,
				PassthroughData:  bytes.Clone(trimmed),
			}}, nil
		}
		var blockType ir.PartType
		if ctx != nil && ctx.BlockOpen && ctx.ActiveBlockIndex == env.Index {
			blockType = ctx.ActiveBlockType
			ctx.BlockOpen = false
			ctx.ActiveBlockIndex = -1
			ctx.ActiveBlockType = ""
		}
		ev := ir.StreamEvent{
			Type:       ir.EventContentBlockEnd,
			BlockIndex: env.Index,
			BlockType:  blockType,
		}
		if tc := findToolCallByBlockIndex(ctx, env.Index); tc != nil {
			ev.BlockType = ir.PartToolCall
			ev.ToolCallID = tc.ID
			ev.ToolCallIndex = tc.Index
		}
		return []ir.StreamEvent{ev}, nil

	case "message_delta":
		var events []ir.StreamEvent
		// Emit EventUsage before EventFinish so downstream usage accounting merges
		// final output_tokens prior to handling termination.
		if env.Usage != nil {
			events = append(events, ir.StreamEvent{
				Type:  ir.EventUsage,
				Usage: decodeUsage(env.Usage, false),
			})
		}
		if env.Delta != nil && env.Delta.StopReason != nil && *env.Delta.StopReason != "" {
			rawStop := *env.Delta.StopReason
			events = append(events, ir.StreamEvent{
				Type:            ir.EventFinish,
				FinishReason:    decodeFinishReason(rawStop),
				RawFinishReason: rawStop,
			})
		}
		return events, nil

	case "message_stop":
		return []ir.StreamEvent{{Type: ir.EventStreamEnd}}, nil

	case "error":
		errType := "api_error"
		errMsg := "upstream stream error"
		if env.Error != nil {
			if env.Error.Type != "" {
				errType = env.Error.Type
			}
			if env.Error.Message != "" {
				errMsg = env.Error.Message
			}
		}
		return []ir.StreamEvent{{
			Type:         ir.EventError,
			ErrorType:    errType,
			ErrorMessage: errMsg,
		}}, nil

	default:
		return nil, nil
	}
}

func nonEmptyInitialToolInput(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("{}")) {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil || len(obj) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return ""
	}
	return buf.String()
}

func findToolCallByBlockIndex(ctx *ir.StreamContext, blockIdx int) *ir.TrackedToolCall {
	if ctx == nil || blockIdx < 0 {
		return nil
	}
	for _, tc := range ctx.ToolCallOrder {
		if tc.BlockIndex == blockIdx {
			return tc
		}
	}
	return nil
}

func markDecodePassthroughBlock(ctx *ir.StreamContext, idx int, open bool) {
	if ctx == nil {
		return
	}
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	set, _ := ctx.Metadata[metaKeyDecodePTBlocks].(map[int]bool)
	if set == nil {
		if !open {
			return
		}
		set = make(map[int]bool)
		ctx.Metadata[metaKeyDecodePTBlocks] = set
	}
	if open {
		set[idx] = true
	} else {
		delete(set, idx)
	}
}

func isDecodePassthroughBlock(ctx *ir.StreamContext, idx int) bool {
	if ctx == nil || ctx.Metadata == nil {
		return false
	}
	set, _ := ctx.Metadata[metaKeyDecodePTBlocks].(map[int]bool)
	return set[idx]
}

func isActiveRedactedBlock(ctx *ir.StreamContext) bool {
	if ctx == nil || ctx.Metadata == nil {
		return false
	}
	b, _ := ctx.Metadata[metaKeyActiveRedacted].(bool)
	return b
}

func setActiveRedactedBlock(ctx *ir.StreamContext, v bool) {
	if ctx == nil {
		return
	}
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	ctx.Metadata[metaKeyActiveRedacted] = v
}

func getDeferredTools(ctx *ir.StreamContext) []*ir.TrackedToolCall {
	if ctx == nil || ctx.Metadata == nil {
		return nil
	}
	list, _ := ctx.Metadata[metaKeyDeferredTools].([]*ir.TrackedToolCall)
	return list
}

func appendDeferredTool(ctx *ir.StreamContext, tc *ir.TrackedToolCall) {
	if ctx == nil || tc == nil {
		return
	}
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	list := getDeferredTools(ctx)
	for _, existing := range list {
		if existing == tc {
			return
		}
	}
	ctx.Metadata[metaKeyDeferredTools] = append(list, tc)
}

func clearDeferredTools(ctx *ir.StreamContext) []*ir.TrackedToolCall {
	list := getDeferredTools(ctx)
	if ctx != nil && ctx.Metadata != nil {
		delete(ctx.Metadata, metaKeyDeferredTools)
	}
	return list
}

// EncodeStreamEvent translates a single IR StreamEvent into Anthropic SSE frames while
// maintaining strict Anthropic content block index open/delta/stop invariants.
func (*Codec) EncodeStreamEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatUnknown, ir.FormatAnthropic)
	}
	if ctx.Ended {
		return nil, nil
	}

	// Capture session envelope metadata and usage updates regardless of event kind.
	if ev.ResponseID != "" && ctx.ResponseID == "" {
		ctx.ResponseID = ev.ResponseID
	}
	if ev.Model != "" && ctx.Model == "" {
		ctx.Model = ev.Model
	}
	if ev.Created > 0 && ctx.Created == 0 {
		ctx.Created = ev.Created
	}
	if ev.Usage != nil {
		ctx.MergeUsage(ev.Usage)
		if ctx.LastUsage != nil && ev.Usage.TotalTokens == 0 && (ctx.LastUsage.InputTokens > 0 || ctx.LastUsage.OutputTokens > 0) {
			ctx.LastUsage.TotalTokens = ctx.LastUsage.InclusiveInputTokens() + ctx.LastUsage.OutputTokens
		}
	}

	switch ev.Type {
	case ir.EventStreamStart:
		return ensureMessageStartFrames(ctx)

	case ir.EventUsage:
		// Usage is already merged into ctx.LastUsage and will be emitted in message_delta.
		return nil, nil

	case ir.EventContentBlockStart:
		return encodeContentBlockStartEvent(ev, ctx)

	case ir.EventReasoningDelta:
		if ev.Reasoning == "" {
			return nil, nil
		}
		var frames []ir.SSEFrame
		startFrames, err := ensureMessageStartFrames(ctx)
		if err != nil {
			return nil, err
		}
		frames = append(frames, startFrames...)

		if !ctx.BlockOpen || ctx.ActiveBlockType != ir.PartReasoning || isActiveRedactedBlock(ctx) {
			stopFrames, err := closeActiveAndDeferredFrames(ctx)
			if err != nil {
				return nil, err
			}
			frames = append(frames, stopFrames...)

			idx := ctx.OpenBlock(ir.PartReasoning)
			setActiveRedactedBlock(ctx, false)
			f, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
				Type:         "content_block_start",
				Index:        idx,
				ContentBlock: wireThinkingBlockEnc{Type: "thinking", Thinking: ""},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, f)
		}

		f, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
			Type:  "content_block_delta",
			Index: ctx.ActiveBlockIndex,
			Delta: wireThinkingDeltaEnc{Type: "thinking_delta", Thinking: ev.Reasoning},
		})
		if err != nil {
			return nil, err
		}
		frames = append(frames, f)
		return frames, nil

	case ir.EventReasoningSignatureDelta:
		if ev.Signature == "" || !ctx.BlockOpen || ctx.ActiveBlockType != ir.PartReasoning || isActiveRedactedBlock(ctx) {
			return nil, nil
		}
		if ev.OriginFormat != ir.FormatUnknown && ev.OriginFormat != ir.FormatAnthropic {
			return nil, nil
		}
		f, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
			Type:  "content_block_delta",
			Index: ctx.ActiveBlockIndex,
			Delta: wireSignatureDeltaEnc{Type: "signature_delta", Signature: ev.Signature},
		})
		if err != nil {
			return nil, err
		}
		return []ir.SSEFrame{f}, nil

	case ir.EventTextDelta, ir.EventRefusalDelta, ir.EventImage:
		text := ev.Text
		if ev.Type == ir.EventRefusalDelta {
			text = ev.Refusal
		} else if ev.Type == ir.EventImage {
			text = formatAnthropicImageMarkdown(ev.Image)
		}
		if text == "" {
			return nil, nil
		}
		var frames []ir.SSEFrame
		startFrames, err := ensureMessageStartFrames(ctx)
		if err != nil {
			return nil, err
		}
		frames = append(frames, startFrames...)

		if !ctx.BlockOpen || ctx.ActiveBlockType != ir.PartText {
			stopFrames, err := closeActiveAndDeferredFrames(ctx)
			if err != nil {
				return nil, err
			}
			frames = append(frames, stopFrames...)

			idx := ctx.OpenBlock(ir.PartText)
			f, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
				Type:         "content_block_start",
				Index:        idx,
				ContentBlock: wireTextBlockEnc{Type: "text", Text: ""},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, f)
		}

		f, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
			Type:  "content_block_delta",
			Index: ctx.ActiveBlockIndex,
			Delta: wireTextDeltaEnc{Type: "text_delta", Text: text},
		})
		if err != nil {
			return nil, err
		}
		frames = append(frames, f)
		return frames, nil

	case ir.EventToolCallStart:
		return encodeToolCallStartEvent(ev, ctx)

	case ir.EventToolCallDelta:
		if ev.ToolArguments == "" {
			return nil, nil
		}
		var frames []ir.SSEFrame
		startFrames, err := ensureMessageStartFrames(ctx)
		if err != nil {
			return nil, err
		}
		frames = append(frames, startFrames...)

		tc := ctx.LookupToolCall(ev.ToolCallIndex, ev.ToolCallID)
		if tc == nil {
			tc, _ = ctx.RegisterToolCall(ev.ToolCallIndex, ev.ToolCallID, ev.ToolCallItemID, ev.ToolName, ev.ToolKind, ev.ThoughtSignature)
		}
		if tc.Closed {
			return frames, nil
		}
		if !tc.Started {
			// If another tool_use block is still open, buffer this interleaved tool's args until
			// the active tool closes so we never emit input_json_delta to a closed Anthropic block.
			if ctx.BlockOpen && ctx.ActiveBlockType == ir.PartToolCall {
				tc.Args.WriteString(ev.ToolArguments)
				appendDeferredTool(ctx, tc)
				return frames, nil
			}
			stopFrames, err := closeActiveAndDeferredFrames(ctx)
			if err != nil {
				return nil, err
			}
			frames = append(frames, stopFrames...)

			idx := ctx.OpenBlock(ir.PartToolCall)
			tc.BlockIndex = idx
			tc.Started = true

			f, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
				Type:  "content_block_start",
				Index: idx,
				ContentBlock: wireToolUseBlockEnc{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: json.RawMessage(`{}`),
				},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, f)
		}

		// Guard: never emit deltas to a block index that is not the currently open Anthropic block.
		if !ctx.BlockOpen || ctx.ActiveBlockType != ir.PartToolCall || tc.BlockIndex != ctx.ActiveBlockIndex {
			return frames, nil
		}

		tc.Args.WriteString(ev.ToolArguments)
		f, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
			Type:  "content_block_delta",
			Index: tc.BlockIndex,
			Delta: wireInputJSONDeltaEnc{
				Type:        "input_json_delta",
				PartialJSON: ev.ToolArguments,
			},
		})
		if err != nil {
			return nil, err
		}
		frames = append(frames, f)
		return frames, nil

	case ir.EventContentBlockEnd:
		if !ctx.BlockOpen {
			return nil, nil
		}
		if ev.BlockType != "" && ev.BlockType != ctx.ActiveBlockType {
			return nil, nil
		}
		if ev.BlockType == ir.PartToolCall && ev.ToolCallID != "" {
			if tc := ctx.LookupToolCall(-1, ev.ToolCallID); tc != nil && tc.BlockIndex != ctx.ActiveBlockIndex {
				return nil, nil
			}
		}
		return closeActiveBlockFrames(ctx)

	case ir.EventFinish:
		if ev.FinishReason != "" {
			fr := ev.FinishReason
			ctx.PendingFinish = &fr
		}
		if ev.RawFinishReason != "" {
			ctx.PendingRawFinish = ev.RawFinishReason
		}
		// Close any open content block and flush deferred interleaved tool calls; defer message_delta + message_stop
		// until EventStreamEnd or FlushStream so trailing EventUsage frames (e.g. OpenAI Chat) are captured.
		return closeActiveAndDeferredFrames(ctx)

	case ir.EventStreamEnd:
		return finalizeAnthropicStream(ctx)

	case ir.EventError:
		errType := nonEmpty(ev.ErrorType, "api_error")
		errMsg := nonEmpty(ev.ErrorMessage, "upstream stream error")
		f, err := makeSSEFrame("error", wireErrorEventEnc{
			Type: "error",
			Error: wireStreamError{
				Type:    errType,
				Message: errMsg,
			},
		})
		if err != nil {
			return nil, err
		}
		ctx.Ended = true
		return []ir.SSEFrame{f}, nil

	case ir.EventPassthrough:
		if ctx.ShouldPreserveMetadata(ev.OriginFormat) && len(ev.PassthroughData) > 0 {
			return encodePassthroughEvent(ev, ctx)
		}
		return nil, nil

	default:
		return nil, nil
	}
}

// FlushStream closes any open content block and emits terminal message_delta and message_stop
// frames if the stream has not already finalized.
func (*Codec) FlushStream(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	return finalizeAnthropicStream(ctx)
}

func encodeContentBlockStartEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ev.BlockType == ir.PartToolCall {
		return encodeToolCallStartEvent(ev, ctx)
	}

	var frames []ir.SSEFrame
	startFrames, err := ensureMessageStartFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, startFrames...)

	switch ev.BlockType {
	case ir.PartReasoning:
		if ev.RedactedData != "" {
			if ev.OriginFormat != ir.FormatUnknown && ev.OriginFormat != ir.FormatAnthropic {
				return frames, nil
			}
			stopFrames, err := closeActiveBlockFrames(ctx)
			if err != nil {
				return nil, err
			}
			frames = append(frames, stopFrames...)

			idx := ctx.OpenBlock(ir.PartReasoning)
			setActiveRedactedBlock(ctx, true)
			f, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
				Type:         "content_block_start",
				Index:        idx,
				ContentBlock: wireRedactedThinkingBlockEnc{Type: "redacted_thinking", Data: ev.RedactedData},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, f)
			return frames, nil
		}

		if !ctx.BlockOpen || ctx.ActiveBlockType != ir.PartReasoning || isActiveRedactedBlock(ctx) {
			stopFrames, err := closeActiveAndDeferredFrames(ctx)
			if err != nil {
				return nil, err
			}
			frames = append(frames, stopFrames...)

			idx := ctx.OpenBlock(ir.PartReasoning)
			setActiveRedactedBlock(ctx, false)
			f, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
				Type:         "content_block_start",
				Index:        idx,
				ContentBlock: wireThinkingBlockEnc{Type: "thinking", Thinking: ""},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, f)
		}
		return frames, nil

	case ir.PartText:
		if !ctx.BlockOpen || ctx.ActiveBlockType != ir.PartText {
			stopFrames, err := closeActiveAndDeferredFrames(ctx)
			if err != nil {
				return nil, err
			}
			frames = append(frames, stopFrames...)

			idx := ctx.OpenBlock(ir.PartText)
			f, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
				Type:         "content_block_start",
				Index:        idx,
				ContentBlock: wireTextBlockEnc{Type: "text", Text: ""},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, f)
		}
		return frames, nil

	default:
		return frames, nil
	}
}

func encodeToolCallStartEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	var frames []ir.SSEFrame
	startFrames, err := ensureMessageStartFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, startFrames...)

	tc, created := ctx.RegisterToolCall(ev.ToolCallIndex, ev.ToolCallID, ev.ToolCallItemID, ev.ToolName, ev.ToolKind, ev.ThoughtSignature)
	if !created && tc.Started {
		// Deduplicate repeated EventToolCallStart for an already-opened tool block.
		if ev.ToolArguments != "" && !tc.Closed && ctx.BlockOpen && ctx.ActiveBlockType == ir.PartToolCall && tc.BlockIndex == ctx.ActiveBlockIndex {
			tc.Args.WriteString(ev.ToolArguments)
			deltaFrame, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
				Type:  "content_block_delta",
				Index: tc.BlockIndex,
				Delta: wireInputJSONDeltaEnc{
					Type:        "input_json_delta",
					PartialJSON: ev.ToolArguments,
				},
			})
			if err != nil {
				return nil, err
			}
			frames = append(frames, deltaFrame)
		}
		return frames, nil
	}

	// If another tool_use block is currently open (e.g. interleaved parallel OpenAI Chat tool calls),
	// defer emitting this new tool_use block until the active tool finishes so prior tool deltas
	// can still stream into their open block without violating Anthropic's single-open-block rule.
	if ctx.BlockOpen && ctx.ActiveBlockType == ir.PartToolCall {
		if ev.ToolArguments != "" {
			tc.Args.WriteString(ev.ToolArguments)
		}
		appendDeferredTool(ctx, tc)
		return frames, nil
	}

	stopFrames, err := closeActiveAndDeferredFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, stopFrames...)

	idx := ctx.OpenBlock(ir.PartToolCall)
	tc.BlockIndex = idx
	tc.Started = true

	startFrame, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
		Type:  "content_block_start",
		Index: idx,
		ContentBlock: wireToolUseBlockEnc{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: json.RawMessage(`{}`),
		},
	})
	if err != nil {
		return nil, err
	}
	frames = append(frames, startFrame)

	if ev.ToolArguments != "" {
		tc.Args.WriteString(ev.ToolArguments)
		deltaFrame, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
			Type:  "content_block_delta",
			Index: idx,
			Delta: wireInputJSONDeltaEnc{
				Type:        "input_json_delta",
				PartialJSON: ev.ToolArguments,
			},
		})
		if err != nil {
			return nil, err
		}
		frames = append(frames, deltaFrame)
	}
	return frames, nil
}

func encodePassthroughEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	var frames []ir.SSEFrame
	startFrames, err := ensureMessageStartFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, startFrames...)

	switch ev.PassthroughEvent {
	case "content_block_start":
		stopFrames, err := closeActiveAndDeferredFrames(ctx)
		if err != nil {
			return nil, err
		}
		frames = append(frames, stopFrames...)
		idx := ctx.OpenBlock(ir.PartType("passthrough"))
		if ctx.Metadata == nil {
			ctx.Metadata = make(map[string]any)
		}
		ctx.Metadata[metaKeyEncodePTActive] = true
		ctx.Metadata[metaKeyEncodePTIndex] = idx
		data := rewriteJSONIndex(ev.PassthroughData, idx)
		frames = append(frames, ir.SSEFrame{Event: ev.PassthroughEvent, Data: data})
		return frames, nil

	case "content_block_delta":
		idx := ctx.ActiveBlockIndex
		if idx < 0 {
			idx = ev.BlockIndex
		}
		data := rewriteJSONIndex(ev.PassthroughData, idx)
		frames = append(frames, ir.SSEFrame{Event: ev.PassthroughEvent, Data: data})
		return frames, nil

	case "content_block_stop":
		idx, _, ok := ctx.CloseActiveBlock()
		if ctx.Metadata != nil {
			ctx.Metadata[metaKeyEncodePTActive] = false
		}
		if !ok {
			idx = ev.BlockIndex
		}
		data := rewriteJSONIndex(ev.PassthroughData, idx)
		frames = append(frames, ir.SSEFrame{Event: ev.PassthroughEvent, Data: data})
		return frames, nil

	default:
		frames = append(frames, ir.SSEFrame{
			Event: ev.PassthroughEvent,
			Data:  bytes.Clone(ev.PassthroughData),
		})
		return frames, nil
	}
}

func rewriteJSONIndex(raw json.RawMessage, idx int) []byte {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return bytes.Clone(raw)
	}
	obj["index"] = idx
	out, err := json.Marshal(obj)
	if err != nil {
		return bytes.Clone(raw)
	}
	return out
}

func ensureMessageStartFrames(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx.Started {
		return nil, nil
	}
	ctx.EnsureEnvelope("msg")
	ctx.ResponseID = ensureMsgIDPrefix(ctx.ResponseID)
	ctx.Started = true

	initialUsage := encodeUsage(ctx.LastUsage)
	// Per Anthropic streaming spec, message_start carries input/cache counts with output_tokens=0 (or initial).
	initialUsage.OutputTokens = 0
	f, err := makeSSEFrame("message_start", wireMessageStartEventEnc{
		Type: "message_start",
		Message: wireStreamMessageStart{
			ID:           ctx.ResponseID,
			Type:         "message",
			Role:         "assistant",
			Model:        ctx.Model,
			Content:      []any{},
			StopReason:   nil,
			StopSequence: nil,
			Usage:        &initialUsage,
		},
	})
	if err != nil {
		return nil, err
	}
	return []ir.SSEFrame{f}, nil
}

func closeActiveBlockFrames(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	idx, bt, ok := ctx.CloseActiveBlock()
	if !ok {
		return nil, nil
	}
	setActiveRedactedBlock(ctx, false)
	if bt == ir.PartToolCall {
		if tc := findToolCallByBlockIndex(ctx, idx); tc != nil {
			tc.Closed = true
		}
	}
	f, err := makeSSEFrame("content_block_stop", wireContentBlockStopEventEnc{
		Type:  "content_block_stop",
		Index: idx,
	})
	if err != nil {
		return nil, err
	}
	return []ir.SSEFrame{f}, nil
}

func closeActiveAndDeferredFrames(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	var frames []ir.SSEFrame
	stopFrames, err := closeActiveBlockFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, stopFrames...)

	for _, tc := range clearDeferredTools(ctx) {
		if tc == nil || tc.Started || tc.Closed {
			continue
		}
		idx := ctx.OpenBlock(ir.PartToolCall)
		tc.BlockIndex = idx
		tc.Started = true
		startFrame, err := makeSSEFrame("content_block_start", wireContentBlockStartEventEnc{
			Type:  "content_block_start",
			Index: idx,
			ContentBlock: wireToolUseBlockEnc{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Name,
				Input: json.RawMessage(`{}`),
			},
		})
		if err != nil {
			return frames, err
		}
		frames = append(frames, startFrame)

		if args := tc.Args.String(); args != "" {
			deltaFrame, err := makeSSEFrame("content_block_delta", wireContentBlockDeltaEventEnc{
				Type:  "content_block_delta",
				Index: idx,
				Delta: wireInputJSONDeltaEnc{
					Type:        "input_json_delta",
					PartialJSON: args,
				},
			})
			if err != nil {
				return frames, err
			}
			frames = append(frames, deltaFrame)
		}

		tcStop, err := closeActiveBlockFrames(ctx)
		if err != nil {
			return frames, err
		}
		frames = append(frames, tcStop...)
	}
	return frames, nil
}

func finalizeAnthropicStream(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil || ctx.Ended {
		return nil, nil
	}
	if !ctx.Started && ctx.PendingFinish == nil && ctx.LastUsage == nil {
		ctx.Ended = true
		return nil, nil
	}

	var frames []ir.SSEFrame
	startFrames, err := ensureMessageStartFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, startFrames...)

	stopFrames, err := closeActiveAndDeferredFrames(ctx)
	if err != nil {
		return nil, err
	}
	frames = append(frames, stopFrames...)

	stopReason := "end_turn"
	if ctx.PendingFinish != nil {
		stopReason = encodeFinishReason(*ctx.PendingFinish, ctx.PendingRawFinish)
	} else if len(ctx.ToolCallOrder) > 0 {
		stopReason = "tool_use"
	}

	deltaFrame, err := makeSSEFrame("message_delta", wireMessageDeltaEventEnc{
		Type: "message_delta",
		Delta: wireMessageDeltaBodyEnc{
			StopReason:   stopReason,
			StopSequence: nil,
		},
		Usage: encodeUsage(ctx.LastUsage),
	})
	if err != nil {
		return nil, err
	}
	frames = append(frames, deltaFrame)

	stopFrame, err := makeSSEFrame("message_stop", wireMessageStopEventEnc{
		Type: "message_stop",
	})
	if err != nil {
		return nil, err
	}
	frames = append(frames, stopFrame)

	ctx.FinishEmitted = true
	ctx.UsageEmitted = true
	ctx.Ended = true
	return frames, nil
}

func makeSSEFrame(event string, payload any) (ir.SSEFrame, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return ir.SSEFrame{}, fmt.Errorf("anthropic: marshal %s frame: %w", event, err)
	}
	return ir.SSEFrame{
		Event: event,
		Data:  b,
	}, nil
}
