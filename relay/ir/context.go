package ir

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var (
	toolCallIDSalt    = initToolCallIDSalt()
	toolCallIDCounter atomic.Uint64
)

func initToolCallIDSalt() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
}

// SynthesizeToolCallID generates a short, collision-resistant tool call ID (<= 24 chars)
// so multi-turn and multi-tenant requests never collide on deterministic IDs like "call_0".
func SynthesizeToolCallID(prefix string) string {
	if prefix == "" {
		prefix = "call"
	}
	return fmt.Sprintf("%s_%s_%x", prefix, toolCallIDSalt, toolCallIDCounter.Add(1))
}

// MetadataMode controls whether provider-specific opaque fields survive round-trips.
type MetadataMode string

const (
	MetadataStrip    MetadataMode = "strip"
	MetadataPreserve MetadataMode = "preserve"
)

// ConversionContext carries shared state, warnings, options, and bridge data through a single conversion pipeline.
type ConversionContext struct {
	SourceFormat Format
	TargetFormat Format
	MetadataMode MetadataMode
	BridgeScope  string
	BridgeState  *BridgeState
	Warnings     []Warning
	Options      map[string]any
	Metadata     map[string]any
}

// NewConversionContext initializes a ConversionContext with defaults.
func NewConversionContext(source, target Format) *ConversionContext {
	mode := MetadataStrip
	if source != FormatUnknown && source == target {
		mode = MetadataPreserve
	}
	return &ConversionContext{
		SourceFormat: source,
		TargetFormat: target,
		MetadataMode: mode,
		BridgeState:  NewBridgeState(),
		Options:      make(map[string]any),
		Metadata:     make(map[string]any),
	}
}

// AddWarning records a lossy translation warning.
func (c *ConversionContext) AddWarning(code, field, message string) {
	if c == nil {
		return
	}
	c.Warnings = append(c.Warnings, Warning{
		Code:    code,
		Field:   field,
		Message: message,
	})
}

// ShouldPreserveMetadata reports whether same-dialect opaque fields should be emitted.
func (c *ConversionContext) ShouldPreserveMetadata(origin Format) bool {
	if c == nil {
		return false
	}
	if origin != FormatUnknown && c.TargetFormat != FormatUnknown && origin != c.TargetFormat {
		return false
	}
	return c.MetadataMode == MetadataPreserve || (origin != FormatUnknown && origin == c.TargetFormat)
}

// GetBoolOption reads a boolean option with a fallback default.
func (c *ConversionContext) GetBoolOption(key string, fallback bool) bool {
	if c == nil || c.Options == nil {
		return fallback
	}
	if v, ok := c.Options[key].(bool); ok {
		return v
	}
	return fallback
}

// GetIntOption reads an integer option with a fallback default.
func (c *ConversionContext) GetIntOption(key string, fallback int) int {
	if c == nil || c.Options == nil {
		return fallback
	}
	if v, ok := c.Options[key].(int); ok {
		return v
	}
	return fallback
}

// GetStringOption reads a string option with a fallback default.
func (c *ConversionContext) GetStringOption(key string, fallback string) string {
	if c == nil || c.Options == nil {
		return fallback
	}
	if v, ok := c.Options[key].(string); ok {
		return v
	}
	return fallback
}

// TrackedToolCall stores accumulated streaming state for one in-flight tool call.
type TrackedToolCall struct {
	Index            int
	BlockIndex       int
	OutputIndex      int
	ID               string
	ItemID           string
	Name             string
	Kind             string
	ThoughtSignature string
	Args             strings.Builder
	Started          bool
	Closed           bool
	ProviderMetadata map[string]any
}

// StreamContext extends ConversionContext with stateful cross-chunk tracking for SSE translation.
type StreamContext struct {
	*ConversionContext

	// Session envelope metadata
	ResponseID string
	Model      string
	Created    int64
	Started    bool
	Ended      bool

	// Block lifecycle state (Anthropic / Responses / OpenAI Chat)
	NextBlockIndex   int
	ActiveBlockIndex int
	ActiveBlockType  PartType
	BlockOpen        bool

	// Tool call tracking
	ToolCallsByID    map[string]*TrackedToolCall
	ToolCallsByIndex map[int]*TrackedToolCall
	ToolCallOrder    []*TrackedToolCall
	NextToolIndex    int

	// OpenAI Responses streaming state
	SequenceNumber       int
	NextOutputIndex      int
	MessageItemID        string
	MessageOutputIndex   int
	MessageItemStarted   bool
	MessageItemDone      bool
	ReasoningItemID      string
	ReasoningOutputIndex int
	ReasoningItemStarted bool
	ReasoningItemDone    bool
	AccumulatedText      strings.Builder
	AccumulatedReasoning strings.Builder
	AccumulatedRefusal   strings.Builder
	ReasoningSignature   strings.Builder
	ReasoningRedacted    string
	ReasoningOrigin      Format

	// Google GenAI streaming delta tracking
	GoogleSeenToolCallCount int

	// Multi-choice streaming tracking (OpenAI Chat n > 1 / Google multi-candidate)
	ChoiceFinishEmitted map[int]bool
	ChoicePendingFinish map[int]FinishReason

	// Deferred completion & usage accounting
	PendingFinish       *FinishReason
	PendingRawFinish    string
	PendingUsage        *Usage
	LastUsage           *Usage
	FinishEmitted       bool
	UsageEmitted        bool
	ReceivedChunks      int
	EmittedFrames       int
	WebSearchCalls      int
	FileSearchCalls     int
	HasImageGenCall     bool
	ImageGenQuality     string
	ImageGenSize        string
	SeenToolItemIDs     map[string]struct{}
	OutputTextEstimate  strings.Builder
}

// NewStreamContext constructs a StreamContext for a source/target translation stream.
func NewStreamContext(source, target Format) *StreamContext {
	return &StreamContext{
		ConversionContext:    NewConversionContext(source, target),
		Created:              time.Now().Unix(),
		ActiveBlockIndex:     -1,
		MessageOutputIndex:   -1,
		ReasoningOutputIndex: -1,
		ToolCallsByID:        make(map[string]*TrackedToolCall),
		ToolCallsByIndex:     make(map[int]*TrackedToolCall),
		ChoiceFinishEmitted:  make(map[int]bool),
		ChoicePendingFinish:  make(map[int]FinishReason),
		SeenToolItemIDs:      make(map[string]struct{}),
	}
}

// EnsureEnvelope guarantees ResponseID, Model, and Created are populated before emitting frames.
func (s *StreamContext) EnsureEnvelope(fallbackPrefix string) {
	if s.Created == 0 {
		s.Created = time.Now().Unix()
	}
	if s.ResponseID == "" {
		s.ResponseID = fmt.Sprintf("%s_%d", fallbackPrefix, time.Now().UnixNano())
	}
}

// NextSeq increments and returns the monotonic OpenAI Responses sequence number (0-indexed).
func (s *StreamContext) NextSeq() int {
	n := s.SequenceNumber
	s.SequenceNumber++
	return n
}

// OpenBlock starts a new content block and returns its 0-based index.
func (s *StreamContext) OpenBlock(blockType PartType) int {
	idx := s.NextBlockIndex
	s.NextBlockIndex++
	s.ActiveBlockIndex = idx
	s.ActiveBlockType = blockType
	s.BlockOpen = true
	return idx
}

// CloseActiveBlock closes the currently open content block if any and returns its index and whether one was closed.
func (s *StreamContext) CloseActiveBlock() (int, PartType, bool) {
	if !s.BlockOpen {
		return -1, "", false
	}
	idx := s.ActiveBlockIndex
	bt := s.ActiveBlockType
	s.BlockOpen = false
	s.ActiveBlockIndex = -1
	s.ActiveBlockType = ""
	return idx, bt, true
}

// RegisterToolCall creates or updates a tracked tool call and returns it along with whether it was newly created.
func (s *StreamContext) RegisterToolCall(index int, id, itemID, name, kind, thoughtSig string) (*TrackedToolCall, bool) {
	var tc *TrackedToolCall
	if id != "" {
		tc = s.ToolCallsByID[id]
	}
	if tc == nil && index >= 0 {
		if existing := s.ToolCallsByIndex[index]; existing != nil {
			// Guard against index collisions when two distinct tool calls reuse the same index (e.g. 0)
			// or when an already-closed slot is reused for a new tool call.
			idConflict := id != "" && existing.ID != "" && existing.ID != id
			if !idConflict && !existing.Closed {
				tc = existing
			} else {
				index = s.NextToolIndex
			}
		}
	}
	created := false
	if tc == nil {
		if index < 0 {
			index = s.NextToolIndex
		}
		for s.ToolCallsByIndex[index] != nil {
			index++
		}
		if index >= s.NextToolIndex {
			s.NextToolIndex = index + 1
		}
		if id == "" {
			id = SynthesizeToolCallID("call")
		}
		tc = &TrackedToolCall{
			Index:       index,
			OutputIndex: -1,
			BlockIndex:  -1,
			ID:          id,
			ItemID:      itemID,
			Name:        name,
			Kind:        kind,
		}
		s.ToolCallsByID[id] = tc
		s.ToolCallsByIndex[index] = tc
		s.ToolCallOrder = append(s.ToolCallOrder, tc)
		created = true
	}
	if id != "" && tc.ID != id {
		tc.ID = id
		s.ToolCallsByID[id] = tc
	}
	if itemID != "" {
		tc.ItemID = itemID
	}
	if name != "" {
		tc.Name = name
	}
	if kind != "" {
		tc.Kind = kind
	}
	if thoughtSig != "" {
		tc.ThoughtSignature = thoughtSig
	}
	if s.BridgeState != nil && tc.ID != "" {
		s.BridgeState.RecordToolCall(ToolCallBridgeEntry{
			ToolCallID:       tc.ID,
			ResponsesItemID:  tc.ItemID,
			ToolName:         tc.Name,
			ThoughtSignature: tc.ThoughtSignature,
			OriginFormat:     s.SourceFormat,
		})
	}
	return tc, created
}

// LookupToolCall finds a tracked tool call by ID or index without cross-contaminating distinct tool IDs.
func (s *StreamContext) LookupToolCall(index int, id string) *TrackedToolCall {
	if id != "" {
		// Exact ID match only: never fall back to a different tool call when caller supplied a specific ID.
		return s.ToolCallsByID[id]
	}
	if index >= 0 {
		if tc := s.ToolCallsByIndex[index]; tc != nil {
			return tc
		}
		return nil
	}
	// Wildcard lookup (id == "" && index < 0): return the most recent open tool call.
	for i := len(s.ToolCallOrder) - 1; i >= 0; i-- {
		if !s.ToolCallOrder[i].Closed {
			return s.ToolCallOrder[i]
		}
	}
	return nil
}

// LookupToolCallByBlockIndex finds a tracked tool call associated with an Anthropic/stream content block index.
func (s *StreamContext) LookupToolCallByBlockIndex(blockIndex int) *TrackedToolCall {
	if blockIndex < 0 {
		return nil
	}
	for i := len(s.ToolCallOrder) - 1; i >= 0; i-- {
		if s.ToolCallOrder[i].BlockIndex == blockIndex {
			return s.ToolCallOrder[i]
		}
	}
	return nil
}

// MergeUsage merges incremental or final usage snapshots into LastUsage without losing earlier fields.
func (s *StreamContext) MergeUsage(u *Usage) {
	if u == nil {
		return
	}
	if s.LastUsage == nil {
		copied := *u
		if copied.ReasoningTokens > copied.OutputTokens {
			copied.OutputTokens = copied.ReasoningTokens
		}
		if computed := copied.InclusiveInputTokens() + copied.OutputTokens; computed > copied.TotalTokens {
			copied.TotalTokens = computed
		}
		s.LastUsage = &copied
		return
	}
	dst := s.LastUsage
	if u.InputExclusive {
		dst.InputExclusive = true
	}
	if u.InputTokens > dst.InputTokens {
		dst.InputTokens = u.InputTokens
	}
	if u.OutputTokens > 0 {
		if u.OutputTokens > dst.OutputTokens {
			dst.OutputTokens = u.OutputTokens
		} else if u.ReasoningTokens == 0 && dst.ReasoningTokens > 0 && u.OutputTokens+dst.ReasoningTokens > dst.OutputTokens {
			// Guard against providers (e.g. Gemini) omitting earlier thoughtsTokenCount on a later chunk.
			dst.OutputTokens = u.OutputTokens + dst.ReasoningTokens
		}
	}
	if u.TotalTokens > dst.TotalTokens {
		dst.TotalTokens = u.TotalTokens
	}
	if u.CachedReadTokens > dst.CachedReadTokens {
		dst.CachedReadTokens = u.CachedReadTokens
	}
	if u.CacheCreationTokens > dst.CacheCreationTokens {
		dst.CacheCreationTokens = u.CacheCreationTokens
	}
	if u.CacheCreation5mTokens > dst.CacheCreation5mTokens {
		dst.CacheCreation5mTokens = u.CacheCreation5mTokens
	}
	if u.CacheCreation1hTokens > dst.CacheCreation1hTokens {
		dst.CacheCreation1hTokens = u.CacheCreation1hTokens
	}
	if u.ReasoningTokens > dst.ReasoningTokens {
		dst.ReasoningTokens = u.ReasoningTokens
	}
	if u.AudioInputTokens > dst.AudioInputTokens {
		dst.AudioInputTokens = u.AudioInputTokens
	}
	if u.AudioOutputTokens > dst.AudioOutputTokens {
		dst.AudioOutputTokens = u.AudioOutputTokens
	}
	if u.ImageInputTokens > dst.ImageInputTokens {
		dst.ImageInputTokens = u.ImageInputTokens
	}
	if u.ImageOutputTokens > dst.ImageOutputTokens {
		dst.ImageOutputTokens = u.ImageOutputTokens
	}
	if u.TextInputTokens > dst.TextInputTokens {
		dst.TextInputTokens = u.TextInputTokens
	}
	if u.TextOutputTokens > dst.TextOutputTokens {
		dst.TextOutputTokens = u.TextOutputTokens
	}
	if u.WebSearchRequests > dst.WebSearchRequests {
		dst.WebSearchRequests = u.WebSearchRequests
	}
	if u.FileSearchCalls > dst.FileSearchCalls {
		dst.FileSearchCalls = u.FileSearchCalls
	}
	if u.HasImageGenCall {
		dst.HasImageGenCall = true
	}
	if u.ImageGenQuality != "" {
		dst.ImageGenQuality = u.ImageGenQuality
	}
	if u.ImageGenSize != "" {
		dst.ImageGenSize = u.ImageGenSize
	}
	if dst.ReasoningTokens > dst.OutputTokens {
		dst.OutputTokens = dst.ReasoningTokens
	}
	if computed := dst.InclusiveInputTokens() + dst.OutputTokens; computed > dst.TotalTokens {
		dst.TotalTokens = computed
	}
}
