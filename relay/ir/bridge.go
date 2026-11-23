package ir

import (
	"container/list"
	"sync"
	"time"
)

// ToolCallBridgeEntry preserves provider-specific correlation metadata that does not survive
// a client protocol that lacks equivalent fields (e.g., OpenAI Responses fc_* item IDs or
// Google Gemini thoughtSignature across OpenAI Chat / Anthropic turns).
type ToolCallBridgeEntry struct {
	ToolCallID       string
	ResponsesItemID  string
	ToolName         string
	Namespace        string
	ThoughtSignature string
	OriginFormat     Format
}

// BridgeState is the per-conversion serializable bag exported after responses and injected before requests.
type BridgeState struct {
	ToolCalls map[string]ToolCallBridgeEntry
}

// NewBridgeState allocates an empty BridgeState.
func NewBridgeState() *BridgeState {
	return &BridgeState{
		ToolCalls: make(map[string]ToolCallBridgeEntry),
	}
}

// RecordToolCall merges non-empty metadata fields for a tool call ID.
func (b *BridgeState) RecordToolCall(entry ToolCallBridgeEntry) {
	if b == nil || entry.ToolCallID == "" {
		return
	}
	if b.ToolCalls == nil {
		b.ToolCalls = make(map[string]ToolCallBridgeEntry)
	}
	existing := b.ToolCalls[entry.ToolCallID]
	existing.ToolCallID = entry.ToolCallID
	if entry.ResponsesItemID != "" {
		existing.ResponsesItemID = entry.ResponsesItemID
	}
	if entry.ToolName != "" {
		existing.ToolName = entry.ToolName
	}
	if entry.Namespace != "" {
		existing.Namespace = entry.Namespace
	}
	if entry.ThoughtSignature != "" {
		existing.ThoughtSignature = entry.ThoughtSignature
	}
	if entry.OriginFormat != FormatUnknown {
		existing.OriginFormat = entry.OriginFormat
	}
	b.ToolCalls[entry.ToolCallID] = existing
}

// ExtractResponseBridge captures cross-turn metadata from a decoded IRResponse.
func (b *BridgeState) ExtractResponseBridge(resp *Response, origin Format) {
	if b == nil || resp == nil {
		return
	}
	for _, ch := range resp.Choices {
		for _, part := range ch.Message.Parts {
			if part.Type == PartToolCall && part.ToolCall != nil && part.ToolCall.ID != "" {
				b.RecordToolCall(ToolCallBridgeEntry{
					ToolCallID:       part.ToolCall.ID,
					ResponsesItemID:  part.ToolCall.ItemID,
					ToolName:         part.ToolCall.Name,
					Namespace:        part.ToolCall.Namespace,
					ThoughtSignature: part.ToolCall.ThoughtSignature,
					OriginFormat:     origin,
				})
			}
		}
	}
}

// InjectIntoRequest enriches request tool calls / results with cached cross-turn metadata.
func (b *BridgeState) InjectIntoRequest(req *Request) {
	if b == nil || req == nil || len(b.ToolCalls) == 0 {
		return
	}
	for mi := range req.Messages {
		for pi := range req.Messages[mi].Parts {
			part := &req.Messages[mi].Parts[pi]
			switch part.Type {
			case PartToolCall:
				if part.ToolCall == nil || part.ToolCall.ID == "" {
					continue
				}
				entry, ok := b.ToolCalls[part.ToolCall.ID]
				if !ok {
					continue
				}
				if part.ToolCall.ItemID == "" && entry.ResponsesItemID != "" {
					part.ToolCall.ItemID = entry.ResponsesItemID
				}
				if part.ToolCall.Name == "" && entry.ToolName != "" {
					part.ToolCall.Name = entry.ToolName
				}
				if part.ToolCall.Namespace == "" && entry.Namespace != "" {
					part.ToolCall.Namespace = entry.Namespace
				}
				if part.ToolCall.ThoughtSignature == "" && entry.ThoughtSignature != "" {
					part.ToolCall.ThoughtSignature = entry.ThoughtSignature
				}
			case PartToolResult:
				if part.ToolResult == nil || part.ToolResult.ToolCallID == "" {
					continue
				}
				if part.ToolResult.Name == "" {
					if entry, ok := b.ToolCalls[part.ToolResult.ToolCallID]; ok && entry.ToolName != "" {
						part.ToolResult.Name = entry.ToolName
					}
				}
			}
		}
	}
}

type bridgeCacheRecord struct {
	key       string
	entry     ToolCallBridgeEntry
	expiresAt time.Time
}

type turnBridgeShard struct {
	mu      sync.Mutex
	maxSize int
	lru     *list.List
	items   map[string]*list.Element
}

// TurnBridge is a concurrency-safe sharded TTL+LRU cache that bridges opaque cross-turn metadata
// across independent HTTP turns in the gateway with strict scope isolation.
type TurnBridge struct {
	ttl     time.Duration
	maxSize int
	now     func() time.Time
	shards  []turnBridgeShard
}

const (
	defaultTurnBridgeTTL     = 30 * time.Minute
	defaultTurnBridgeMaxSize = 10000
	defaultTurnBridgeShards  = 16
)

// NewTurnBridge creates a TurnBridge with the given TTL and maximum entries.
func NewTurnBridge(ttl time.Duration, maxSize int) *TurnBridge {
	if ttl <= 0 {
		ttl = defaultTurnBridgeTTL
	}
	if maxSize <= 0 {
		maxSize = defaultTurnBridgeMaxSize
	}
	numShards := 1
	if maxSize >= 64 {
		numShards = defaultTurnBridgeShards
	}
	shards := make([]turnBridgeShard, numShards)
	baseCap := maxSize / numShards
	rem := maxSize % numShards
	for i := range shards {
		shardCap := baseCap
		if i < rem {
			shardCap++
		}
		if shardCap < 1 {
			shardCap = 1
		}
		shards[i] = turnBridgeShard{
			maxSize: shardCap,
			lru:     list.New(),
			items:   make(map[string]*list.Element, 32),
		}
	}
	return &TurnBridge{
		ttl:     ttl,
		maxSize: maxSize,
		now:     time.Now,
		shards:  shards,
	}
}

var defaultTurnBridge = NewTurnBridge(defaultTurnBridgeTTL, defaultTurnBridgeMaxSize)

// DefaultTurnBridge returns the process-wide TurnBridge instance used by the relay pipeline.
func DefaultTurnBridge() *TurnBridge {
	return defaultTurnBridge
}

func (tb *TurnBridge) shardFor(scope string) *turnBridgeShard {
	if len(tb.shards) == 1 {
		return &tb.shards[0]
	}
	// FNV-1a 32-bit hash on scope so all entries for a tenant/channel scope stay in one shard.
	var h uint32 = 2166136261
	for i := 0; i < len(scope); i++ {
		h ^= uint32(scope[i])
		h *= 16777619
	}
	return &tb.shards[h%uint32(len(tb.shards))]
}

func makeBridgeKey(scope, toolCallID string) string {
	if scope == "" {
		return toolCallID
	}
	return scope + "\x00" + toolCallID
}

// Save stores all tool-call entries from state under scope.
func (tb *TurnBridge) Save(scope string, state *BridgeState) {
	if tb == nil || state == nil || len(state.ToolCalls) == 0 {
		return
	}
	shard := tb.shardFor(scope)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := tb.now()
	exp := now.Add(tb.ttl)
	for id, entry := range state.ToolCalls {
		if id == "" {
			continue
		}
		if entry.ResponsesItemID == "" && entry.ThoughtSignature == "" && entry.Namespace == "" && entry.ToolName == "" {
			continue
		}
		k := makeBridgeKey(scope, id)
		if elem, ok := shard.items[k]; ok {
			rec := elem.Value.(*bridgeCacheRecord)
			rec.entry = mergeBridgeEntry(rec.entry, entry)
			rec.expiresAt = exp
			shard.lru.MoveToFront(elem)
			continue
		}
		rec := &bridgeCacheRecord{
			key:       k,
			entry:     entry,
			expiresAt: exp,
		}
		elem := shard.lru.PushFront(rec)
		shard.items[k] = elem
	}
	shard.evictLocked(now)
}

// LoadForRequest looks up cached entries for every tool call / tool result ID referenced in req
// strictly within the caller's scope (never falling back across scopes).
func (tb *TurnBridge) LoadForRequest(scope string, req *Request) *BridgeState {
	state := NewBridgeState()
	if tb == nil || req == nil {
		return state
	}
	ids := collectRequestToolCallIDs(req)
	if len(ids) == 0 {
		return state
	}

	shard := tb.shardFor(scope)
	shard.mu.Lock()
	defer shard.mu.Unlock()

	now := tb.now()
	exp := now.Add(tb.ttl)
	for _, id := range ids {
		k := makeBridgeKey(scope, id)
		elem, ok := shard.items[k]
		if !ok {
			continue
		}
		rec := elem.Value.(*bridgeCacheRecord)
		if now.After(rec.expiresAt) {
			shard.lru.Remove(elem)
			delete(shard.items, rec.key)
			continue
		}
		// Refresh sliding TTL alongside MoveToFront so the LRU list stays monotonic in expiresAt.
		rec.expiresAt = exp
		shard.lru.MoveToFront(elem)
		state.ToolCalls[id] = rec.entry
	}
	return state
}

// Clear removes all cached bridge records.
func (tb *TurnBridge) Clear() {
	if tb == nil {
		return
	}
	for i := range tb.shards {
		s := &tb.shards[i]
		s.mu.Lock()
		s.lru.Init()
		s.items = make(map[string]*list.Element, 32)
		s.mu.Unlock()
	}
}

// Len returns the current number of cached entries (for observability/testing).
func (tb *TurnBridge) Len() int {
	if tb == nil {
		return 0
	}
	total := 0
	for i := range tb.shards {
		s := &tb.shards[i]
		s.mu.Lock()
		total += len(s.items)
		s.mu.Unlock()
	}
	return total
}

func (s *turnBridgeShard) evictLocked(now time.Time) {
	// Because every MoveToFront refreshes expiresAt, the LRU list is monotonic from tail (oldest) to head (newest).
	for elem := s.lru.Back(); elem != nil; {
		prev := elem.Prev()
		rec := elem.Value.(*bridgeCacheRecord)
		if now.After(rec.expiresAt) || len(s.items) > s.maxSize {
			s.lru.Remove(elem)
			delete(s.items, rec.key)
		} else {
			break
		}
		elem = prev
	}
}

func mergeBridgeEntry(base, next ToolCallBridgeEntry) ToolCallBridgeEntry {
	// If the colliding entry comes from a different origin protocol or distinct tool identity,
	// replace instead of stitching incompatible metadata together.
	if (base.OriginFormat != FormatUnknown && next.OriginFormat != FormatUnknown && base.OriginFormat != next.OriginFormat) ||
		(base.ToolName != "" && next.ToolName != "" && base.ToolName != next.ToolName) ||
		(base.ResponsesItemID != "" && next.ResponsesItemID != "" && base.ResponsesItemID != next.ResponsesItemID) {
		return next
	}
	if next.ToolCallID != "" {
		base.ToolCallID = next.ToolCallID
	}
	if next.ResponsesItemID != "" {
		base.ResponsesItemID = next.ResponsesItemID
	}
	if next.ToolName != "" {
		base.ToolName = next.ToolName
	}
	if next.Namespace != "" {
		base.Namespace = next.Namespace
	}
	if next.ThoughtSignature != "" {
		base.ThoughtSignature = next.ThoughtSignature
	}
	if next.OriginFormat != FormatUnknown {
		base.OriginFormat = next.OriginFormat
	}
	return base
}

func collectRequestToolCallIDs(req *Request) []string {
	var ids []string
	seen := make(map[string]struct{})
	for _, msg := range req.Messages {
		for _, part := range msg.Parts {
			var id string
			if part.Type == PartToolCall && part.ToolCall != nil {
				id = part.ToolCall.ID
			} else if part.Type == PartToolResult && part.ToolResult != nil {
				id = part.ToolResult.ToolCallID
			}
			if id == "" {
				continue
			}
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	return ids
}
