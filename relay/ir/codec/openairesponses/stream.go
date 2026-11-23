package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

const (
	metaKeyCompletedOutputs       = "_openai_responses_completed_outputs"
	metaKeyCurrentMsgText         = "_openai_responses_cur_msg_text"
	metaKeyCurrentMsgRefusal      = "_openai_responses_cur_msg_refusal"
	metaKeyCurrentMsgHasText      = "_openai_responses_cur_msg_has_text"
	metaKeyCurrentMsgHasRefuse    = "_openai_responses_cur_msg_has_refusal"
	metaKeyCurrentMsgTextIdx      = "_openai_responses_cur_msg_text_idx"
	metaKeyCurrentMsgRefuseIdx    = "_openai_responses_cur_msg_refusal_idx"
	metaKeyCurrentReasoning       = "_openai_responses_cur_reasoning_text"
	metaKeyCurrentReasoningHasSum = "_openai_responses_cur_reasoning_has_summary"
	metaKeyDecodeItemHasText      = "_openai_responses_dec_item_has_text"
	metaKeyDecodeItemHasRefusal   = "_openai_responses_dec_item_has_refusal"
	metaKeyDecodeItemHasReasoning = "_openai_responses_dec_item_has_reasoning"
	metaKeyDecodeItemHasSummary   = "_openai_responses_dec_item_has_summary"
	metaKeyDecodeLastSummaryIdx   = "_openai_responses_dec_last_summary_idx"
	metaKeyPreferredReasoningID   = "_openai_responses_preferred_rs_id"
)

type responsesCompletedOutput struct {
	OutputIndex int
	Item        any
}

type responsesStreamEventWire struct {
	Type           string                   `json:"type,omitempty"`
	SequenceNumber *int                     `json:"sequence_number,omitempty"`
	Response       *responsesResponseWire   `json:"response,omitempty"`
	Item           *responsesOutputItemWire `json:"item,omitempty"`
	OutputIndex    *int                     `json:"output_index,omitempty"`
	ContentIndex   *int                     `json:"content_index,omitempty"`
	SummaryIndex   *int                     `json:"summary_index,omitempty"`
	ItemID         string                   `json:"item_id,omitempty"`
	Delta          string                   `json:"delta,omitempty"`
	Text           string                   `json:"text,omitempty"`
	Arguments      string                   `json:"arguments,omitempty"`
	Input          string                   `json:"input,omitempty"`
	Refusal        string                   `json:"refusal,omitempty"`
	Error          json.RawMessage          `json:"error,omitempty"`
	Code           string                   `json:"code,omitempty"`
	Message        string                   `json:"message,omitempty"`
}

// DecodeStreamEvent translates a single OpenAI Responses SSE event into zero or more IR StreamEvents.
func (c *Codec) DecodeStreamEvent(sseEvent string, rawData []byte, ctx *ir.StreamContext) ([]ir.StreamEvent, error) {
	trimmed := bytes.TrimSpace(rawData)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[DONE]")) {
		return nil, nil
	}
	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatOpenAIResponses, ir.FormatUnknown)
	}

	var wire responsesStreamEventWire
	if err := json.Unmarshal(trimmed, &wire); err != nil {
		return nil, fmt.Errorf("openairesponses: decode stream event: %w", err)
	}

	eventType := wire.Type
	if eventType == "" {
		eventType = sseEvent
	}

	switch eventType {
	case "response.created", "response.in_progress":
		if wire.Response != nil {
			if wire.Response.ID != "" {
				ctx.ResponseID = wire.Response.ID
			}
			if wire.Response.Model != "" {
				ctx.Model = wire.Response.Model
			}
			if wire.Response.CreatedAt > 0 {
				ctx.Created = wire.Response.CreatedAt
			}
		}
		if !ctx.Started {
			ctx.Started = true
			return []ir.StreamEvent{{
				Type:       ir.EventStreamStart,
				ResponseID: ctx.ResponseID,
				Model:      ctx.Model,
				Created:    ctx.Created,
			}}, nil
		}
		return nil, nil

	case "response.output_item.added":
		return decodeOutputItemAdded(&wire, ctx), nil

	case "response.output_text.delta":
		if wire.Delta == "" {
			return nil, nil
		}
		ctx.AccumulatedText.WriteString(wire.Delta)
		setMetaBool(ctx, metaKeyDecodeItemHasText, true)
		return []ir.StreamEvent{{
			Type:       ir.EventTextDelta,
			BlockIndex: derefInt(wire.OutputIndex, ctx.MessageOutputIndex),
			Text:       wire.Delta,
		}}, nil

	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if wire.Delta == "" {
			return nil, nil
		}
		if eventType == "response.reasoning_text.delta" && getMetaBool(ctx, metaKeyDecodeItemHasSummary) {
			// Avoid duplicating reasoning when an upstream emits both summary_text and reasoning_text deltas.
			return nil, nil
		}
		delta := wire.Delta
		if eventType == "response.reasoning_summary_text.delta" {
			setMetaBool(ctx, metaKeyDecodeItemHasSummary, true)
			if wire.SummaryIndex != nil {
				lastIdx := getMetaInt(ctx, metaKeyDecodeLastSummaryIdx)
				if getMetaBool(ctx, metaKeyDecodeItemHasReasoning) && lastIdx >= 0 && *wire.SummaryIndex != lastIdx {
					delta = "\n\n" + delta
				}
				setMetaInt(ctx, metaKeyDecodeLastSummaryIdx, *wire.SummaryIndex)
			}
		}
		ctx.AccumulatedReasoning.WriteString(delta)
		setMetaBool(ctx, metaKeyDecodeItemHasReasoning, true)
		return []ir.StreamEvent{{
			Type:         ir.EventReasoningDelta,
			BlockIndex:   derefInt(wire.OutputIndex, ctx.ReasoningOutputIndex),
			Reasoning:    delta,
			OriginFormat: ir.FormatOpenAIResponses,
		}}, nil

	case "response.refusal.delta":
		if wire.Delta == "" {
			return nil, nil
		}
		ctx.AccumulatedRefusal.WriteString(wire.Delta)
		setMetaBool(ctx, metaKeyDecodeItemHasRefusal, true)
		return []ir.StreamEvent{{
			Type:       ir.EventRefusalDelta,
			BlockIndex: derefInt(wire.OutputIndex, ctx.MessageOutputIndex),
			Refusal:    wire.Delta,
		}}, nil

	case "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		if wire.Delta == "" {
			return nil, nil
		}
		kind := "function"
		if eventType == "response.custom_tool_call_input.delta" {
			kind = "custom"
		}
		var events []ir.StreamEvent
		tc := lookupResponsesToolCall(ctx, wire.ItemID, wire.OutputIndex)
		if tc == nil {
			outIdx := derefInt(wire.OutputIndex, ctx.NextOutputIndex)
			tc, _ = ctx.RegisterToolCall(-1, wire.ItemID, wire.ItemID, "", kind, "")
			tc.OutputIndex = outIdx
			tc.Started = true
			if wire.ItemID != "" {
				ctx.ToolCallsByID[wire.ItemID] = tc
			}
			recordStreamToolBridge(ctx, tc)
			events = append(events, ir.StreamEvent{
				Type:           ir.EventToolCallStart,
				BlockIndex:     outIdx,
				BlockType:      ir.PartToolCall,
				ToolCallID:     tc.ID,
				ToolCallItemID: tc.ItemID,
				ToolName:       tc.Name,
				ToolCallIndex:  tc.Index,
				ToolKind:       tc.Kind,
				OriginFormat:   ir.FormatOpenAIResponses,
			})
		}
		tc.Args.WriteString(wire.Delta)
		events = append(events, ir.StreamEvent{
			Type:           ir.EventToolCallDelta,
			BlockIndex:     derefInt(wire.OutputIndex, tc.OutputIndex),
			ToolCallID:     tc.ID,
			ToolCallItemID: tc.ItemID,
			ToolName:       tc.Name,
			ToolCallIndex:  tc.Index,
			ToolArguments:  wire.Delta,
			ToolKind:       tc.Kind,
			OriginFormat:   ir.FormatOpenAIResponses,
		})
		return events, nil

	case "response.output_item.done":
		return decodeOutputItemDone(&wire, ctx), nil

	case "response.completed", "response.incomplete":
		return decodeStreamTerminal(eventType, &wire, ctx), nil

	case "response.failed", "error":
		errMsg, errType, errCode := extractStreamError(&wire)
		return []ir.StreamEvent{{
			Type:         ir.EventError,
			ErrorMessage: errMsg,
			ErrorType:    errType,
			ErrorCode:    errCode,
		}}, nil

	default:
		// Lifecycle sub-events (content_part.added/done, output_text.done, reasoning_summary_part.*, etc.)
		// are handled via deltas and output_item.done.
		return nil, nil
	}
}

func decodeOutputItemAdded(wire *responsesStreamEventWire, ctx *ir.StreamContext) []ir.StreamEvent {
	if wire.Item == nil {
		return nil
	}
	item := wire.Item
	outIdx := derefInt(wire.OutputIndex, 0)

	switch item.Type {
	case "function_call", "custom_tool_call":
		kind := "function"
		if item.Type == "custom_tool_call" {
			kind = "custom"
		}
		callID := item.CallID
		if callID == "" {
			callID = item.ID
		}
		tc, _ := ctx.RegisterToolCall(-1, callID, item.ID, item.Name, kind, "")
		tc.OutputIndex = outIdx
		tc.Started = true
		if item.ID != "" {
			ctx.ToolCallsByID[item.ID] = tc
		}
		recordStreamToolBridge(ctx, tc)

		events := []ir.StreamEvent{{
			Type:           ir.EventToolCallStart,
			BlockIndex:     outIdx,
			BlockType:      ir.PartToolCall,
			ToolCallID:     tc.ID,
			ToolCallItemID: tc.ItemID,
			ToolName:       tc.Name,
			ToolCallIndex:  tc.Index,
			ToolKind:       kind,
			OriginFormat:   ir.FormatOpenAIResponses,
		}}

		initialArgs := item.Arguments
		if kind == "custom" && item.Input != "" {
			initialArgs = item.Input
		}
		if initialArgs != "" {
			tc.Args.WriteString(initialArgs)
			events = append(events, ir.StreamEvent{
				Type:           ir.EventToolCallDelta,
				BlockIndex:     outIdx,
				ToolCallID:     tc.ID,
				ToolCallItemID: tc.ItemID,
				ToolName:       tc.Name,
				ToolCallIndex:  tc.Index,
				ToolArguments:  initialArgs,
				ToolKind:       kind,
				OriginFormat:   ir.FormatOpenAIResponses,
			})
		}
		return events

	case "reasoning":
		ctx.ReasoningItemID = item.ID
		ctx.ReasoningOutputIndex = outIdx
		ctx.ReasoningItemStarted = true
		ctx.ReasoningOrigin = ir.FormatOpenAIResponses
		setMetaBool(ctx, metaKeyDecodeItemHasReasoning, false)
		setMetaBool(ctx, metaKeyDecodeItemHasSummary, false)
		setMetaInt(ctx, metaKeyDecodeLastSummaryIdx, -1)
		ev := ir.StreamEvent{
			Type:         ir.EventContentBlockStart,
			BlockIndex:   outIdx,
			BlockType:    ir.PartReasoning,
			OriginFormat: ir.FormatOpenAIResponses,
		}
		if item.ID != "" {
			ev.ProviderMetadata = map[string]any{"item_id": item.ID}
		}
		return []ir.StreamEvent{ev}

	case "message":
		ctx.MessageItemID = item.ID
		ctx.MessageOutputIndex = outIdx
		ctx.MessageItemStarted = true
		setMetaBool(ctx, metaKeyDecodeItemHasText, false)
		setMetaBool(ctx, metaKeyDecodeItemHasRefusal, false)
		return []ir.StreamEvent{{
			Type:         ir.EventContentBlockStart,
			BlockIndex:   outIdx,
			BlockType:    ir.PartText,
			OriginFormat: ir.FormatOpenAIResponses,
		}}

	case "web_search_call", "file_search_call", "image_generation_call":
		countBuiltinStreamToolOnce(ctx, item.Type, item.ID, outIdx, item.Quality, item.Size)
		return nil

	default:
		return nil
	}
}

func decodeOutputItemDone(wire *responsesStreamEventWire, ctx *ir.StreamContext) []ir.StreamEvent {
	if wire.Item == nil {
		return nil
	}
	item := wire.Item
	outIdx := derefInt(wire.OutputIndex, 0)

	switch item.Type {
	case "function_call", "custom_tool_call":
		kind := "function"
		if item.Type == "custom_tool_call" {
			kind = "custom"
		}
		callID := item.CallID
		if callID == "" {
			callID = item.ID
		}
		var events []ir.StreamEvent
		tc := lookupResponsesToolCall(ctx, callID, nil)
		if tc == nil {
			tc = lookupResponsesToolCall(ctx, item.ID, wire.OutputIndex)
		}
		if tc == nil {
			tc, _ = ctx.RegisterToolCall(-1, callID, item.ID, item.Name, kind, "")
			tc.OutputIndex = outIdx
			tc.Started = true
			if item.ID != "" {
				ctx.ToolCallsByID[item.ID] = tc
			}
			recordStreamToolBridge(ctx, tc)
			events = append(events, ir.StreamEvent{
				Type:           ir.EventToolCallStart,
				BlockIndex:     outIdx,
				BlockType:      ir.PartToolCall,
				ToolCallID:     tc.ID,
				ToolCallItemID: tc.ItemID,
				ToolName:       tc.Name,
				ToolCallIndex:  tc.Index,
				ToolKind:       kind,
				OriginFormat:   ir.FormatOpenAIResponses,
			})
		} else {
			if item.ID != "" && tc.ItemID == "" {
				tc.ItemID = item.ID
				ctx.ToolCallsByID[item.ID] = tc
			}
			if item.Name != "" && tc.Name == "" {
				tc.Name = item.Name
			}
			recordStreamToolBridge(ctx, tc)
		}

		fullArgs := item.Arguments
		if kind == "custom" && item.Input != "" {
			fullArgs = item.Input
		}
		if tc.Args.Len() == 0 && fullArgs != "" {
			tc.Args.WriteString(fullArgs)
			events = append(events, ir.StreamEvent{
				Type:           ir.EventToolCallDelta,
				BlockIndex:     outIdx,
				ToolCallID:     tc.ID,
				ToolCallItemID: tc.ItemID,
				ToolName:       tc.Name,
				ToolCallIndex:  tc.Index,
				ToolArguments:  fullArgs,
				ToolKind:       kind,
				OriginFormat:   ir.FormatOpenAIResponses,
			})
		}
		tc.Closed = true
		events = append(events, ir.StreamEvent{
			Type:           ir.EventContentBlockEnd,
			BlockIndex:     outIdx,
			BlockType:      ir.PartToolCall,
			ToolCallID:     tc.ID,
			ToolCallItemID: tc.ItemID,
			ToolCallIndex:  tc.Index,
			OriginFormat:   ir.FormatOpenAIResponses,
		})
		return events

	case "reasoning":
		var events []ir.StreamEvent
		if !getMetaBool(ctx, metaKeyDecodeItemHasReasoning) {
			decoded := decodeReasoningItem(item.ID, item.Summary, item.Content, item.EncryptedContent)
			if decoded.Text != "" {
				ctx.AccumulatedReasoning.WriteString(decoded.Text)
				setMetaBool(ctx, metaKeyDecodeItemHasReasoning, true)
				events = append(events, ir.StreamEvent{
					Type:         ir.EventReasoningDelta,
					BlockIndex:   outIdx,
					Reasoning:    decoded.Text,
					OriginFormat: ir.FormatOpenAIResponses,
				})
			}
		}
		if item.EncryptedContent != "" {
			ctx.ReasoningRedacted = item.EncryptedContent
			ctx.ReasoningOrigin = ir.FormatOpenAIResponses
			events = append(events, ir.StreamEvent{
				Type:         ir.EventReasoningSignatureDelta,
				BlockIndex:   outIdx,
				BlockType:    ir.PartReasoning,
				RedactedData: item.EncryptedContent,
				OriginFormat: ir.FormatOpenAIResponses,
			})
		}
		ctx.ReasoningItemDone = true
		events = append(events, ir.StreamEvent{
			Type:         ir.EventContentBlockEnd,
			BlockIndex:   outIdx,
			BlockType:    ir.PartReasoning,
			RedactedData: item.EncryptedContent,
			OriginFormat: ir.FormatOpenAIResponses,
		})
		return events

	case "message":
		var events []ir.StreamEvent
		if (!getMetaBool(ctx, metaKeyDecodeItemHasText) || !getMetaBool(ctx, metaKeyDecodeItemHasRefusal)) && len(item.Content) > 0 {
			if parts, err := decodeMessageContent(item.Content); err == nil {
				for _, p := range parts {
					if p.Type == ir.PartText && p.Text != "" && !getMetaBool(ctx, metaKeyDecodeItemHasText) {
						ctx.AccumulatedText.WriteString(p.Text)
						events = append(events, ir.StreamEvent{
							Type:       ir.EventTextDelta,
							BlockIndex: outIdx,
							Text:       p.Text,
						})
					} else if p.Type == ir.PartRefusal && p.Refusal != "" && !getMetaBool(ctx, metaKeyDecodeItemHasRefusal) {
						ctx.AccumulatedRefusal.WriteString(p.Refusal)
						events = append(events, ir.StreamEvent{
							Type:       ir.EventRefusalDelta,
							BlockIndex: outIdx,
							Refusal:    p.Refusal,
						})
					}
				}
			}
		}
		ctx.MessageItemDone = true
		events = append(events, ir.StreamEvent{
			Type:       ir.EventContentBlockEnd,
			BlockIndex: outIdx,
			BlockType:  ir.PartText,
		})
		return events

	case "web_search_call", "file_search_call", "image_generation_call":
		countBuiltinStreamToolOnce(ctx, item.Type, item.ID, outIdx, item.Quality, item.Size)
		return nil

	default:
		return nil
	}
}

func decodeStreamTerminal(eventType string, wire *responsesStreamEventWire, ctx *ir.StreamContext) []ir.StreamEvent {
	var events []ir.StreamEvent
	status := "completed"
	if eventType == "response.incomplete" {
		status = "incomplete"
	}

	var incomplete *responsesIncompleteDetailsWire
	hasToolCalls := len(ctx.ToolCallOrder) > 0
	var usage *ir.Usage

	if wire.Response != nil {
		if wire.Response.ID != "" && ctx.ResponseID == "" {
			ctx.ResponseID = wire.Response.ID
		}
		if wire.Response.Model != "" && ctx.Model == "" {
			ctx.Model = wire.Response.Model
		}
		if wire.Response.CreatedAt > 0 && ctx.Created == 0 {
			ctx.Created = wire.Response.CreatedAt
		}
		if wire.Response.Status != "" {
			status = wire.Response.Status
		}
		incomplete = wire.Response.IncompleteDetails
		usage = decodeResponsesUsage(wire.Response.Usage)

		noPriorItems := !ctx.MessageItemStarted && !ctx.ReasoningItemStarted && len(ctx.ToolCallOrder) == 0 && ctx.AccumulatedText.Len() == 0 && ctx.AccumulatedReasoning.Len() == 0
		for idx, rawItem := range wire.Response.Output {
			var item responsesOutputItemWire
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			switch item.Type {
			case "function_call", "custom_tool_call":
				hasToolCalls = true
				if noPriorItems {
					outIdx := idx
					events = append(events, decodeOutputItemDone(&responsesStreamEventWire{
						OutputIndex: &outIdx,
						Item:        &item,
					}, ctx)...)
				}
			case "message", "reasoning":
				if noPriorItems {
					outIdx := idx
					events = append(events, decodeOutputItemDone(&responsesStreamEventWire{
						OutputIndex: &outIdx,
						Item:        &item,
					}, ctx)...)
				}
			case "web_search_call", "file_search_call", "image_generation_call":
				countBuiltinStreamToolOnce(ctx, item.Type, item.ID, idx, item.Quality, item.Size)
			}
		}
	}

	if usage == nil && (ctx.WebSearchCalls > 0 || ctx.FileSearchCalls > 0 || ctx.HasImageGenCall) {
		usage = &ir.Usage{InputExclusive: false}
	}
	if usage != nil {
		if ctx.WebSearchCalls > usage.WebSearchRequests {
			usage.WebSearchRequests = ctx.WebSearchCalls
		}
		if ctx.FileSearchCalls > usage.FileSearchCalls {
			usage.FileSearchCalls = ctx.FileSearchCalls
		}
		if ctx.HasImageGenCall {
			usage.HasImageGenCall = true
		}
		if ctx.ImageGenQuality != "" && usage.ImageGenQuality == "" {
			usage.ImageGenQuality = ctx.ImageGenQuality
		}
		if ctx.ImageGenSize != "" && usage.ImageGenSize == "" {
			usage.ImageGenSize = ctx.ImageGenSize
		}
		ctx.MergeUsage(usage)
		events = append(events, ir.StreamEvent{
			Type:  ir.EventUsage,
			Usage: usage,
		})
	}

	finishReason, rawFinish := deriveFinishReason(status, incomplete, hasToolCalls)
	events = append(events, ir.StreamEvent{
		Type:            ir.EventFinish,
		FinishReason:    finishReason,
		RawFinishReason: rawFinish,
	})
	events = append(events, ir.StreamEvent{
		Type: ir.EventStreamEnd,
	})
	ctx.Ended = true
	return events
}

func countBuiltinStreamToolOnce(ctx *ir.StreamContext, itemType, itemID string, outIdx int, quality, size string) {
	if ctx == nil {
		return
	}
	if itemType == "image_generation_call" {
		if quality != "" {
			ctx.ImageGenQuality = quality
		}
		if size != "" {
			ctx.ImageGenSize = size
		}
	}
	if ctx.SeenToolItemIDs == nil {
		ctx.SeenToolItemIDs = make(map[string]struct{})
	}
	key := itemID
	if key == "" {
		key = fmt.Sprintf("%s:%d", itemType, outIdx)
	}
	if _, seen := ctx.SeenToolItemIDs[key]; seen {
		return
	}
	ctx.SeenToolItemIDs[key] = struct{}{}
	switch itemType {
	case "web_search_call":
		ctx.WebSearchCalls++
	case "file_search_call":
		ctx.FileSearchCalls++
	case "image_generation_call":
		ctx.HasImageGenCall = true
	}
}

func lookupResponsesToolCall(ctx *ir.StreamContext, idOrItemID string, outputIdx *int) *ir.TrackedToolCall {
	if ctx == nil {
		return nil
	}
	if idOrItemID != "" {
		if tc := ctx.ToolCallsByID[idOrItemID]; tc != nil {
			return tc
		}
		for _, tc := range ctx.ToolCallOrder {
			if tc.ID == idOrItemID || tc.ItemID == idOrItemID {
				return tc
			}
		}
	}
	if outputIdx != nil {
		for _, tc := range ctx.ToolCallOrder {
			if tc.OutputIndex == *outputIdx {
				return tc
			}
		}
	}
	return nil
}

func recordStreamToolBridge(ctx *ir.StreamContext, tc *ir.TrackedToolCall) {
	if ctx == nil || ctx.BridgeState == nil || tc == nil || tc.ID == "" {
		return
	}
	ctx.BridgeState.RecordToolCall(ir.ToolCallBridgeEntry{
		ToolCallID:      tc.ID,
		ResponsesItemID: tc.ItemID,
		ToolName:        tc.Name,
		OriginFormat:    ir.FormatOpenAIResponses,
	})
}

func extractStreamError(wire *responsesStreamEventWire) (msg, errType, errCode string) {
	errType = "error"
	var rawErr json.RawMessage
	if len(bytes.TrimSpace(wire.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(wire.Error), []byte("null")) {
		rawErr = wire.Error
	} else if wire.Response != nil && len(bytes.TrimSpace(wire.Response.Error)) > 0 && !bytes.Equal(bytes.TrimSpace(wire.Response.Error), []byte("null")) {
		rawErr = wire.Response.Error
	}
	if len(rawErr) > 0 {
		var errObj struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		}
		if err := json.Unmarshal(rawErr, &errObj); err == nil {
			msg = errObj.Message
			if errObj.Type != "" {
				errType = errObj.Type
			}
			if errObj.Code != nil {
				errCode = fmt.Sprintf("%v", errObj.Code)
			}
		} else {
			msg = strings.Trim(string(rawErr), `"`)
		}
	}
	if msg == "" {
		msg = wire.Message
	}
	if errCode == "" {
		errCode = wire.Code
	}
	if msg == "" {
		msg = "upstream response failed"
	}
	return msg, errType, errCode
}

func derefInt(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

// EncodeStreamEvent translates a single IR StreamEvent into zero or more OpenAI Responses SSEFrames.
func (c *Codec) EncodeStreamEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatUnknown, ir.FormatOpenAIResponses)
	}
	if (ctx.Ended || ctx.FinishEmitted) && ev.Type != ir.EventError {
		return nil, nil
	}

	var frames []ir.SSEFrame

	switch ev.Type {
	case ir.EventStreamStart:
		if ev.ResponseID != "" {
			ctx.ResponseID = ensureResponseID(ev.ResponseID)
		}
		if ev.Model != "" {
			ctx.Model = ev.Model
		}
		if ev.Created > 0 {
			ctx.Created = ev.Created
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		return frames, nil

	case ir.EventContentBlockStart:
		if ev.BlockType == ir.PartReasoning && ev.ProviderMetadata != nil {
			if id, ok := ev.ProviderMetadata["item_id"].(string); ok && id != "" {
				setMetaString(ctx, metaKeyPreferredReasoningID, id)
			}
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		return frames, nil

	case ir.EventReasoningDelta:
		if ev.Reasoning == "" {
			return nil, nil
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		frames = append(frames, closeOpenMessageItem(ctx)...)
		frames = append(frames, closeOpenToolCalls(ctx)...)
		frames = append(frames, ensureReasoningSummaryOpen(ctx, ev.OriginFormat)...)

		ctx.AccumulatedReasoning.WriteString(ev.Reasoning)
		appendMetaString(ctx, metaKeyCurrentReasoning, ev.Reasoning)

		frames = append(frames, makeSSEFrame(ctx, "response.reasoning_summary_text.delta", map[string]any{
			"item_id":       ctx.ReasoningItemID,
			"output_index":  ctx.ReasoningOutputIndex,
			"summary_index": 0,
			"delta":         ev.Reasoning,
		}))
		return frames, nil

	case ir.EventReasoningSignatureDelta:
		if ev.OriginFormat == ir.FormatOpenAIResponses && ev.RedactedData != "" {
			if shouldPreserveResponsesNative(ev.OriginFormat, ctx.ConversionContext) {
				frames = append(frames, ensureStreamStarted(ctx)...)
				frames = append(frames, closeOpenMessageItem(ctx)...)
				frames = append(frames, closeOpenToolCalls(ctx)...)
				frames = append(frames, ensureReasoningItemOpen(ctx, ev.OriginFormat)...)
				ctx.ReasoningRedacted = ev.RedactedData
				ctx.ReasoningOrigin = ir.FormatOpenAIResponses
			}
			return frames, nil
		}
		if ev.Signature != "" || ev.RedactedData != "" {
			ctx.AddWarning("dropped_foreign_reasoning_opaque", "stream.reasoning", fmt.Sprintf("dropped foreign reasoning signature/redacted_data from origin %q", ev.OriginFormat))
		}
		return nil, nil

	case ir.EventTextDelta:
		if ev.Text == "" {
			return nil, nil
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		frames = append(frames, closeOpenReasoningItem(ctx)...)
		frames = append(frames, closeOpenToolCalls(ctx)...)
		frames = append(frames, ensureMessageTextPartOpen(ctx)...)

		ctx.AccumulatedText.WriteString(ev.Text)
		appendMetaString(ctx, metaKeyCurrentMsgText, ev.Text)

		frames = append(frames, makeSSEFrame(ctx, "response.output_text.delta", map[string]any{
			"item_id":       ctx.MessageItemID,
			"output_index":  ctx.MessageOutputIndex,
			"content_index": getMetaInt(ctx, metaKeyCurrentMsgTextIdx),
			"delta":         ev.Text,
		}))
		return frames, nil

	case ir.EventRefusalDelta:
		if ev.Refusal == "" {
			return nil, nil
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		frames = append(frames, closeOpenReasoningItem(ctx)...)
		frames = append(frames, closeOpenToolCalls(ctx)...)
		frames = append(frames, ensureMessageRefusalPartOpen(ctx)...)

		ctx.AccumulatedRefusal.WriteString(ev.Refusal)
		appendMetaString(ctx, metaKeyCurrentMsgRefusal, ev.Refusal)

		frames = append(frames, makeSSEFrame(ctx, "response.refusal.delta", map[string]any{
			"item_id":       ctx.MessageItemID,
			"output_index":  ctx.MessageOutputIndex,
			"content_index": getMetaInt(ctx, metaKeyCurrentMsgRefuseIdx),
			"delta":         ev.Refusal,
		}))
		return frames, nil

	case ir.EventImage:
		md := formatResponsesImageMarkdown(ev.Image)
		if md == "" {
			return nil, nil
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		frames = append(frames, closeOpenReasoningItem(ctx)...)
		frames = append(frames, closeOpenToolCalls(ctx)...)
		frames = append(frames, ensureMessageTextPartOpen(ctx)...)

		ctx.AccumulatedText.WriteString(md)
		appendMetaString(ctx, metaKeyCurrentMsgText, md)

		frames = append(frames, makeSSEFrame(ctx, "response.output_text.delta", map[string]any{
			"item_id":       ctx.MessageItemID,
			"output_index":  ctx.MessageOutputIndex,
			"content_index": getMetaInt(ctx, metaKeyCurrentMsgTextIdx),
			"delta":         md,
		}))
		return frames, nil

	case ir.EventToolCallStart:
		frames = append(frames, ensureStreamStarted(ctx)...)
		frames = append(frames, closeOpenReasoningItem(ctx)...)
		frames = append(frames, closeOpenMessageItem(ctx)...)

		tc, startFrames := openOrGetToolCall(ctx, ev)
		frames = append(frames, startFrames...)

		if ev.ToolArguments != "" && tc != nil && !tc.Closed {
			tc.Args.WriteString(ev.ToolArguments)
			frames = append(frames, makeToolCallDeltaFrame(ctx, tc, ev.ToolArguments))
		}
		return frames, nil

	case ir.EventToolCallDelta:
		if ev.ToolArguments == "" {
			return nil, nil
		}
		frames = append(frames, ensureStreamStarted(ctx)...)
		frames = append(frames, closeOpenReasoningItem(ctx)...)
		frames = append(frames, closeOpenMessageItem(ctx)...)

		tc := ctx.LookupToolCall(ev.ToolCallIndex, ev.ToolCallID)
		if tc != nil && tc.Closed {
			return nil, nil
		}
		if tc == nil && ev.ToolName == "" && ev.ToolCallID != "" {
			// Ignore stray delta referencing an unregistered/unknown tool ID without a tool name.
			return nil, nil
		}
		if tc == nil || !tc.Started {
			var startFrames []ir.SSEFrame
			tc, startFrames = openOrGetToolCall(ctx, ev)
			frames = append(frames, startFrames...)
		}
		if tc == nil || tc.Closed {
			return frames, nil
		}
		tc.Args.WriteString(ev.ToolArguments)
		frames = append(frames, makeToolCallDeltaFrame(ctx, tc, ev.ToolArguments))
		return frames, nil

	case ir.EventContentBlockEnd:
		switch ev.BlockType {
		case ir.PartReasoning:
			return closeOpenReasoningItem(ctx), nil
		case ir.PartText, ir.PartRefusal:
			return closeOpenMessageItem(ctx), nil
		case ir.PartToolCall:
			var tc *ir.TrackedToolCall
			if ev.ToolCallID != "" {
				tc = ctx.LookupToolCall(-1, ev.ToolCallID)
			}
			if tc == nil && ev.BlockIndex >= 0 {
				tc = ctx.LookupToolCallByBlockIndex(ev.BlockIndex)
			}
			if tc == nil && ev.ToolCallIndex >= 0 {
				tc = ctx.LookupToolCall(ev.ToolCallIndex, "")
			}
			if tc != nil {
				return closeSingleToolCall(ctx, tc, "completed"), nil
			}
			return nil, nil
		default:
			if ev.BlockType == "" && ev.BlockIndex >= 0 {
				if tc := ctx.LookupToolCallByBlockIndex(ev.BlockIndex); tc != nil {
					return closeSingleToolCall(ctx, tc, "completed"), nil
				}
			}
			return nil, nil
		}

	case ir.EventUsage:
		ctx.MergeUsage(ev.Usage)
		return nil, nil

	case ir.EventFinish:
		if ev.Usage != nil {
			ctx.MergeUsage(ev.Usage)
		}
		fr := ev.FinishReason
		ctx.PendingFinish = &fr
		ctx.PendingRawFinish = ev.RawFinishReason

		// For OpenAI Chat upstreams using stream_options.include_usage, finish_reason arrives one
		// chunk before the final usage-only chunk; defer terminal frame until FlushStream/StreamEnd
		// if no non-zero usage has been observed yet.
		if ctx.SourceFormat == ir.FormatOpenAIChat && (ctx.LastUsage == nil || (ctx.LastUsage.InputTokens == 0 && ctx.LastUsage.OutputTokens == 0)) {
			return nil, nil
		}
		return emitTerminalCompletion(ctx), nil

	case ir.EventStreamEnd:
		return c.FlushStream(ctx)

	case ir.EventError:
		errCode := ev.ErrorCode
		if errCode == "" {
			errCode = ev.ErrorType
		}
		errMsg := ev.ErrorMessage
		if errMsg == "" {
			errMsg = "stream error"
		}
		frames = append(frames, makeSSEFrame(ctx, "error", map[string]any{
			"code":    errCode,
			"message": errMsg,
			"param":   nil,
		}))
		// Terminate stream so FlushStream never fabricates response.completed after an error.
		ctx.FinishEmitted = true
		ctx.Ended = true
		return frames, nil

	case ir.EventPassthrough:
		if len(ev.PassthroughData) > 0 && shouldPreserveResponsesNative(ev.OriginFormat, ctx.ConversionContext) {
			data := rewritePassthroughSeq(ctx, ev.PassthroughData)
			return []ir.SSEFrame{{
				Event: ev.PassthroughEvent,
				Data:  data,
			}}, nil
		}
		return nil, nil

	default:
		return nil, nil
	}
}

// FlushStream closes any open items and emits the terminal response.completed/response.incomplete event.
func (c *Codec) FlushStream(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil || ctx.FinishEmitted {
		return nil, nil
	}
	if !ctx.Started && ctx.PendingFinish == nil {
		ctx.FinishEmitted = true
		return nil, nil
	}
	return emitTerminalCompletion(ctx), nil
}

func emitTerminalCompletion(ctx *ir.StreamContext) []ir.SSEFrame {
	if ctx.FinishEmitted {
		return nil
	}
	eventType := "response.completed"
	status := "completed"
	itemStatus := "completed"
	var incomplete *responsesIncompleteDetailsWire

	if ctx.PendingFinish != nil {
		switch *ctx.PendingFinish {
		case ir.FinishLength:
			eventType = "response.incomplete"
			status = "incomplete"
			itemStatus = "incomplete"
			incomplete = &responsesIncompleteDetailsWire{Reason: "max_output_tokens"}
		case ir.FinishContentFilter:
			eventType = "response.incomplete"
			status = "incomplete"
			itemStatus = "incomplete"
			incomplete = &responsesIncompleteDetailsWire{Reason: "content_filter"}
		case ir.FinishError:
			eventType = "response.failed"
			status = "failed"
			itemStatus = "incomplete"
		}
	}

	var frames []ir.SSEFrame
	frames = append(frames, ensureStreamStarted(ctx)...)
	frames = append(frames, closeOpenReasoningItemWithStatus(ctx, itemStatus)...)
	frames = append(frames, closeOpenMessageItemWithStatus(ctx, itemStatus)...)
	frames = append(frames, closeOpenToolCallsWithStatus(ctx, itemStatus)...)

	respObj := buildStreamResponseObject(ctx, status, collectCompletedOutputs(ctx), encodeResponsesUsage(ctx.LastUsage), incomplete)
	if status == "failed" {
		msg := "upstream generation failed"
		if ctx.PendingRawFinish != "" {
			msg = fmt.Sprintf("upstream generation failed (%s)", ctx.PendingRawFinish)
		}
		respObj["error"] = map[string]any{
			"code":    "server_error",
			"message": msg,
		}
	}
	frames = append(frames, makeSSEFrame(ctx, eventType, map[string]any{
		"response": respObj,
	}))

	ctx.FinishEmitted = true
	return frames
}

func ensureStreamStarted(ctx *ir.StreamContext) []ir.SSEFrame {
	if ctx.Started {
		return nil
	}
	ctx.EnsureEnvelope("resp")
	ctx.ResponseID = ensureResponseID(ctx.ResponseID)
	ctx.Started = true

	respObj := buildStreamResponseObject(ctx, "in_progress", []any{}, nil, nil)
	return []ir.SSEFrame{
		makeSSEFrame(ctx, "response.created", map[string]any{"response": respObj}),
		makeSSEFrame(ctx, "response.in_progress", map[string]any{"response": respObj}),
	}
}

func buildStreamResponseObject(ctx *ir.StreamContext, status string, output []any, usage *responsesUsageWire, incomplete *responsesIncompleteDetailsWire) map[string]any {
	if output == nil {
		output = []any{}
	}
	obj := map[string]any{
		"id":         ctx.ResponseID,
		"object":     "response",
		"created_at": ctx.Created,
		"status":     status,
		"model":      ctx.Model,
		"output":     output,
	}
	if incomplete != nil {
		obj["incomplete_details"] = incomplete
	}
	if usage != nil {
		obj["usage"] = usage
	}
	return obj
}

func ensureReasoningItemOpen(ctx *ir.StreamContext, origin ir.Format) []ir.SSEFrame {
	if ctx.ReasoningItemStarted && !ctx.ReasoningItemDone {
		return nil
	}
	outIdx := ctx.NextOutputIndex
	ctx.NextOutputIndex++
	rsID := getMetaString(ctx, metaKeyPreferredReasoningID)
	setMetaString(ctx, metaKeyPreferredReasoningID, "")
	if rsID == "" {
		rsID = fmt.Sprintf("rs_%s_%d", shortIDSuffix(ctx.ResponseID), outIdx)
	}

	ctx.ReasoningItemID = rsID
	ctx.ReasoningOutputIndex = outIdx
	ctx.ReasoningItemStarted = true
	ctx.ReasoningItemDone = false
	ctx.ReasoningOrigin = origin
	ctx.ReasoningRedacted = ""
	setMetaString(ctx, metaKeyCurrentReasoning, "")
	setMetaBool(ctx, metaKeyCurrentReasoningHasSum, false)

	return []ir.SSEFrame{
		makeSSEFrame(ctx, "response.output_item.added", map[string]any{
			"output_index": outIdx,
			"item": map[string]any{
				"id":      rsID,
				"type":    "reasoning",
				"status":  "in_progress",
				"summary": []any{},
			},
		}),
	}
}

func ensureReasoningSummaryOpen(ctx *ir.StreamContext, origin ir.Format) []ir.SSEFrame {
	var frames []ir.SSEFrame
	frames = append(frames, ensureReasoningItemOpen(ctx, origin)...)
	if getMetaBool(ctx, metaKeyCurrentReasoningHasSum) {
		return frames
	}
	setMetaBool(ctx, metaKeyCurrentReasoningHasSum, true)
	frames = append(frames, makeSSEFrame(ctx, "response.reasoning_summary_part.added", map[string]any{
		"item_id":       ctx.ReasoningItemID,
		"output_index":  ctx.ReasoningOutputIndex,
		"summary_index": 0,
		"part": map[string]any{
			"type": "summary_text",
			"text": "",
		},
	}))
	return frames
}

func closeOpenReasoningItem(ctx *ir.StreamContext) []ir.SSEFrame {
	return closeOpenReasoningItemWithStatus(ctx, "completed")
}

func closeOpenReasoningItemWithStatus(ctx *ir.StreamContext, itemStatus string) []ir.SSEFrame {
	if !ctx.ReasoningItemStarted || ctx.ReasoningItemDone {
		return nil
	}
	if itemStatus == "" {
		itemStatus = "completed"
	}
	var frames []ir.SSEFrame
	summary := make([]map[string]any, 0, 1)

	if getMetaBool(ctx, metaKeyCurrentReasoningHasSum) {
		fullText := getMetaString(ctx, metaKeyCurrentReasoning)
		summaryPart := map[string]any{
			"type": "summary_text",
			"text": fullText,
		}
		summary = append(summary, summaryPart)
		frames = append(frames,
			makeSSEFrame(ctx, "response.reasoning_summary_text.done", map[string]any{
				"item_id":       ctx.ReasoningItemID,
				"output_index":  ctx.ReasoningOutputIndex,
				"summary_index": 0,
				"text":          fullText,
			}),
			makeSSEFrame(ctx, "response.reasoning_summary_part.done", map[string]any{
				"item_id":       ctx.ReasoningItemID,
				"output_index":  ctx.ReasoningOutputIndex,
				"summary_index": 0,
				"part":          summaryPart,
			}),
		)
	}

	completedItem := map[string]any{
		"id":      ctx.ReasoningItemID,
		"type":    "reasoning",
		"status":  itemStatus,
		"summary": summary,
	}
	if ctx.ReasoningOrigin == ir.FormatOpenAIResponses && ctx.ReasoningRedacted != "" && shouldPreserveResponsesNative(ctx.ReasoningOrigin, ctx.ConversionContext) {
		completedItem["encrypted_content"] = ctx.ReasoningRedacted
	}

	frames = append(frames, makeSSEFrame(ctx, "response.output_item.done", map[string]any{
		"output_index": ctx.ReasoningOutputIndex,
		"item":         completedItem,
	}))

	recordCompletedOutput(ctx, ctx.ReasoningOutputIndex, completedItem)
	ctx.ReasoningItemDone = true
	return frames
}

func ensureMessageOpen(ctx *ir.StreamContext) []ir.SSEFrame {
	if ctx.MessageItemStarted && !ctx.MessageItemDone {
		return nil
	}
	outIdx := ctx.NextOutputIndex
	ctx.NextOutputIndex++
	msgID := fmt.Sprintf("msg_%s_%d", shortIDSuffix(ctx.ResponseID), outIdx)

	ctx.MessageItemID = msgID
	ctx.MessageOutputIndex = outIdx
	ctx.MessageItemStarted = true
	ctx.MessageItemDone = false
	setMetaString(ctx, metaKeyCurrentMsgText, "")
	setMetaString(ctx, metaKeyCurrentMsgRefusal, "")
	setMetaBool(ctx, metaKeyCurrentMsgHasText, false)
	setMetaBool(ctx, metaKeyCurrentMsgHasRefuse, false)
	setMetaInt(ctx, metaKeyCurrentMsgTextIdx, -1)
	setMetaInt(ctx, metaKeyCurrentMsgRefuseIdx, -1)

	return []ir.SSEFrame{
		makeSSEFrame(ctx, "response.output_item.added", map[string]any{
			"output_index": outIdx,
			"item": map[string]any{
				"id":      msgID,
				"type":    "message",
				"status":  "in_progress",
				"role":    "assistant",
				"content": []any{},
			},
		}),
	}
}

func ensureMessageTextPartOpen(ctx *ir.StreamContext) []ir.SSEFrame {
	var frames []ir.SSEFrame
	frames = append(frames, ensureMessageOpen(ctx)...)
	if getMetaBool(ctx, metaKeyCurrentMsgHasText) {
		return frames
	}
	contentIdx := 0
	if getMetaBool(ctx, metaKeyCurrentMsgHasRefuse) {
		contentIdx = 1
	}
	setMetaBool(ctx, metaKeyCurrentMsgHasText, true)
	setMetaInt(ctx, metaKeyCurrentMsgTextIdx, contentIdx)

	frames = append(frames, makeSSEFrame(ctx, "response.content_part.added", map[string]any{
		"item_id":       ctx.MessageItemID,
		"output_index":  ctx.MessageOutputIndex,
		"content_index": contentIdx,
		"part": map[string]any{
			"type":        "output_text",
			"text":        "",
			"annotations": []any{},
		},
	}))
	return frames
}

func ensureMessageRefusalPartOpen(ctx *ir.StreamContext) []ir.SSEFrame {
	var frames []ir.SSEFrame
	frames = append(frames, ensureMessageOpen(ctx)...)
	if getMetaBool(ctx, metaKeyCurrentMsgHasRefuse) {
		return frames
	}
	contentIdx := 0
	if getMetaBool(ctx, metaKeyCurrentMsgHasText) {
		contentIdx = 1
	}
	setMetaBool(ctx, metaKeyCurrentMsgHasRefuse, true)
	setMetaInt(ctx, metaKeyCurrentMsgRefuseIdx, contentIdx)

	frames = append(frames, makeSSEFrame(ctx, "response.content_part.added", map[string]any{
		"item_id":       ctx.MessageItemID,
		"output_index":  ctx.MessageOutputIndex,
		"content_index": contentIdx,
		"part": map[string]any{
			"type":    "refusal",
			"refusal": "",
		},
	}))
	return frames
}

func closeOpenMessageItem(ctx *ir.StreamContext) []ir.SSEFrame {
	return closeOpenMessageItemWithStatus(ctx, "completed")
}

func closeOpenMessageItemWithStatus(ctx *ir.StreamContext, itemStatus string) []ir.SSEFrame {
	if !ctx.MessageItemStarted || ctx.MessageItemDone {
		return nil
	}
	if itemStatus == "" {
		itemStatus = "completed"
	}
	var frames []ir.SSEFrame
	type indexedContent struct {
		idx  int
		part map[string]any
	}
	var parts []indexedContent

	if getMetaBool(ctx, metaKeyCurrentMsgHasText) {
		textIdx := getMetaInt(ctx, metaKeyCurrentMsgTextIdx)
		fullText := getMetaString(ctx, metaKeyCurrentMsgText)
		part := map[string]any{
			"type":        "output_text",
			"text":        fullText,
			"annotations": []any{},
		}
		frames = append(frames,
			makeSSEFrame(ctx, "response.output_text.done", map[string]any{
				"item_id":       ctx.MessageItemID,
				"output_index":  ctx.MessageOutputIndex,
				"content_index": textIdx,
				"text":          fullText,
			}),
			makeSSEFrame(ctx, "response.content_part.done", map[string]any{
				"item_id":       ctx.MessageItemID,
				"output_index":  ctx.MessageOutputIndex,
				"content_index": textIdx,
				"part":          part,
			}),
		)
		parts = append(parts, indexedContent{idx: textIdx, part: part})
	}

	if getMetaBool(ctx, metaKeyCurrentMsgHasRefuse) {
		refIdx := getMetaInt(ctx, metaKeyCurrentMsgRefuseIdx)
		fullRef := getMetaString(ctx, metaKeyCurrentMsgRefusal)
		part := map[string]any{
			"type":    "refusal",
			"refusal": fullRef,
		}
		frames = append(frames,
			makeSSEFrame(ctx, "response.refusal.done", map[string]any{
				"item_id":       ctx.MessageItemID,
				"output_index":  ctx.MessageOutputIndex,
				"content_index": refIdx,
				"refusal":       fullRef,
			}),
			makeSSEFrame(ctx, "response.content_part.done", map[string]any{
				"item_id":       ctx.MessageItemID,
				"output_index":  ctx.MessageOutputIndex,
				"content_index": refIdx,
				"part":          part,
			}),
		)
		parts = append(parts, indexedContent{idx: refIdx, part: part})
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].idx < parts[j].idx })
	content := make([]any, 0, len(parts))
	for _, p := range parts {
		content = append(content, p.part)
	}

	completedItem := map[string]any{
		"id":      ctx.MessageItemID,
		"type":    "message",
		"status":  itemStatus,
		"role":    "assistant",
		"content": content,
	}
	frames = append(frames, makeSSEFrame(ctx, "response.output_item.done", map[string]any{
		"output_index": ctx.MessageOutputIndex,
		"item":         completedItem,
	}))

	recordCompletedOutput(ctx, ctx.MessageOutputIndex, completedItem)
	ctx.MessageItemDone = true
	return frames
}

func openOrGetToolCall(ctx *ir.StreamContext, ev ir.StreamEvent) (*ir.TrackedToolCall, []ir.SSEFrame) {
	callID := ev.ToolCallID
	if callID == "" {
		callID = ir.SynthesizeToolCallID("call")
	}
	kind := ev.ToolKind
	if kind == "" {
		kind = "function"
	}
	fcID := normalizeResponsesToolItemID(ev.ToolCallItemID, callID, kind, ev.ToolCallIndex)

	regIdx := ev.ToolCallIndex
	if regIdx < 0 {
		regIdx = -1
	} else if callID != "" {
		if existing := ctx.ToolCallsByIndex[regIdx]; existing != nil && existing.ID != "" && existing.ID != callID {
			regIdx = -1
		}
	}

	tc, _ := ctx.RegisterToolCall(regIdx, callID, fcID, ev.ToolName, kind, ev.ThoughtSignature)
	if ev.BlockIndex >= 0 && tc.BlockIndex < 0 {
		tc.BlockIndex = ev.BlockIndex
	}
	tc.ItemID = normalizeResponsesToolItemID(tc.ItemID, tc.ID, tc.Kind, tc.Index)
	if tc.Started {
		return tc, nil
	}

	outIdx := ctx.NextOutputIndex
	ctx.NextOutputIndex++
	tc.OutputIndex = outIdx
	tc.Started = true
	tc.Closed = false

	var item map[string]any
	if tc.Kind == "custom" {
		item = map[string]any{
			"id":      tc.ItemID,
			"type":    "custom_tool_call",
			"call_id": tc.ID,
			"name":    tc.Name,
			"input":   "",
			"status":  "in_progress",
		}
	} else {
		item = map[string]any{
			"id":        tc.ItemID,
			"type":      "function_call",
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": "",
			"status":    "in_progress",
		}
	}

	return tc, []ir.SSEFrame{
		makeSSEFrame(ctx, "response.output_item.added", map[string]any{
			"output_index": outIdx,
			"item":         item,
		}),
	}
}

func makeToolCallDeltaFrame(ctx *ir.StreamContext, tc *ir.TrackedToolCall, delta string) ir.SSEFrame {
	evType := "response.function_call_arguments.delta"
	if tc.Kind == "custom" {
		evType = "response.custom_tool_call_input.delta"
	}
	return makeSSEFrame(ctx, evType, map[string]any{
		"item_id":      tc.ItemID,
		"output_index": tc.OutputIndex,
		"delta":        delta,
	})
}

func closeOpenToolCalls(ctx *ir.StreamContext) []ir.SSEFrame {
	return closeOpenToolCallsWithStatus(ctx, "completed")
}

func closeOpenToolCallsWithStatus(ctx *ir.StreamContext, itemStatus string) []ir.SSEFrame {
	var frames []ir.SSEFrame
	for _, tc := range ctx.ToolCallOrder {
		frames = append(frames, closeSingleToolCall(ctx, tc, itemStatus)...)
	}
	return frames
}

func closeSingleToolCall(ctx *ir.StreamContext, tc *ir.TrackedToolCall, itemStatus string) []ir.SSEFrame {
	if tc == nil || !tc.Started || tc.Closed {
		return nil
	}
	if itemStatus == "" {
		itemStatus = "completed"
	}
	fullArgs := tc.Args.String()
	var doneFrame ir.SSEFrame
	var completedItem map[string]any

	if tc.Kind == "custom" {
		doneFrame = makeSSEFrame(ctx, "response.custom_tool_call_input.done", map[string]any{
			"item_id":      tc.ItemID,
			"output_index": tc.OutputIndex,
			"input":        fullArgs,
		})
		completedItem = map[string]any{
			"id":      tc.ItemID,
			"type":    "custom_tool_call",
			"call_id": tc.ID,
			"name":    tc.Name,
			"input":   fullArgs,
			"status":  itemStatus,
		}
	} else {
		if strings.TrimSpace(fullArgs) == "" {
			fullArgs = "{}"
		}
		doneFrame = makeSSEFrame(ctx, "response.function_call_arguments.done", map[string]any{
			"item_id":      tc.ItemID,
			"output_index": tc.OutputIndex,
			"arguments":    fullArgs,
		})
		completedItem = map[string]any{
			"id":        tc.ItemID,
			"type":      "function_call",
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": fullArgs,
			"status":    itemStatus,
		}
	}

	itemDoneFrame := makeSSEFrame(ctx, "response.output_item.done", map[string]any{
		"output_index": tc.OutputIndex,
		"item":         completedItem,
	})

	recordCompletedOutput(ctx, tc.OutputIndex, completedItem)
	tc.Closed = true
	return []ir.SSEFrame{doneFrame, itemDoneFrame}
}

func makeSSEFrame(ctx *ir.StreamContext, eventType string, fields map[string]any) ir.SSEFrame {
	payload := make(map[string]any, len(fields)+2)
	for k, v := range fields {
		payload[k] = v
	}
	// Always stamp canonical SSE event discriminator & monotonic sequence_number last so fields cannot clobber them.
	payload["type"] = eventType
	payload["sequence_number"] = ctx.NextSeq()
	data, _ := json.Marshal(payload)
	return ir.SSEFrame{
		Event: eventType,
		Data:  data,
	}
}

func rewritePassthroughSeq(ctx *ir.StreamContext, raw json.RawMessage) []byte {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return raw
	}
	obj["sequence_number"] = ctx.NextSeq()
	out, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return out
}

func recordCompletedOutput(ctx *ir.StreamContext, outIdx int, item any) {
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	list, _ := ctx.Metadata[metaKeyCompletedOutputs].([]responsesCompletedOutput)
	list = append(list, responsesCompletedOutput{
		OutputIndex: outIdx,
		Item:        item,
	})
	ctx.Metadata[metaKeyCompletedOutputs] = list
}

func collectCompletedOutputs(ctx *ir.StreamContext) []any {
	if ctx.Metadata == nil {
		return []any{}
	}
	list, _ := ctx.Metadata[metaKeyCompletedOutputs].([]responsesCompletedOutput)
	if len(list) == 0 {
		return []any{}
	}
	cp := make([]responsesCompletedOutput, len(list))
	copy(cp, list)
	sort.SliceStable(cp, func(i, j int) bool {
		return cp[i].OutputIndex < cp[j].OutputIndex
	})
	out := make([]any, 0, len(cp))
	for _, entry := range cp {
		out = append(out, entry.Item)
	}
	return out
}

func setMetaString(ctx *ir.StreamContext, key, val string) {
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	if b, ok := ctx.Metadata[key].(*strings.Builder); ok && b != nil {
		b.Reset()
		b.WriteString(val)
		return
	}
	var b strings.Builder
	b.WriteString(val)
	ctx.Metadata[key] = &b
}

func appendMetaString(ctx *ir.StreamContext, key, delta string) {
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	if b, ok := ctx.Metadata[key].(*strings.Builder); ok && b != nil {
		b.WriteString(delta)
		return
	}
	var b strings.Builder
	if prev, ok := ctx.Metadata[key].(string); ok {
		b.WriteString(prev)
	}
	b.WriteString(delta)
	ctx.Metadata[key] = &b
}

func getMetaString(ctx *ir.StreamContext, key string) string {
	if ctx.Metadata == nil {
		return ""
	}
	if b, ok := ctx.Metadata[key].(*strings.Builder); ok && b != nil {
		return b.String()
	}
	s, _ := ctx.Metadata[key].(string)
	return s
}

func setMetaBool(ctx *ir.StreamContext, key string, val bool) {
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	ctx.Metadata[key] = val
}

func getMetaBool(ctx *ir.StreamContext, key string) bool {
	if ctx.Metadata == nil {
		return false
	}
	b, _ := ctx.Metadata[key].(bool)
	return b
}

func setMetaInt(ctx *ir.StreamContext, key string, val int) {
	if ctx.Metadata == nil {
		ctx.Metadata = make(map[string]any)
	}
	ctx.Metadata[key] = val
}

func getMetaInt(ctx *ir.StreamContext, key string) int {
	if ctx.Metadata == nil {
		return 0
	}
	v, _ := ctx.Metadata[key].(int)
	return v
}
