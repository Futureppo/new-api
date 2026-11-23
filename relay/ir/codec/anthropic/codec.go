package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relay/ir"
)

// Codec implements bidirectional translation between the Anthropic Messages API
// (/v1/messages) wire dialect and the provider-neutral IR.
type Codec struct{}

var _ ir.Codec = (*Codec)(nil)

// New creates an Anthropic Messages API codec instance.
func New() *Codec {
	return &Codec{}
}

// Format returns ir.FormatAnthropic.
func (*Codec) Format() ir.Format {
	return ir.FormatAnthropic
}

// Shared Anthropic wire structures used across request, response, and streaming codecs.

type wireCacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type wireSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type wireCitation struct {
	Type          string `json:"type,omitempty"`
	URL           string `json:"url,omitempty"`
	Title         string `json:"title,omitempty"`
	DocumentTitle string `json:"document_title,omitempty"`
	CitedText     string `json:"cited_text,omitempty"`
	StartCharIdx  int    `json:"start_char_index,omitempty"`
	EndCharIdx    int    `json:"end_char_index,omitempty"`
	StartBlockIdx int    `json:"start_block_index,omitempty"`
	EndBlockIdx   int    `json:"end_block_index,omitempty"`
}

// wireContentBlock is the unified decoding struct for Anthropic content blocks.
type wireContentBlock struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	Thinking     string            `json:"thinking,omitempty"`
	Signature    string            `json:"signature,omitempty"`
	Data         string            `json:"data,omitempty"`
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name,omitempty"`
	Input        json.RawMessage   `json:"input,omitempty"`
	ToolUseID    string            `json:"tool_use_id,omitempty"`
	Content      json.RawMessage   `json:"content,omitempty"`
	IsError      bool              `json:"is_error,omitempty"`
	Source       *wireSource       `json:"source,omitempty"`
	Title        string            `json:"title,omitempty"`
	Citations    []wireCitation    `json:"citations,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

// Dedicated encoding block types avoid omitempty bugs on required empty fields
// (such as empty text/thinking strings or empty {} tool inputs).

type wireTextBlockEnc struct {
	Type         string            `json:"type"`
	Text         string            `json:"text"`
	Citations    []wireCitation    `json:"citations,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireImageBlockEnc struct {
	Type         string            `json:"type"`
	Source       wireSource        `json:"source"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireDocumentBlockEnc struct {
	Type         string            `json:"type"`
	Source       wireSource        `json:"source"`
	Title        string            `json:"title,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireThinkingBlockEnc struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}

type wireRedactedThinkingBlockEnc struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type wireToolUseBlockEnc struct {
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Input        json.RawMessage   `json:"input"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireToolResultBlockEnc struct {
	Type         string            `json:"type"`
	ToolUseID    string            `json:"tool_use_id"`
	Content      any               `json:"content"`
	IsError      bool              `json:"is_error,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireCacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens,omitempty"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens,omitempty"`
}

type wireServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests,omitempty"`
}

type wireUsage struct {
	InputTokens              int                `json:"input_tokens"`
	OutputTokens             int                `json:"output_tokens"`
	CacheCreationInputTokens int                `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                `json:"cache_read_input_tokens,omitempty"`
	CacheCreation            *wireCacheCreation `json:"cache_creation,omitempty"`
	ServerToolUse            *wireServerToolUse `json:"server_tool_use,omitempty"`
	ServiceTier              string             `json:"service_tier,omitempty"`
}

func decodeCacheControl(cc *wireCacheControl) *ir.CacheControl {
	if cc == nil || cc.Type == "" {
		return nil
	}
	return &ir.CacheControl{
		Type: cc.Type,
		TTL:  cc.TTL,
	}
}

func encodeCacheControl(cc *ir.CacheControl) *wireCacheControl {
	if cc == nil || cc.Type == "" {
		return nil
	}
	return &wireCacheControl{
		Type: cc.Type,
		TTL:  cc.TTL,
	}
}

// decodeUsage converts Anthropic usage into IR Usage with InputExclusive=true.
// When computeTotal is true (non-streaming final responses), TotalTokens is populated immediately.
// For streaming chunks, leave TotalTokens=0 so StreamContext.MergeUsage computes the final sum.
func decodeUsage(wu *wireUsage, computeTotal bool) *ir.Usage {
	if wu == nil {
		return nil
	}
	cacheCreationTotal := wu.CacheCreationInputTokens
	var raw5m, raw1h int
	if wu.CacheCreation != nil {
		raw5m = wu.CacheCreation.Ephemeral5mInputTokens
		raw1h = wu.CacheCreation.Ephemeral1hInputTokens
	}
	if cacheCreationTotal == 0 && (raw5m > 0 || raw1h > 0) {
		cacheCreationTotal = raw5m + raw1h
	}
	c5m, c1h := ir.NormalizeCacheCreationSplit(cacheCreationTotal, raw5m, raw1h)

	u := &ir.Usage{
		InputTokens:           wu.InputTokens,
		OutputTokens:          wu.OutputTokens,
		InputExclusive:        true,
		CachedReadTokens:      wu.CacheReadInputTokens,
		CacheCreationTokens:   cacheCreationTotal,
		CacheCreation5mTokens: c5m,
		CacheCreation1hTokens: c1h,
	}
	if wu.ServerToolUse != nil {
		u.WebSearchRequests = wu.ServerToolUse.WebSearchRequests
	}
	if computeTotal {
		u.TotalTokens = u.InclusiveInputTokens() + u.OutputTokens
	}
	return u
}

func encodeUsage(u *ir.Usage) wireUsage {
	if u == nil {
		return wireUsage{}
	}
	out := wireUsage{
		InputTokens:              u.ExclusiveInputTokens(),
		OutputTokens:             u.OutputTokens,
		CacheReadInputTokens:     u.CachedReadTokens,
		CacheCreationInputTokens: u.CacheCreationTokens,
	}
	if u.CacheCreation5mTokens > 0 || u.CacheCreation1hTokens > 0 {
		out.CacheCreation = &wireCacheCreation{
			Ephemeral5mInputTokens: u.CacheCreation5mTokens,
			Ephemeral1hInputTokens: u.CacheCreation1hTokens,
		}
	}
	if u.WebSearchRequests > 0 {
		out.ServerToolUse = &wireServerToolUse{
			WebSearchRequests: u.WebSearchRequests,
		}
	}
	return out
}

func decodeFinishReason(stopReason string) ir.FinishReason {
	switch stopReason {
	case "end_turn", "stop_sequence", "pause_turn":
		return ir.FinishStop
	case "max_tokens", "model_context_window_exceeded":
		return ir.FinishLength
	case "tool_use":
		return ir.FinishToolCalls
	case "refusal":
		return ir.FinishContentFilter
	default:
		return ""
	}
}

func encodeFinishReason(fr ir.FinishReason, raw string) string {
	switch fr {
	case ir.FinishStop:
		if raw == "stop_sequence" || raw == "pause_turn" {
			return raw
		}
		return "end_turn"
	case ir.FinishLength:
		return "max_tokens"
	case ir.FinishToolCalls:
		return "tool_use"
	case ir.FinishContentFilter:
		return "refusal"
	case ir.FinishError:
		return "end_turn"
	default:
		if raw != "" {
			return raw
		}
		return "end_turn"
	}
}

// canonicalizeJSONObject compacts a raw JSON payload into a canonical JSON string,
// defaulting empty or invalid values to "{}".
func canonicalizeJSONObject(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return "{}"
	}
	return buf.String()
}

// ensureJSONObject guarantees a valid JSON object payload for Anthropic tool_use.input.
func ensureJSONObject(raw string) json.RawMessage {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	b := []byte(trimmed)
	if b[0] != '{' || !json.Valid(b) {
		return json.RawMessage(`{}`)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(buf.Bytes())
}

// ensureMsgIDPrefix guarantees Anthropic message IDs always start with "msg_".
func ensureMsgIDPrefix(id string) string {
	if id == "" {
		return fmt.Sprintf("msg_%d", time.Now().UnixNano())
	}
	if strings.HasPrefix(id, "msg_") {
		return id
	}
	return "msg_" + id
}

func nonEmpty(val, fallback string) string {
	if val != "" {
		return val
	}
	return fallback
}
