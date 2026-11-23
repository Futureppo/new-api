package ir

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxOutputTextEstimateBytes = 2 * 1024 * 1024

// PipelineConfig configures a Pipeline translation execution.
type PipelineConfig struct {
	Registry     *Registry
	Bridge       *TurnBridge
	BridgeScope  string
	SourceFormat Format
	TargetFormat Format
	SourceShim   *Shim
	TargetShim   *Shim
	MetadataMode MetadataMode
	Options      map[string]any
}

// Pipeline orchestrates bidirectional Source <-> IR <-> Target conversions,
// declarative shims, and cross-turn metadata bridging.
type Pipeline struct {
	registry    *Registry
	bridge      *TurnBridge
	bridgeScope string
	source      Codec
	target      Codec
	sourceShim  *Shim
	targetShim  *Shim
	mode        MetadataMode
	options     map[string]any
}

// NewPipeline validates formats and builds a reusable Pipeline.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	reg := cfg.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	srcFmt := cfg.SourceFormat
	if srcFmt == FormatUnknown && cfg.SourceShim != nil {
		srcFmt = cfg.SourceShim.BaseFormat
	}
	dstFmt := cfg.TargetFormat
	if dstFmt == FormatUnknown && cfg.TargetShim != nil {
		dstFmt = cfg.TargetShim.BaseFormat
	}
	srcCodec, err := reg.Get(srcFmt)
	if err != nil {
		return nil, fmt.Errorf("ir: source codec: %w", err)
	}
	dstCodec, err := reg.Get(dstFmt)
	if err != nil {
		return nil, fmt.Errorf("ir: target codec: %w", err)
	}
	mode := cfg.MetadataMode
	if mode == "" {
		if srcFmt == dstFmt {
			mode = MetadataPreserve
		} else {
			mode = MetadataStrip
		}
	}
	opts := make(map[string]any, len(cfg.Options))
	for k, v := range cfg.Options {
		opts[k] = v
	}
	return &Pipeline{
		registry:    reg,
		bridge:      cfg.Bridge,
		bridgeScope: cfg.BridgeScope,
		source:      srcCodec,
		target:      dstCodec,
		sourceShim:  cfg.SourceShim,
		targetShim:  cfg.TargetShim,
		mode:        mode,
		options:     opts,
	}, nil
}

// ConvertRequest translates a raw request body from SourceFormat to TargetFormat.
// It returns the target JSON bytes, the intermediate Request, and the ConversionContext (including warnings).
func (p *Pipeline) ConvertRequest(raw []byte) ([]byte, *Request, *ConversionContext, error) {
	ctx := NewConversionContext(p.source.Format(), p.target.Format())
	ctx.MetadataMode = p.mode
	ctx.BridgeScope = p.bridgeScope
	for k, v := range p.options {
		ctx.Options[k] = v
	}

	in := raw
	if p.sourceShim != nil && len(p.sourceShim.PreIRRequestTransforms) > 0 {
		var err error
		in, err = ApplyJSONTransforms(in, p.sourceShim.PreIRRequestTransforms, ctx)
		if err != nil {
			return nil, nil, ctx, fmt.Errorf("ir: source pre-IR request shim: %w", err)
		}
	}

	irReq, err := p.source.DecodeRequest(in, ctx)
	if err != nil {
		return nil, nil, ctx, fmt.Errorf("ir: decode %s request: %w", p.source.Format(), err)
	}

	if p.bridge != nil {
		loaded := p.bridge.LoadForRequest(p.bridgeScope, irReq)
		ctx.BridgeState = loaded
		loaded.InjectIntoRequest(irReq)
	}

	out, err := p.target.EncodeRequest(irReq, ctx)
	if err != nil {
		return nil, irReq, ctx, fmt.Errorf("ir: encode %s request: %w", p.target.Format(), err)
	}

	if p.targetShim != nil && len(p.targetShim.PostIRRequestTransforms) > 0 {
		out, err = ApplyJSONTransforms(out, p.targetShim.PostIRRequestTransforms, ctx)
		if err != nil {
			return nil, irReq, ctx, fmt.Errorf("ir: target post-IR request shim: %w", err)
		}
	}
	return out, irReq, ctx, nil
}

// ConvertResponse translates a non-streaming response body from TargetFormat back to SourceFormat.
func (p *Pipeline) ConvertResponse(raw []byte, fallbackModel string) ([]byte, *Response, *ConversionContext, error) {
	ctx := NewConversionContext(p.target.Format(), p.source.Format())
	ctx.MetadataMode = p.mode
	ctx.BridgeScope = p.bridgeScope
	for k, v := range p.options {
		ctx.Options[k] = v
	}

	in := raw
	if p.targetShim != nil && len(p.targetShim.PreIRResponseTransforms) > 0 {
		var err error
		in, err = ApplyJSONTransforms(in, p.targetShim.PreIRResponseTransforms, ctx)
		if err != nil {
			return nil, nil, ctx, fmt.Errorf("ir: target pre-IR response shim: %w", err)
		}
	}

	irResp, err := p.target.DecodeResponse(in, ctx)
	if err != nil {
		return nil, nil, ctx, fmt.Errorf("ir: decode %s response: %w", p.target.Format(), err)
	}
	if irResp.Model == "" {
		irResp.Model = fallbackModel
	}

	if p.bridge != nil {
		ctx.BridgeState.ExtractResponseBridge(irResp, p.target.Format())
		p.bridge.Save(p.bridgeScope, ctx.BridgeState)
	}

	out, err := p.source.EncodeResponse(irResp, ctx)
	if err != nil {
		return nil, irResp, ctx, fmt.Errorf("ir: encode %s response: %w", p.source.Format(), err)
	}

	if p.sourceShim != nil && len(p.sourceShim.PostIRResponseTransforms) > 0 {
		out, err = ApplyJSONTransforms(out, p.sourceShim.PostIRResponseTransforms, ctx)
		if err != nil {
			return nil, irResp, ctx, fmt.Errorf("ir: source post-IR response shim: %w", err)
		}
	}
	return out, irResp, ctx, nil
}

// StreamTranslator translates a single upstream SSE stream (in TargetFormat)
// into downstream SSE frames (in SourceFormat) with full stateful context tracking.
type StreamTranslator struct {
	pipeline         *Pipeline
	decodeCtx        *StreamContext
	encodeCtx        *StreamContext
	bridgePersisted  bool
	streamErr        *StreamEvent
	hasNonErrorEvent bool
}

// NewStreamTranslator initializes a stateful translator for one streaming response.
func (p *Pipeline) NewStreamTranslator(fallbackModel string) *StreamTranslator {
	decodeCtx := NewStreamContext(p.target.Format(), p.source.Format())
	decodeCtx.MetadataMode = p.mode
	decodeCtx.BridgeScope = p.bridgeScope
	decodeCtx.Model = fallbackModel
	for k, v := range p.options {
		decodeCtx.Options[k] = v
	}

	encodeCtx := NewStreamContext(p.target.Format(), p.source.Format())
	encodeCtx.MetadataMode = p.mode
	encodeCtx.BridgeScope = p.bridgeScope
	encodeCtx.Model = fallbackModel
	for k, v := range p.options {
		encodeCtx.Options[k] = v
	}

	return &StreamTranslator{
		pipeline:  p,
		decodeCtx: decodeCtx,
		encodeCtx: encodeCtx,
	}
}

// ProcessChunk ingests one upstream SSE event (or Bedrock JSON chunk) and returns zero or more downstream SSEFrames.
func (st *StreamTranslator) ProcessChunk(sseEvent string, rawData []byte) ([]SSEFrame, error) {
	trimmed := bytes.TrimSpace(rawData)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return st.Flush()
	}

	st.decodeCtx.ReceivedChunks++
	events, err := st.pipeline.target.DecodeStreamEvent(sseEvent, trimmed, st.decodeCtx)
	if err != nil {
		return nil, err
	}

	var frames []SSEFrame
	for _, ev := range events {
		st.recordStreamAccounting(ev)
		if ev.Type == EventError {
			copied := ev
			st.streamErr = &copied
			out, encErr := st.pipeline.source.EncodeStreamEvent(ev, st.encodeCtx)
			if encErr != nil {
				return frames, encErr
			}
			frames = append(frames, out...)
			// Terminate stream immediately so Flush() never fabricates a completion frame after an error.
			st.encodeCtx.FinishEmitted = true
			st.encodeCtx.Ended = true
			continue
		}
		if ev.Type != EventStreamStart {
			st.hasNonErrorEvent = true
		}
		out, err := st.pipeline.source.EncodeStreamEvent(ev, st.encodeCtx)
		if err != nil {
			return frames, err
		}
		frames = append(frames, out...)
		if ev.Type == EventStreamEnd {
			st.persistBridge()
		}
	}
	st.encodeCtx.EmittedFrames += len(frames)
	return frames, nil
}

// Flush closes open blocks, emits terminal events, and persists captured cross-turn bridge metadata.
func (st *StreamTranslator) Flush() ([]SSEFrame, error) {
	// Always persist cross-turn bridge metadata even if EventStreamEnd already marked encodeCtx.Ended = true.
	st.persistBridge()
	if st.encodeCtx.Ended {
		return nil, nil
	}
	frames, err := st.pipeline.source.FlushStream(st.encodeCtx)
	st.encodeCtx.Ended = true
	st.encodeCtx.EmittedFrames += len(frames)
	return frames, err
}

// StreamError returns the upstream EventError observed during streaming, if any.
func (st *StreamTranslator) StreamError() *StreamEvent {
	return st.streamErr
}

// HasNonErrorOutput reports whether any meaningful non-error event was observed before a stream error or disconnect.
func (st *StreamTranslator) HasNonErrorOutput() bool {
	return st.hasNonErrorEvent
}

// EmittedFrames returns the count of downstream SSE frames produced so far.
func (st *StreamTranslator) EmittedFrames() int {
	return st.encodeCtx.EmittedFrames
}

// PersistBridgeNow flushes captured TurnBridge metadata idempotently (safe on early disconnect).
func (st *StreamTranslator) PersistBridgeNow() {
	st.persistBridge()
}

// Usage returns the accumulated Usage observed across the stream (nil if upstream reported none).
func (st *StreamTranslator) Usage() *Usage {
	if st.encodeCtx.LastUsage != nil {
		return st.encodeCtx.LastUsage
	}
	return st.decodeCtx.LastUsage
}

// EstimatedOutputText returns all streamed text, reasoning, and tool argument fragments concatenated
// for fallback token estimation when an upstream omits usage metrics.
func (st *StreamTranslator) EstimatedOutputText() string {
	return st.decodeCtx.OutputTextEstimate.String()
}

// ReceivedChunks returns the count of non-empty upstream chunks processed.
func (st *StreamTranslator) ReceivedChunks() int {
	return st.decodeCtx.ReceivedChunks
}

// Warnings returns all conversion warnings accumulated by decode and encode contexts.
func (st *StreamTranslator) Warnings() []Warning {
	out := make([]Warning, 0, len(st.decodeCtx.Warnings)+len(st.encodeCtx.Warnings))
	out = append(out, st.decodeCtx.Warnings...)
	out = append(out, st.encodeCtx.Warnings...)
	return out
}

// TranslateReader streams an entire upstream SSE io.Reader, invoking emit for each downstream SSEFrame.
func (st *StreamTranslator) TranslateReader(r io.Reader, emit func(frame SSEFrame) error) error {
	reader := NewSSEReader(r)
	for {
		evName, data, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			st.persistBridge()
			return err
		}
		frames, procErr := st.ProcessChunk(evName, data)
		if procErr != nil {
			st.persistBridge()
			return procErr
		}
		for _, f := range frames {
			if emitErr := emit(f); emitErr != nil {
				st.persistBridge()
				return emitErr
			}
		}
	}
	tail, flushErr := st.Flush()
	if flushErr != nil {
		return flushErr
	}
	for _, f := range tail {
		if emitErr := emit(f); emitErr != nil {
			return emitErr
		}
	}
	return nil
}

// TranslateStreamToResponse consumes an upstream SSE stream (TargetFormat), aggregates all decoded IR
// stream events into a single complete IR Response, persists TurnBridge metadata, and encodes the
// response into SourceFormat JSON (used e.g. when a non-streaming client targets a stream-only upstream like Codex).
func (p *Pipeline) TranslateStreamToResponse(r io.Reader, fallbackModel string) ([]byte, *Response, *StreamEvent, int, error) {
	decodeCtx := NewStreamContext(p.target.Format(), p.source.Format())
	decodeCtx.MetadataMode = p.mode
	decodeCtx.BridgeScope = p.bridgeScope
	decodeCtx.Model = fallbackModel
	for k, v := range p.options {
		decodeCtx.Options[k] = v
	}

	var (
		textBuf          strings.Builder
		reasoningBuf     strings.Builder
		signatureBuf     strings.Builder
		refusalBuf       strings.Builder
		redactedData     string
		reasoningOrigin  Format
		images           []*ImageSource
		toolCallsByID    = make(map[string]*ToolCall)
		toolCallsByIndex = make(map[int]*ToolCall)
		toolCallOrder    []*ToolCall
		finishReason     FinishReason
		rawFinishReason  string
		streamErr        *StreamEvent
	)

	reader := NewSSEReader(r)
	for {
		evName, data, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, streamErr, decodeCtx.ReceivedChunks, err
		}
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.Equal(trimmed, []byte("[DONE]")) {
			break
		}
		decodeCtx.ReceivedChunks++
		events, decErr := p.target.DecodeStreamEvent(evName, trimmed, decodeCtx)
		if decErr != nil {
			return nil, nil, streamErr, decodeCtx.ReceivedChunks, decErr
		}
		for _, ev := range events {
			if ev.ResponseID != "" && decodeCtx.ResponseID == "" {
				decodeCtx.ResponseID = ev.ResponseID
			}
			if ev.Model != "" && decodeCtx.Model == "" {
				decodeCtx.Model = ev.Model
			}
			if ev.Created > 0 && decodeCtx.Created == 0 {
				decodeCtx.Created = ev.Created
			}
			if ev.Usage != nil {
				decodeCtx.MergeUsage(ev.Usage)
			}
			switch ev.Type {
			case EventError:
				copied := ev
				streamErr = &copied
			case EventTextDelta:
				textBuf.WriteString(ev.Text)
			case EventImage:
				if ev.Image != nil {
					copiedImg := *ev.Image
					images = append(images, &copiedImg)
				}
			case EventReasoningDelta:
				reasoningBuf.WriteString(ev.Reasoning)
				if ev.Signature != "" {
					signatureBuf.WriteString(ev.Signature)
				}
				if ev.RedactedData != "" {
					redactedData = ev.RedactedData
				}
				if ev.OriginFormat != FormatUnknown {
					reasoningOrigin = ev.OriginFormat
				}
			case EventReasoningSignatureDelta:
				if ev.Signature != "" {
					signatureBuf.WriteString(ev.Signature)
				}
				if ev.OriginFormat != FormatUnknown {
					reasoningOrigin = ev.OriginFormat
				}
			case EventRefusalDelta:
				refusalBuf.WriteString(ev.Refusal)
			case EventToolCallStart:
				tc := toolCallsByID[ev.ToolCallID]
				if tc == nil && ev.ToolCallID == "" {
					tc = toolCallsByIndex[ev.ToolCallIndex]
				}
				if tc == nil {
					id := ev.ToolCallID
					if id == "" {
						id = SynthesizeToolCallID("call")
					}
					tc = &ToolCall{
						ID:               id,
						ItemID:           ev.ToolCallItemID,
						Name:             ev.ToolName,
						Kind:             ev.ToolKind,
						ThoughtSignature: ev.ThoughtSignature,
					}
					toolCallsByID[id] = tc
					toolCallsByIndex[ev.ToolCallIndex] = tc
					toolCallOrder = append(toolCallOrder, tc)
				} else {
					if ev.ToolCallItemID != "" {
						tc.ItemID = ev.ToolCallItemID
					}
					if ev.ToolName != "" {
						tc.Name = ev.ToolName
					}
					if ev.ToolKind != "" {
						tc.Kind = ev.ToolKind
					}
					if ev.ThoughtSignature != "" {
						tc.ThoughtSignature = ev.ThoughtSignature
					}
				}
				if ev.ToolArguments != "" {
					tc.Arguments += ev.ToolArguments
				}
			case EventToolCallDelta:
				var tc *ToolCall
				if ev.ToolCallID != "" {
					tc = toolCallsByID[ev.ToolCallID]
				} else {
					tc = toolCallsByIndex[ev.ToolCallIndex]
				}
				if tc == nil {
					id := ev.ToolCallID
					if id == "" {
						id = SynthesizeToolCallID("call")
					}
					tc = &ToolCall{
						ID:               id,
						ItemID:           ev.ToolCallItemID,
						Name:             ev.ToolName,
						Kind:             ev.ToolKind,
						ThoughtSignature: ev.ThoughtSignature,
					}
					toolCallsByID[id] = tc
					toolCallsByIndex[ev.ToolCallIndex] = tc
					toolCallOrder = append(toolCallOrder, tc)
				} else {
					if ev.ToolName != "" && tc.Name == "" {
						tc.Name = ev.ToolName
					}
					if ev.ThoughtSignature != "" && tc.ThoughtSignature == "" {
						tc.ThoughtSignature = ev.ThoughtSignature
					}
				}
				if ev.ToolArguments != "" {
					tc.Arguments += ev.ToolArguments
				}
			case EventFinish:
				if ev.FinishReason != "" {
					finishReason = ev.FinishReason
				}
				if ev.RawFinishReason != "" {
					rawFinishReason = ev.RawFinishReason
				}
			}
		}
	}

	if streamErr != nil && textBuf.Len() == 0 && reasoningBuf.Len() == 0 && len(toolCallOrder) == 0 && len(images) == 0 {
		return nil, nil, streamErr, decodeCtx.ReceivedChunks, nil
	}

	var parts []ContentPart
	if reasoningBuf.Len() > 0 || signatureBuf.Len() > 0 || redactedData != "" {
		orig := reasoningOrigin
		if orig == FormatUnknown {
			orig = p.target.Format()
		}
		parts = append(parts, ContentPart{
			Type: PartReasoning,
			Reasoning: &Reasoning{
				Text:         reasoningBuf.String(),
				Signature:    signatureBuf.String(),
				RedactedData: redactedData,
				OriginFormat: orig,
			},
		})
	}
	if textBuf.Len() > 0 {
		parts = append(parts, ContentPart{Type: PartText, Text: textBuf.String()})
	}
	for _, img := range images {
		parts = append(parts, ContentPart{Type: PartImage, Image: img})
	}
	if refusalBuf.Len() > 0 {
		parts = append(parts, ContentPart{Type: PartRefusal, Refusal: refusalBuf.String()})
	}
	for _, tc := range toolCallOrder {
		if strings.TrimSpace(tc.Arguments) == "" && tc.Kind != "custom" {
			tc.Arguments = "{}"
		}
		parts = append(parts, ContentPart{Type: PartToolCall, ToolCall: tc})
	}

	if finishReason == "" {
		if len(toolCallOrder) > 0 {
			finishReason = FinishToolCalls
		} else {
			finishReason = FinishStop
		}
	}

	decodeCtx.EnsureEnvelope("resp")
	model := decodeCtx.Model
	if model == "" {
		model = fallbackModel
	}
	irResp := &Response{
		ID:      decodeCtx.ResponseID,
		Model:   model,
		Created: decodeCtx.Created,
		Choices: []Choice{{
			Index: 0,
			Message: Message{
				Role:  RoleAssistant,
				Parts: parts,
			},
			FinishReason:    finishReason,
			RawFinishReason: rawFinishReason,
		}},
		Usage: decodeCtx.LastUsage,
	}

	if p.bridge != nil {
		decodeCtx.BridgeState.ExtractResponseBridge(irResp, p.target.Format())
		p.bridge.Save(p.bridgeScope, decodeCtx.BridgeState)
	}

	encCtx := NewConversionContext(p.target.Format(), p.source.Format())
	encCtx.MetadataMode = p.mode
	encCtx.BridgeScope = p.bridgeScope
	for k, v := range p.options {
		encCtx.Options[k] = v
	}
	out, err := p.source.EncodeResponse(irResp, encCtx)
	if err != nil {
		return nil, irResp, streamErr, decodeCtx.ReceivedChunks, fmt.Errorf("ir: encode aggregated %s response: %w", p.source.Format(), err)
	}
	if p.sourceShim != nil && len(p.sourceShim.PostIRResponseTransforms) > 0 {
		out, err = ApplyJSONTransforms(out, p.sourceShim.PostIRResponseTransforms, encCtx)
		if err != nil {
			return nil, irResp, streamErr, decodeCtx.ReceivedChunks, fmt.Errorf("ir: source post-IR response shim: %w", err)
		}
	}
	return out, irResp, streamErr, decodeCtx.ReceivedChunks, nil
}

func (st *StreamTranslator) appendOutputEstimate(s string) {
	if s == "" {
		return
	}
	remaining := maxOutputTextEstimateBytes - st.decodeCtx.OutputTextEstimate.Len()
	if remaining <= 0 {
		return
	}
	if len(s) > remaining {
		st.decodeCtx.OutputTextEstimate.WriteString(s[:remaining])
		return
	}
	st.decodeCtx.OutputTextEstimate.WriteString(s)
}

func (st *StreamTranslator) recordStreamAccounting(ev StreamEvent) {
	if ev.ResponseID != "" && st.encodeCtx.ResponseID == "" {
		st.encodeCtx.ResponseID = ev.ResponseID
	}
	if ev.Model != "" && st.encodeCtx.Model == "" {
		st.encodeCtx.Model = ev.Model
	}
	if ev.Created > 0 && st.encodeCtx.Created == 0 {
		st.encodeCtx.Created = ev.Created
	}
	st.appendOutputEstimate(ev.Text)
	st.appendOutputEstimate(ev.Reasoning)
	st.appendOutputEstimate(ev.ToolArguments)
	if ev.Usage != nil {
		st.decodeCtx.MergeUsage(ev.Usage)
		st.encodeCtx.MergeUsage(ev.Usage)
	}
	if (ev.Type == EventToolCallStart || ev.Type == EventToolCallDelta) && ev.ToolCallID != "" {
		st.decodeCtx.BridgeState.RecordToolCall(ToolCallBridgeEntry{
			ToolCallID:       ev.ToolCallID,
			ResponsesItemID:  ev.ToolCallItemID,
			ToolName:         ev.ToolName,
			ThoughtSignature: ev.ThoughtSignature,
			OriginFormat:     st.pipeline.target.Format(),
		})
	}
}

func (st *StreamTranslator) persistBridge() {
	if st.bridgePersisted || st.pipeline.bridge == nil {
		return
	}
	st.bridgePersisted = true
	st.pipeline.bridge.Save(st.pipeline.bridgeScope, st.decodeCtx.BridgeState)
}
