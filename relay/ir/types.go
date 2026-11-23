package ir

import (
	"encoding/json"
	"strings"
)

// Format identifies a wire protocol dialect handled by the IR conversion engine.
type Format string

const (
	FormatUnknown         Format = ""
	FormatOpenAIChat      Format = "openai_chat"
	FormatOpenAIResponses Format = "openai_responses"
	FormatAnthropic       Format = "anthropic_messages"
	FormatGoogleGenAI     Format = "google_genai"
)

// Role identifies the logical speaker of an IR message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleDeveloper Role = "developer"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// PartType discriminates the 9 atomic content part kinds in the provider-neutral IR.
type PartType string

const (
	PartText       PartType = "text"
	PartImage      PartType = "image"
	PartAudio      PartType = "audio"
	PartFile       PartType = "file"
	PartToolCall   PartType = "tool_call"
	PartToolResult PartType = "tool_result"
	PartReasoning  PartType = "reasoning"
	PartRefusal    PartType = "refusal"
	PartCitation   PartType = "citation"
)

// FinishReason represents normalized generation termination reasons.
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
	FinishError         FinishReason = "error"
)

// GoogleDummyThoughtSignature is Google's documented sentinel for cross-model tool history
// when a Gemini 3+ target requires thoughtSignature on functionCall parts without an origin signature.
const GoogleDummyThoughtSignature = "context_engineering_is_the_way_to_go"

// CacheControl captures prompt caching breakpoints across providers.
type CacheControl struct {
	Type string `json:"type,omitempty"` // e.g., "ephemeral"
	TTL  string `json:"ttl,omitempty"`  // e.g., "5m", "1h"
}

// ImageSource holds either a URL reference, provider file_id, or inline base64 image payload.
type ImageSource struct {
	FileID    string `json:"file_id,omitempty"`
	URL       string `json:"url,omitempty"`
	Data      string `json:"data,omitempty"` // raw base64 without data: prefix
	MediaType string `json:"media_type,omitempty"`
	Detail    string `json:"detail,omitempty"` // auto | low | high
}

// AudioSource holds inline audio data or a URL reference.
type AudioSource struct {
	Data      string `json:"data,omitempty"`
	Format    string `json:"format,omitempty"` // wav, mp3, pcm16, etc.
	MediaType string `json:"media_type,omitempty"`
	URL       string `json:"url,omitempty"`
}

// FileSource holds an arbitrary file/document attachment (e.g., PDF or plain text).
type FileSource struct {
	FileID    string `json:"file_id,omitempty"`
	Filename  string `json:"filename,omitempty"`
	URL       string `json:"url,omitempty"`
	Data      string `json:"data,omitempty"`
	Text      string `json:"text,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

// ToolCall represents a model-initiated tool invocation.
type ToolCall struct {
	ID        string `json:"id"`
	ItemID    string `json:"item_id,omitempty"` // OpenAI Responses fc_* output item id
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // canonical JSON object string
	Kind      string `json:"kind,omitempty"`      // "function" (default) or "custom"
	Namespace string `json:"namespace,omitempty"` // optional routing namespace
	// ThoughtSignature carries Google Gemini thoughtSignature attached to a functionCall part.
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

// ToolResult represents the output returned for a prior tool invocation.
type ToolResult struct {
	ToolCallID string        `json:"tool_call_id"`
	Name       string        `json:"name,omitempty"`
	Content    string        `json:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty"` // structured / multimodal tool results
	IsError    bool          `json:"is_error,omitempty"`
}

// Reasoning separates visible reasoning text, verification signatures, and opaque encrypted blobs.
type Reasoning struct {
	Text         string `json:"text,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Signature    string `json:"signature,omitempty"`     // coexists with visible text (Anthropic / Google)
	RedactedData string `json:"redacted_data,omitempty"` // replaces visible text (Anthropic redacted_thinking / Responses encrypted_content)
	OriginFormat Format `json:"origin_format,omitempty"` // dialect that minted Signature / RedactedData
	ItemID       string `json:"item_id,omitempty"`       // OpenAI Responses rs_* item id
}

// Citation captures a source attribution attached to text output.
type Citation struct {
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	CitedText  string `json:"cited_text,omitempty"`
	StartIndex int    `json:"start_index,omitempty"`
	EndIndex   int    `json:"end_index,omitempty"`
}

// ContentPart is the atomic unit inside an IR Message.
type ContentPart struct {
	Type             PartType       `json:"type"`
	Text             string         `json:"text,omitempty"`
	Image            *ImageSource   `json:"image,omitempty"`
	Audio            *AudioSource   `json:"audio,omitempty"`
	File             *FileSource    `json:"file,omitempty"`
	ToolCall         *ToolCall      `json:"tool_call,omitempty"`
	ToolResult       *ToolResult    `json:"tool_result,omitempty"`
	Reasoning        *Reasoning     `json:"reasoning,omitempty"`
	Refusal          string         `json:"refusal,omitempty"`
	Citation         *Citation      `json:"citation,omitempty"`
	CacheControl     *CacheControl  `json:"cache_control,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// Message is a role-tagged sequence of content parts.
type Message struct {
	Role             Role           `json:"role"`
	Parts            []ContentPart  `json:"parts"`
	Name             string         `json:"name,omitempty"`
	CacheControl     *CacheControl  `json:"cache_control,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// ToolDefinition describes a callable function or provider-native built-in tool.
type ToolDefinition struct {
	Type             string          `json:"type"` // "function", "custom", or native type (e.g., "web_search")
	Name             string          `json:"name"`
	Description      string          `json:"description,omitempty"`
	Parameters       json.RawMessage `json:"parameters,omitempty"`
	Strict           *bool           `json:"strict,omitempty"`
	CacheControl     *CacheControl   `json:"cache_control,omitempty"`
	NativeRaw        json.RawMessage `json:"native_raw,omitempty"` // opaque non-function tool definition
	OriginFormat     Format          `json:"origin_format,omitempty"`
	ProviderMetadata map[string]any  `json:"provider_metadata,omitempty"`
}

// ToolChoice controls how the model selects tools.
type ToolChoice struct {
	Mode             string            `json:"mode"` // "auto", "none", "required"/"any", "tool"
	ToolName         string            `json:"tool_name,omitempty"`
	ToolType         string            `json:"tool_type,omitempty"` // "function", "custom", or built-in type (e.g. "web_search_preview")
	AllowedTools     []string          `json:"allowed_tools,omitempty"`
	AllowedToolTypes map[string]string `json:"allowed_tool_types,omitempty"` // optional name -> kind ("function" | "custom" | built-in)
	DisableParallel  *bool             `json:"disable_parallel,omitempty"`
}

// ReasoningConfig configures chain-of-thought / extended thinking generation.
type ReasoningConfig struct {
	Enabled      *bool  `json:"enabled,omitempty"`
	Mode         string `json:"mode,omitempty"`    // "enabled", "adaptive", "disabled"
	Effort       string `json:"effort,omitempty"`  // "none", "minimal", "low", "medium", "high", "max", "xhigh"
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
	Summary      string `json:"summary,omitempty"` // "auto", "concise", "detailed"
	Display      string `json:"display,omitempty"` // e.g., "summarized", "omitted"
}

// ResponseFormat controls structured output formatting.
type ResponseFormat struct {
	Type        string          `json:"type"` // "text", "json_object", "json_schema"
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// GenerationConfig captures provider-neutral decoding parameters.
type GenerationConfig struct {
	MaxTokens           *int               `json:"max_tokens,omitempty"`
	Temperature         *float64           `json:"temperature,omitempty"`
	TopP                *float64           `json:"top_p,omitempty"`
	TopK                *int               `json:"top_k,omitempty"`
	StopSequences       []string           `json:"stop_sequences,omitempty"`
	FrequencyPenalty    *float64           `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64           `json:"presence_penalty,omitempty"`
	Seed                *int               `json:"seed,omitempty"`
	N                   *int               `json:"n,omitempty"`
	LogProbs            *bool              `json:"logprobs,omitempty"`
	TopLogProbs         *int               `json:"top_logprobs,omitempty"`
	LogitBias           map[string]float64 `json:"logit_bias,omitempty"`
	User                string             `json:"user,omitempty"`
	ServiceTier         string             `json:"service_tier,omitempty"`
	Verbosity           string             `json:"verbosity,omitempty"`
	Modalities          []string           `json:"modalities,omitempty"`
	Store               *bool              `json:"store,omitempty"`
	Truncation          string             `json:"truncation,omitempty"`
	PreviousResponseID  string             `json:"previous_response_id,omitempty"`
	PromptCacheKey      string             `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string            `json:"prompt_cache_retention,omitempty"`
}

// PassthroughItem preserves opaque provider-native items when source and target formats match.
type PassthroughItem struct {
	OriginFormat Format          `json:"origin_format"`
	Position     int             `json:"position"`
	Kind         string          `json:"kind,omitempty"`
	Payload      json.RawMessage `json:"payload"`
}

// Request is the provider-neutral intermediate representation of a chat/generation request.
type Request struct {
	Model              string                     `json:"model"`
	System             []ContentPart              `json:"system,omitempty"`
	Messages           []Message                  `json:"messages"`
	Tools              []ToolDefinition           `json:"tools,omitempty"`
	ToolChoice         *ToolChoice                `json:"tool_choice,omitempty"`
	ParallelToolCalls  *bool                      `json:"parallel_tool_calls,omitempty"`
	Generation         GenerationConfig           `json:"generation,omitempty"`
	Reasoning          *ReasoningConfig           `json:"reasoning,omitempty"`
	ResponseFormat     *ResponseFormat            `json:"response_format,omitempty"`
	Stream             bool                       `json:"stream,omitempty"`
	StreamIncludeUsage *bool                      `json:"stream_include_usage,omitempty"`
	Include            []string                   `json:"include,omitempty"`
	Metadata           map[string]any             `json:"metadata,omitempty"`
	CacheControl       *CacheControl              `json:"cache_control,omitempty"` // top-level prompt cache control (e.g. Anthropic automatic caching)
	PassthroughItems   []PassthroughItem          `json:"passthrough_items,omitempty"`
	ProviderExtensions map[string]json.RawMessage `json:"provider_extensions,omitempty"`
}

// Choice represents a single completion candidate in an IRResponse.
type Choice struct {
	Index            int            `json:"index"`
	Message          Message        `json:"message"`
	FinishReason     FinishReason   `json:"finish_reason,omitempty"`
	RawFinishReason  string         `json:"raw_finish_reason,omitempty"`
	StopSequence     string         `json:"stop_sequence,omitempty"`
	ProviderMetadata map[string]any `json:"provider_metadata,omitempty"`
}

// ResponseError captures a provider error carried inside a response envelope (e.g. Responses status="failed").
type ResponseError struct {
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Param   string `json:"param,omitempty"`
}

// Usage captures normalized token and built-in tool usage metrics.
type Usage struct {
	// InputTokens is the provider-reported prompt/input token count.
	// When InputExclusive is true (Anthropic semantics), InputTokens excludes CachedReadTokens
	// and CacheCreationTokens. When false (OpenAI/Google semantics), InputTokens includes them.
	InputTokens           int  `json:"input_tokens"`
	OutputTokens          int  `json:"output_tokens"`
	TotalTokens           int  `json:"total_tokens"`
	InputExclusive        bool `json:"input_exclusive,omitempty"`
	CachedReadTokens      int  `json:"cached_read_tokens,omitempty"`
	CacheCreationTokens   int  `json:"cache_creation_tokens,omitempty"`
	CacheCreation5mTokens int  `json:"cache_creation_5m_tokens,omitempty"`
	CacheCreation1hTokens int  `json:"cache_creation_1h_tokens,omitempty"`
	ReasoningTokens       int  `json:"reasoning_tokens,omitempty"`
	AudioInputTokens      int  `json:"audio_input_tokens,omitempty"`
	AudioOutputTokens     int  `json:"audio_output_tokens,omitempty"`
	ImageInputTokens      int  `json:"image_input_tokens,omitempty"`
	ImageOutputTokens     int  `json:"image_output_tokens,omitempty"`
	TextInputTokens       int  `json:"text_input_tokens,omitempty"`
	TextOutputTokens      int  `json:"text_output_tokens,omitempty"`
	WebSearchRequests     int    `json:"web_search_requests,omitempty"`
	FileSearchCalls       int    `json:"file_search_calls,omitempty"`
	HasImageGenCall       bool   `json:"has_image_gen_call,omitempty"`
	ImageGenQuality       string `json:"image_gen_quality,omitempty"`
	ImageGenSize          string `json:"image_gen_size,omitempty"`
}

// InclusiveInputTokens returns total input tokens including cached read and creation tokens.
func (u *Usage) InclusiveInputTokens() int {
	if u == nil {
		return 0
	}
	if u.InputExclusive {
		return u.InputTokens + u.CachedReadTokens + u.CacheCreationTokens
	}
	return u.InputTokens
}

// ExclusiveInputTokens returns uncached input tokens (Anthropic semantics).
func (u *Usage) ExclusiveInputTokens() int {
	if u == nil {
		return 0
	}
	if u.InputExclusive {
		return u.InputTokens
	}
	v := u.InputTokens - u.CachedReadTokens - u.CacheCreationTokens
	if v < 0 {
		return 0
	}
	return v
}

// Response is the provider-neutral intermediate representation of a non-streaming response.
type Response struct {
	ID                 string                     `json:"id"`
	Model              string                     `json:"model"`
	Created            int64                      `json:"created,omitempty"`
	Choices            []Choice                   `json:"choices"`
	Usage              *Usage                     `json:"usage,omitempty"`
	Error              *ResponseError             `json:"error,omitempty"`
	SystemFingerprint  string                     `json:"system_fingerprint,omitempty"`
	ServiceTier        string                     `json:"service_tier,omitempty"`
	PassthroughItems   []PassthroughItem          `json:"passthrough_items,omitempty"`
	ProviderExtensions map[string]json.RawMessage `json:"provider_extensions,omitempty"`
}

// StreamEventType enumerates the provider-neutral SSE stream event contract.
type StreamEventType string

const (
	EventStreamStart              StreamEventType = "stream_start"
	EventContentBlockStart        StreamEventType = "content_block_start"
	EventTextDelta                StreamEventType = "text_delta"
	EventImage                    StreamEventType = "image"
	EventReasoningDelta           StreamEventType = "reasoning_delta"
	EventReasoningSignatureDelta  StreamEventType = "reasoning_signature_delta"
	EventRefusalDelta             StreamEventType = "refusal_delta"
	EventToolCallStart            StreamEventType = "tool_call_start"
	EventToolCallDelta            StreamEventType = "tool_call_delta"
	EventContentBlockEnd          StreamEventType = "content_block_end"
	EventFinish                   StreamEventType = "finish"
	EventUsage                    StreamEventType = "usage"
	EventStreamEnd                StreamEventType = "stream_end"
	EventPassthrough              StreamEventType = "passthrough"
	EventError                    StreamEventType = "error"
)

// StreamEvent represents a single normalized streaming transition.
type StreamEvent struct {
	Type              StreamEventType `json:"type"`
	ChoiceIndex       int             `json:"choice_index,omitempty"`
	BlockIndex        int             `json:"block_index,omitempty"`
	BlockType         PartType        `json:"block_type,omitempty"`
	ResponseID        string          `json:"response_id,omitempty"`
	Model             string          `json:"model,omitempty"`
	Created           int64           `json:"created,omitempty"`
	Text              string          `json:"text,omitempty"`
	Image             *ImageSource    `json:"image,omitempty"`
	Reasoning         string          `json:"reasoning,omitempty"`
	Signature         string          `json:"signature,omitempty"`
	RedactedData      string          `json:"redacted_data,omitempty"`
	OriginFormat      Format          `json:"origin_format,omitempty"`
	Refusal           string          `json:"refusal,omitempty"`
	ToolCallID        string          `json:"tool_call_id,omitempty"`
	ToolCallItemID    string          `json:"tool_call_item_id,omitempty"`
	ToolName          string          `json:"tool_name,omitempty"`
	ToolCallIndex     int             `json:"tool_call_index,omitempty"`
	ToolArguments     string          `json:"tool_arguments,omitempty"`
	ToolKind          string          `json:"tool_kind,omitempty"`
	ThoughtSignature  string          `json:"thought_signature,omitempty"`
	FinishReason      FinishReason    `json:"finish_reason,omitempty"`
	RawFinishReason   string          `json:"raw_finish_reason,omitempty"`
	Usage             *Usage          `json:"usage,omitempty"`
	ErrorMessage      string          `json:"error_message,omitempty"`
	ErrorType         string          `json:"error_type,omitempty"`
	ErrorCode         string          `json:"error_code,omitempty"`
	PassthroughEvent  string          `json:"passthrough_event,omitempty"`
	PassthroughData   json.RawMessage `json:"passthrough_data,omitempty"`
	ProviderMetadata  map[string]any  `json:"provider_metadata,omitempty"`
}

// Warning records an explicit lossy translation decision rather than silently dropping data.
type Warning struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// JoinTextParts concatenates all text parts into a single string.
func JoinTextParts(parts []ContentPart, sep string) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type != PartText || p.Text == "" {
			continue
		}
		if b.Len() > 0 && sep != "" {
			b.WriteString(sep)
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// ParseDataURL splits a data:<media>;base64,<payload> URI into mediaType and raw base64 data.
func ParseDataURL(raw string) (mediaType string, data string, ok bool) {
	if !strings.HasPrefix(raw, "data:") {
		return "", "", false
	}
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return "", "", false
	}
	meta := raw[5:comma]
	payload := raw[comma+1:]
	if !strings.Contains(meta, ";base64") {
		return "", "", false
	}
	mediaType = strings.TrimSuffix(meta, ";base64")
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = mediaType[:idx]
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return mediaType, payload, true
}
