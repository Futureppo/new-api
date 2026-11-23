package openaichat

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relay/ir"
)

type chatStreamChunkWire struct {
	ID                string                 `json:"id,omitempty"`
	Object            string                 `json:"object,omitempty"`
	Created           flexInt64              `json:"created,omitempty"`
	Model             string                 `json:"model,omitempty"`
	SystemFingerprint string                 `json:"system_fingerprint,omitempty"`
	ServiceTier       string                 `json:"service_tier,omitempty"`
	Choices           []chatStreamChoiceWire `json:"choices,omitempty"`
	Usage             *chatUsageWire         `json:"usage,omitempty"`
	Error             *chatErrorPayload      `json:"error,omitempty"`
}

type chatStreamChoiceWire struct {
	Index        int                 `json:"index"`
	Delta        chatStreamDeltaWire `json:"delta"`
	FinishReason *string             `json:"finish_reason"`
	Logprobs     json.RawMessage     `json:"logprobs,omitempty"`
}

type chatStreamDeltaWire struct {
	Role             string                   `json:"role,omitempty"`
	Content          *string                  `json:"content,omitempty"`
	ReasoningContent *string                  `json:"reasoning_content,omitempty"`
	Reasoning        *string                  `json:"reasoning,omitempty"`
	Refusal          *string                  `json:"refusal,omitempty"`
	ToolCalls        []chatStreamToolCallWire `json:"tool_calls,omitempty"`
	FunctionCall     *chatFunctionCall        `json:"function_call,omitempty"`
}

type chatStreamToolCallWire struct {
	Index    *int              `json:"index,omitempty"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function *chatFunctionCall `json:"function,omitempty"`
	Custom   *chatCustomCall   `json:"custom,omitempty"`
}

type outStreamChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []outStreamChoice `json:"choices"`
	Usage   *chatUsageWire    `json:"usage,omitempty"`
}

type outStreamChoice struct {
	Index        int            `json:"index"`
	Delta        outStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type outStreamDelta struct {
	Role             string              `json:"role,omitempty"`
	Content          *string             `json:"content,omitempty"`
	ReasoningContent *string             `json:"reasoning_content,omitempty"`
	Refusal          *string             `json:"refusal,omitempty"`
	ToolCalls        []outStreamToolCall `json:"tool_calls,omitempty"`
}

type outStreamToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function *outStreamToolFunc   `json:"function,omitempty"`
	Custom   *outStreamToolCustom `json:"custom,omitempty"`
}

type outStreamToolFunc struct {
	Name      string  `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}

type outStreamToolCustom struct {
	Name  string  `json:"name,omitempty"`
	Input *string `json:"input,omitempty"`
}

// DecodeStreamEvent parses one OpenAI Chat Completions SSE chunk into zero or more IR StreamEvents.
func (c *Codec) DecodeStreamEvent(_ string, rawData []byte, ctx *ir.StreamContext) ([]ir.StreamEvent, error) {
	trimmed := bytes.TrimSpace(rawData)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return []ir.StreamEvent{{Type: ir.EventStreamEnd}}, nil
	}

	var chunk chatStreamChunkWire
	if err := json.Unmarshal(trimmed, &chunk); err != nil {
		return nil, fmt.Errorf("openaichat: decode stream chunk: %w", err)
	}

	if chunk.Error != nil {
		return []ir.StreamEvent{{
			Type:         ir.EventError,
			ErrorMessage: chunk.Error.Message,
			ErrorType:    chunk.Error.Type,
			ErrorCode:    chunk.Error.codeString(),
		}}, nil
	}

	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatUnknown)
	}

	var events []ir.StreamEvent

	// Populate envelope metadata and emit EventStreamStart once on first chunk.
	if chunk.ID != "" {
		ctx.ResponseID = chunk.ID
	}
	if chunk.Model != "" {
		ctx.Model = chunk.Model
	}
	if int64(chunk.Created) > 0 {
		ctx.Created = int64(chunk.Created)
	}

	if !ctx.Started {
		ctx.Started = true
		events = append(events, ir.StreamEvent{
			Type:       ir.EventStreamStart,
			ResponseID: ctx.ResponseID,
			Model:      ctx.Model,
			Created:    ctx.Created,
		})
	}

	for _, ch := range chunk.Choices {
		// Reasoning delta (reasoning_content takes precedence over reasoning).
		if rText := extractReasoningText(ch.Delta.ReasoningContent, ch.Delta.Reasoning); rText != "" {
			events = append(events, ir.StreamEvent{
				Type:         ir.EventReasoningDelta,
				ChoiceIndex:  ch.Index,
				Reasoning:    rText,
				OriginFormat: ir.FormatOpenAIChat,
			})
		}

		// Text delta.
		if ch.Delta.Content != nil && *ch.Delta.Content != "" {
			events = append(events, ir.StreamEvent{
				Type:        ir.EventTextDelta,
				ChoiceIndex: ch.Index,
				Text:        *ch.Delta.Content,
			})
		}

		// Refusal delta.
		if ch.Delta.Refusal != nil && *ch.Delta.Refusal != "" {
			events = append(events, ir.StreamEvent{
				Type:        ir.EventRefusalDelta,
				ChoiceIndex: ch.Index,
				Refusal:     *ch.Delta.Refusal,
			})
		}

		// Tool call deltas.
		for _, tc := range ch.Delta.ToolCalls {
			events = append(events, decodeStreamToolCallDelta(ch.Index, tc, ctx)...)
		}

		// Legacy function_call streaming fallback.
		if len(ch.Delta.ToolCalls) == 0 && ch.Delta.FunctionCall != nil {
			idx := 0
			legacyTC := chatStreamToolCallWire{
				Index:    &idx,
				Type:     "function",
				Function: ch.Delta.FunctionCall,
			}
			if ch.Delta.FunctionCall.Name != "" {
				legacyTC.ID = ir.SynthesizeToolCallID("call_legacy")
			}
			events = append(events, decodeStreamToolCallDelta(ch.Index, legacyTC, ctx)...)
		}

		// Finish reason.
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			rawFinish := *ch.FinishReason
			events = append(events, ir.StreamEvent{
				Type:            ir.EventFinish,
				ChoiceIndex:     ch.Index,
				FinishReason:    normalizeFinishReason(rawFinish),
				RawFinishReason: rawFinish,
			})
		}
	}

	if chunk.Usage != nil {
		u := decodeUsage(chunk.Usage)
		ctx.MergeUsage(u)
		events = append(events, ir.StreamEvent{
			Type:  ir.EventUsage,
			Usage: u,
		})
	}

	return events, nil
}

func decodeStreamToolCallDelta(choiceIdx int, tc chatStreamToolCallWire, ctx *ir.StreamContext) []ir.StreamEvent {
	var events []ir.StreamEvent

	idx := -1
	if tc.Index != nil {
		idx = *tc.Index
	}

	var kind, name, args string
	if tc.Type == "custom" || tc.Custom != nil {
		kind = "custom"
		if tc.Custom != nil {
			name = tc.Custom.Name
			args = tc.Custom.Input
		}
	} else {
		kind = tc.Type
		if tc.Function != nil {
			name = tc.Function.Name
			args = tc.Function.Arguments
		}
	}

	if tc.ID != "" || name != "" {
		regIdx := idx
		if tc.ID != "" && regIdx >= 0 {
			if existing := ctx.ToolCallsByIndex[regIdx]; existing != nil && existing.ID != "" && existing.ID != tc.ID {
				regIdx = -1
			}
		}
		if kind == "" {
			kind = "function"
		}
		var prevName string
		if regIdx >= 0 && tc.ID == "" {
			if existing := ctx.ToolCallsByIndex[regIdx]; existing != nil {
				prevName = existing.Name
			}
		}
		tracked, created := ctx.RegisterToolCall(regIdx, tc.ID, "", name, kind, "")
		if created || !tracked.Started {
			tracked.Started = true
			events = append(events, ir.StreamEvent{
				Type:          ir.EventToolCallStart,
				ChoiceIndex:   choiceIdx,
				ToolCallIndex: tracked.Index,
				ToolCallID:    tracked.ID,
				ToolName:      tracked.Name,
				ToolKind:      tracked.Kind,
			})
		} else if name != "" && prevName == "" {
			// Split id/name chunks: propagate late-arriving function name via delta.
			events = append(events, ir.StreamEvent{
				Type:          ir.EventToolCallDelta,
				ChoiceIndex:   choiceIdx,
				ToolCallIndex: tracked.Index,
				ToolCallID:    tracked.ID,
				ToolName:      tracked.Name,
				ToolKind:      tracked.Kind,
				ToolArguments: args,
			})
			if args != "" {
				tracked.Args.WriteString(args)
			}
			return events
		}
		if args != "" {
			tracked.Args.WriteString(args)
			events = append(events, ir.StreamEvent{
				Type:          ir.EventToolCallDelta,
				ChoiceIndex:   choiceIdx,
				ToolCallIndex: tracked.Index,
				ToolCallID:    tracked.ID,
				ToolName:      tracked.Name,
				ToolKind:      tracked.Kind,
				ToolArguments: args,
			})
		}
		return events
	}

	if args != "" {
		tracked := ctx.LookupToolCall(idx, "")
		callIdx := idx
		callID := ""
		callName := ""
		callKind := kind
		if tracked != nil {
			callIdx = tracked.Index
			callID = tracked.ID
			callName = tracked.Name
			if tracked.Kind != "" {
				callKind = tracked.Kind
			}
			tracked.Args.WriteString(args)
		} else if callIdx < 0 {
			callIdx = 0
		}
		if callKind == "" {
			callKind = "function"
		}
		events = append(events, ir.StreamEvent{
			Type:          ir.EventToolCallDelta,
			ChoiceIndex:   choiceIdx,
			ToolCallIndex: callIdx,
			ToolCallID:    callID,
			ToolName:      callName,
			ToolKind:      callKind,
			ToolArguments: args,
		})
	}

	return events
}

// EncodeStreamEvent encodes one normalized IR StreamEvent into OpenAI Chat Completions SSE frames.
func (c *Codec) EncodeStreamEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatUnknown, ir.FormatOpenAIChat)
	}
	if ctx.Ended {
		return nil, nil
	}

	syncStreamEnvelope(ev, ctx)

	switch ev.Type {
	case ir.EventStreamStart:
		if ctx.Started {
			return nil, nil
		}
		ctx.Started = true
		ctx.EnsureEnvelope("chatcmpl")
		empty := ""
		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:    "assistant",
					Content: &empty,
				},
			}},
		})

	case ir.EventTextDelta:
		if ev.Text == "" {
			return nil, nil
		}
		role := startStreamIfNeeded(ctx)
		s := ev.Text
		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:    role,
					Content: &s,
				},
			}},
		})

	case ir.EventReasoningDelta:
		if ev.Reasoning == "" {
			return nil, nil
		}
		role := startStreamIfNeeded(ctx)
		s := ev.Reasoning
		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:             role,
					ReasoningContent: &s,
				},
			}},
		})

	case ir.EventRefusalDelta:
		if ev.Refusal == "" {
			return nil, nil
		}
		role := startStreamIfNeeded(ctx)
		s := ev.Refusal
		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:    role,
					Refusal: &s,
				},
			}},
		})

	case ir.EventImage:
		md := formatImageMarkdown(ev.Image)
		if md == "" {
			return nil, nil
		}
		role := startStreamIfNeeded(ctx)
		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:    role,
					Content: &md,
				},
			}},
		})

	case ir.EventToolCallStart:
		role := startStreamIfNeeded(ctx)
		regIdx := ev.ToolCallIndex
		if regIdx < 0 {
			regIdx = -1
		} else if ev.ToolCallID != "" {
			if existing := ctx.ToolCallsByIndex[regIdx]; existing != nil && existing.ID != "" && existing.ID != ev.ToolCallID {
				regIdx = -1
			}
		}
		kind := ev.ToolKind
		if kind == "" {
			kind = "function"
		}
		tracked, created := ctx.RegisterToolCall(regIdx, ev.ToolCallID, ev.ToolCallItemID, ev.ToolName, kind, ev.ThoughtSignature)
		if ev.BlockIndex >= 0 && tracked.BlockIndex < 0 {
			tracked.BlockIndex = ev.BlockIndex
		}
		if !created && tracked.Started {
			// Prevent duplicate start chunk that would corrupt concatenated function.name.
			if ev.ToolArguments == "" {
				return nil, nil
			}
			tracked.Args.WriteString(ev.ToolArguments)
			args := ev.ToolArguments
			var deltaTC outStreamToolCall
			if tracked.Kind == "custom" {
				deltaTC = outStreamToolCall{Index: tracked.Index, Custom: &outStreamToolCustom{Input: &args}}
			} else {
				deltaTC = outStreamToolCall{Index: tracked.Index, Function: &outStreamToolFunc{Arguments: &args}}
			}
			return marshalChunkFrame(outStreamChunk{
				ID:      ctx.ResponseID,
				Object:  "chat.completion.chunk",
				Created: ctx.Created,
				Model:   ctx.Model,
				Choices: []outStreamChoice{{
					Index: ev.ChoiceIndex,
					Delta: outStreamDelta{Role: role, ToolCalls: []outStreamToolCall{deltaTC}},
				}},
			})
		}
		tracked.Started = true

		initArgs := ev.ToolArguments
		if initArgs != "" {
			tracked.Args.WriteString(initArgs)
		}

		var outTC outStreamToolCall
		if tracked.Kind == "custom" {
			outTC = outStreamToolCall{
				Index: tracked.Index,
				ID:    tracked.ID,
				Type:  "custom",
				Custom: &outStreamToolCustom{
					Name:  tracked.Name,
					Input: &initArgs,
				},
			}
		} else {
			outTC = outStreamToolCall{
				Index: tracked.Index,
				ID:    tracked.ID,
				Type:  "function",
				Function: &outStreamToolFunc{
					Name:      tracked.Name,
					Arguments: &initArgs,
				},
			}
		}

		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:      role,
					ToolCalls: []outStreamToolCall{outTC},
				},
			}},
		})

	case ir.EventToolCallDelta:
		if ev.ToolArguments == "" && ev.ToolName == "" {
			return nil, nil
		}
		role := startStreamIfNeeded(ctx)
		tracked := ctx.LookupToolCall(ev.ToolCallIndex, ev.ToolCallID)
		if tracked == nil && (ev.ToolCallID != "" || ev.ToolName != "") {
			kind := ev.ToolKind
			if kind == "" {
				kind = "function"
			}
			tracked, _ = ctx.RegisterToolCall(ev.ToolCallIndex, ev.ToolCallID, ev.ToolCallItemID, ev.ToolName, kind, ev.ThoughtSignature)
			tracked.Started = true
		}
		if tracked != nil && tracked.Closed {
			return nil, nil
		}
		idx := ev.ToolCallIndex
		kind := ev.ToolKind
		emitName := ""
		if tracked != nil {
			idx = tracked.Index
			if tracked.Kind != "" {
				kind = tracked.Kind
			}
			if ev.ToolName != "" && tracked.Name == "" {
				tracked.Name = ev.ToolName
				emitName = ev.ToolName
			}
			if ev.ToolArguments != "" {
				tracked.Args.WriteString(ev.ToolArguments)
			}
		}
		if idx < 0 {
			idx = 0
		}

		args := ev.ToolArguments
		var argsPtr *string
		if args != "" {
			argsPtr = &args
		}
		if argsPtr == nil && emitName == "" {
			return nil, nil
		}
		var outTC outStreamToolCall
		if kind == "custom" {
			outTC = outStreamToolCall{
				Index: idx,
				Custom: &outStreamToolCustom{
					Name:  emitName,
					Input: argsPtr,
				},
			}
		} else {
			outTC = outStreamToolCall{
				Index: idx,
				Function: &outStreamToolFunc{
					Name:      emitName,
					Arguments: argsPtr,
				},
			}
		}

		return marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index: ev.ChoiceIndex,
				Delta: outStreamDelta{
					Role:      role,
					ToolCalls: []outStreamToolCall{outTC},
				},
			}},
		})

	case ir.EventContentBlockEnd:
		if ev.BlockType == ir.PartToolCall {
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
			if tc != nil && tc.Started && !tc.Closed {
				tc.Closed = true
				if tc.Kind != "custom" && tc.Args.Len() == 0 {
					emptyObj := "{}"
					tc.Args.WriteString(emptyObj)
					ctx.EnsureEnvelope("chatcmpl")
					return marshalChunkFrame(outStreamChunk{
						ID:      ctx.ResponseID,
						Object:  "chat.completion.chunk",
						Created: ctx.Created,
						Model:   ctx.Model,
						Choices: []outStreamChoice{{
							Index: ev.ChoiceIndex,
							Delta: outStreamDelta{
								ToolCalls: []outStreamToolCall{{
									Index: tc.Index,
									Function: &outStreamToolFunc{
										Arguments: &emptyObj,
									},
								}},
							},
						}},
					})
				}
			}
		}
		return nil, nil

	case ir.EventFinish:
		if !ctx.Started {
			ctx.Started = true
			ctx.EnsureEnvelope("chatcmpl")
		}
		fr := ev.FinishReason
		if fr == "" {
			if len(ctx.ToolCallOrder) > 0 {
				fr = ir.FinishToolCalls
			} else {
				fr = ir.FinishStop
			}
		}
		frCopy := fr
		ctx.PendingFinish = &frCopy
		ctx.PendingRawFinish = ev.RawFinishReason
		if ev.Usage != nil {
			ctx.MergeUsage(ev.Usage)
		}
		if ctx.ChoiceFinishEmitted == nil {
			ctx.ChoiceFinishEmitted = make(map[int]bool)
		}
		if ctx.ChoiceFinishEmitted[ev.ChoiceIndex] {
			return nil, nil
		}
		var frames []ir.SSEFrame
		if emptyFrames, err := emitEmptyToolArgDeltas(ctx, ev.ChoiceIndex); err != nil {
			return nil, err
		} else {
			frames = append(frames, emptyFrames...)
		}
		wireFR := encodeFinishReason(fr)
		ctx.ChoiceFinishEmitted[ev.ChoiceIndex] = true
		ctx.FinishEmitted = true
		finishFrames, err := marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index:        ev.ChoiceIndex,
				Delta:        outStreamDelta{},
				FinishReason: &wireFR,
			}},
		})
		if err != nil {
			return frames, err
		}
		frames = append(frames, finishFrames...)
		return frames, nil

	case ir.EventUsage:
		if ev.Usage != nil {
			ctx.MergeUsage(ev.Usage)
		}
		if ctx.FinishEmitted && !ctx.UsageEmitted && ctx.LastUsage != nil && ctx.GetBoolOption("include_usage", true) {
			ctx.EnsureEnvelope("chatcmpl")
			ctx.UsageEmitted = true
			return marshalChunkFrame(outStreamChunk{
				ID:      ctx.ResponseID,
				Object:  "chat.completion.chunk",
				Created: ctx.Created,
				Model:   ctx.Model,
				Choices: []outStreamChoice{},
				Usage:   encodeUsage(ctx.LastUsage),
			})
		}
		return nil, nil

	case ir.EventStreamEnd:
		return c.FlushStream(ctx)

	case ir.EventError:
		errType := ev.ErrorType
		if errType == "" {
			errType = "server_error"
		}
		errMsg := ev.ErrorMessage
		if errMsg == "" {
			errMsg = "stream error"
		}
		payload := map[string]any{
			"error": map[string]any{
				"message": errMsg,
				"type":    errType,
				"code":    ev.ErrorCode,
			},
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		// Terminate stream so FlushStream never fabricates finish_reason:"stop" or [DONE] after an error.
		ctx.FinishEmitted = true
		ctx.Ended = true
		return []ir.SSEFrame{{Data: data}}, nil

	case ir.EventPassthrough:
		if ctx.ShouldPreserveMetadata(ev.OriginFormat) && len(ev.PassthroughData) > 0 {
			return []ir.SSEFrame{{
				Event: ev.PassthroughEvent,
				Data:  ev.PassthroughData,
			}}, nil
		}
		return nil, nil

	default:
		return nil, nil
	}
}

// FlushStream emits any pending finish chunk, final usage chunk (when enabled), and the terminal [DONE] marker.
func (c *Codec) FlushStream(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil {
		return []ir.SSEFrame{{Data: []byte("[DONE]")}}, nil
	}
	if ctx.Ended {
		return nil, nil
	}

	var frames []ir.SSEFrame

	if ctx.Started && !ctx.FinishEmitted {
		ctx.EnsureEnvelope("chatcmpl")
		if emptyFrames, err := emitEmptyToolArgDeltas(ctx, 0); err != nil {
			return frames, err
		} else {
			frames = append(frames, emptyFrames...)
		}
		fr := ir.FinishStop
		if ctx.PendingFinish != nil && *ctx.PendingFinish != "" {
			fr = *ctx.PendingFinish
		} else if len(ctx.ToolCallOrder) > 0 {
			fr = ir.FinishToolCalls
		}
		wireFR := encodeFinishReason(fr)
		finishFrames, err := marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{{
				Index:        0,
				Delta:        outStreamDelta{},
				FinishReason: &wireFR,
			}},
		})
		if err != nil {
			return frames, err
		}
		frames = append(frames, finishFrames...)
		ctx.FinishEmitted = true
	}

	if ctx.LastUsage != nil && !ctx.UsageEmitted && ctx.GetBoolOption("include_usage", true) {
		ctx.EnsureEnvelope("chatcmpl")
		usageFrames, err := marshalChunkFrame(outStreamChunk{
			ID:      ctx.ResponseID,
			Object:  "chat.completion.chunk",
			Created: ctx.Created,
			Model:   ctx.Model,
			Choices: []outStreamChoice{},
			Usage:   encodeUsage(ctx.LastUsage),
		})
		if err != nil {
			return frames, err
		}
		frames = append(frames, usageFrames...)
		ctx.UsageEmitted = true
	}

	if ctx.Started || ctx.FinishEmitted || ctx.UsageEmitted {
		frames = append(frames, ir.SSEFrame{Data: []byte("[DONE]")})
	}
	ctx.Ended = true
	return frames, nil
}

func emitEmptyToolArgDeltas(ctx *ir.StreamContext, choiceIndex int) ([]ir.SSEFrame, error) {
	var frames []ir.SSEFrame
	for _, tc := range ctx.ToolCallOrder {
		if tc == nil || !tc.Started || tc.Closed {
			continue
		}
		tc.Closed = true
		if tc.Kind != "custom" && tc.Args.Len() == 0 {
			emptyObj := "{}"
			tc.Args.WriteString(emptyObj)
			chunkFrames, err := marshalChunkFrame(outStreamChunk{
				ID:      ctx.ResponseID,
				Object:  "chat.completion.chunk",
				Created: ctx.Created,
				Model:   ctx.Model,
				Choices: []outStreamChoice{{
					Index: choiceIndex,
					Delta: outStreamDelta{
						ToolCalls: []outStreamToolCall{{
							Index: tc.Index,
							Function: &outStreamToolFunc{
								Arguments: &emptyObj,
							},
						}},
					},
				}},
			})
			if err != nil {
				return frames, err
			}
			frames = append(frames, chunkFrames...)
		}
	}
	return frames, nil
}

func syncStreamEnvelope(ev ir.StreamEvent, ctx *ir.StreamContext) {
	if !ctx.Started {
		if ev.ResponseID != "" {
			ctx.ResponseID = ev.ResponseID
		}
		if ev.Model != "" {
			ctx.Model = ev.Model
		}
		if ev.Created > 0 {
			ctx.Created = ev.Created
		}
		return
	}
	if ev.ResponseID != "" && ctx.ResponseID == "" {
		ctx.ResponseID = ev.ResponseID
	}
	if ev.Model != "" && ctx.Model == "" {
		ctx.Model = ev.Model
	}
	if ev.Created > 0 && ctx.Created == 0 {
		ctx.Created = ev.Created
	}
}

func startStreamIfNeeded(ctx *ir.StreamContext) string {
	if ctx.Started {
		return ""
	}
	ctx.Started = true
	ctx.EnsureEnvelope("chatcmpl")
	return "assistant"
}

func marshalChunkFrame(chunk outStreamChunk) ([]ir.SSEFrame, error) {
	data, err := json.Marshal(chunk)
	if err != nil {
		return nil, fmt.Errorf("openaichat: encode stream chunk: %w", err)
	}
	return []ir.SSEFrame{{Data: data}}, nil
}
