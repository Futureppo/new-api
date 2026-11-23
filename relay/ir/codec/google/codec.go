package google

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

// Codec implements bidirectional translation between Google GenAI (Gemini API)
// wire JSON/SSE and the provider-neutral IR.
type Codec struct{}

// New returns a new Google GenAI Codec instance.
func New() *Codec {
	return &Codec{}
}

var _ ir.Codec = (*Codec)(nil)

// Format returns ir.FormatGoogleGenAI.
func (c *Codec) Format() ir.Format {
	return ir.FormatGoogleGenAI
}

// --- Shared Google GenAI wire types (camelCase + snake_case decode support) ---

type googleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []googlePart `json:"parts"`
}

// UnmarshalJSON accepts either a standard content object or a plain text string.
func (c *googleContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return err
		}
		if s != "" {
			c.Parts = []googlePart{{Text: &s}}
		}
		return nil
	}
	type alias googleContent
	var aux alias
	if err := json.Unmarshal(trimmed, &aux); err != nil {
		return err
	}
	*c = googleContent(aux)
	return nil
}

type googleInlineData struct {
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

func (d *googleInlineData) UnmarshalJSON(data []byte) error {
	type alias googleInlineData
	var aux struct {
		alias
		MimeTypeSnake string `json:"mime_type,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*d = googleInlineData(aux.alias)
	if d.MimeType == "" && aux.MimeTypeSnake != "" {
		d.MimeType = aux.MimeTypeSnake
	}
	return nil
}

type googleFileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri,omitempty"`
}

func (d *googleFileData) UnmarshalJSON(data []byte) error {
	type alias googleFileData
	var aux struct {
		alias
		MimeTypeSnake string `json:"mime_type,omitempty"`
		FileURISnake  string `json:"file_uri,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*d = googleFileData(aux.alias)
	if d.MimeType == "" && aux.MimeTypeSnake != "" {
		d.MimeType = aux.MimeTypeSnake
	}
	if d.FileURI == "" && aux.FileURISnake != "" {
		d.FileURI = aux.FileURISnake
	}
	return nil
}

type googleFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type googleFunctionResponse struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
	Parts    []googlePart    `json:"parts,omitempty"`
}

type googleExecutableCode struct {
	Language string `json:"language,omitempty"`
	Code     string `json:"code,omitempty"`
}

type googleCodeExecutionResult struct {
	Outcome string `json:"outcome,omitempty"`
	Output  string `json:"output,omitempty"`
}

type googlePart struct {
	Text                *string                    `json:"text,omitempty"`
	Thought             bool                       `json:"thought,omitempty"`
	ThoughtSignature    string                     `json:"thoughtSignature,omitempty"`
	InlineData          *googleInlineData          `json:"inlineData,omitempty"`
	FileData            *googleFileData            `json:"fileData,omitempty"`
	FunctionCall        *googleFunctionCall        `json:"functionCall,omitempty"`
	FunctionResponse    *googleFunctionResponse    `json:"functionResponse,omitempty"`
	ExecutableCode      *googleExecutableCode      `json:"executableCode,omitempty"`
	CodeExecutionResult *googleCodeExecutionResult `json:"codeExecutionResult,omitempty"`
}

func (p *googlePart) UnmarshalJSON(data []byte) error {
	type alias googlePart
	var aux struct {
		alias
		ThoughtSignatureSnake    string                     `json:"thought_signature,omitempty"`
		InlineDataSnake          *googleInlineData          `json:"inline_data,omitempty"`
		FileDataSnake            *googleFileData            `json:"file_data,omitempty"`
		FunctionCallSnake        *googleFunctionCall        `json:"function_call,omitempty"`
		FunctionResponseSnake    *googleFunctionResponse    `json:"function_response,omitempty"`
		ExecutableCodeSnake      *googleExecutableCode      `json:"executable_code,omitempty"`
		CodeExecutionResultSnake *googleCodeExecutionResult `json:"code_execution_result,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*p = googlePart(aux.alias)
	if p.ThoughtSignature == "" && aux.ThoughtSignatureSnake != "" {
		p.ThoughtSignature = aux.ThoughtSignatureSnake
	}
	if p.InlineData == nil && aux.InlineDataSnake != nil {
		p.InlineData = aux.InlineDataSnake
	}
	if p.FileData == nil && aux.FileDataSnake != nil {
		p.FileData = aux.FileDataSnake
	}
	if p.FunctionCall == nil && aux.FunctionCallSnake != nil {
		p.FunctionCall = aux.FunctionCallSnake
	}
	if p.FunctionResponse == nil && aux.FunctionResponseSnake != nil {
		p.FunctionResponse = aux.FunctionResponseSnake
	}
	if p.ExecutableCode == nil && aux.ExecutableCodeSnake != nil {
		p.ExecutableCode = aux.ExecutableCodeSnake
	}
	if p.CodeExecutionResult == nil && aux.CodeExecutionResultSnake != nil {
		p.CodeExecutionResult = aux.CodeExecutionResultSnake
	}
	return nil
}

type googleModalityTokenCount struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

func (m *googleModalityTokenCount) UnmarshalJSON(data []byte) error {
	type alias googleModalityTokenCount
	var aux struct {
		alias
		TokenCountSnake *int `json:"token_count,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*m = googleModalityTokenCount(aux.alias)
	if m.TokenCount == 0 && aux.TokenCountSnake != nil {
		m.TokenCount = *aux.TokenCountSnake
	}
	return nil
}

type googleUsageMetadata struct {
	PromptTokenCount           int                        `json:"promptTokenCount,omitempty"`
	ToolUsePromptTokenCount    int                        `json:"toolUsePromptTokenCount,omitempty"`
	CandidatesTokenCount       int                        `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount            int                        `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount         int                        `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount    int                        `json:"cachedContentTokenCount,omitempty"`
	PromptTokensDetails        []googleModalityTokenCount `json:"promptTokensDetails,omitempty"`
	ToolUsePromptTokensDetails []googleModalityTokenCount `json:"toolUsePromptTokensDetails,omitempty"`
	CandidatesTokensDetails    []googleModalityTokenCount `json:"candidatesTokensDetails,omitempty"`
}

func (u *googleUsageMetadata) UnmarshalJSON(data []byte) error {
	type alias googleUsageMetadata
	var aux struct {
		alias
		PromptTokenCountSnake           *int                       `json:"prompt_token_count,omitempty"`
		ToolUsePromptTokenCountSnake    *int                       `json:"tool_use_prompt_token_count,omitempty"`
		CandidatesTokenCountSnake       *int                       `json:"candidates_token_count,omitempty"`
		TotalTokenCountSnake            *int                       `json:"total_token_count,omitempty"`
		ThoughtsTokenCountSnake         *int                       `json:"thoughts_token_count,omitempty"`
		CachedContentTokenCountSnake    *int                       `json:"cached_content_token_count,omitempty"`
		PromptTokensDetailsSnake        []googleModalityTokenCount `json:"prompt_tokens_details,omitempty"`
		ToolUsePromptTokensDetailsSnake []googleModalityTokenCount `json:"tool_use_prompt_tokens_details,omitempty"`
		CandidatesTokensDetailsSnake    []googleModalityTokenCount `json:"candidates_tokens_details,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*u = googleUsageMetadata(aux.alias)
	if u.PromptTokenCount == 0 && aux.PromptTokenCountSnake != nil {
		u.PromptTokenCount = *aux.PromptTokenCountSnake
	}
	if u.ToolUsePromptTokenCount == 0 && aux.ToolUsePromptTokenCountSnake != nil {
		u.ToolUsePromptTokenCount = *aux.ToolUsePromptTokenCountSnake
	}
	if u.CandidatesTokenCount == 0 && aux.CandidatesTokenCountSnake != nil {
		u.CandidatesTokenCount = *aux.CandidatesTokenCountSnake
	}
	if u.TotalTokenCount == 0 && aux.TotalTokenCountSnake != nil {
		u.TotalTokenCount = *aux.TotalTokenCountSnake
	}
	if u.ThoughtsTokenCount == 0 && aux.ThoughtsTokenCountSnake != nil {
		u.ThoughtsTokenCount = *aux.ThoughtsTokenCountSnake
	}
	if u.CachedContentTokenCount == 0 && aux.CachedContentTokenCountSnake != nil {
		u.CachedContentTokenCount = *aux.CachedContentTokenCountSnake
	}
	if len(u.PromptTokensDetails) == 0 && len(aux.PromptTokensDetailsSnake) > 0 {
		u.PromptTokensDetails = aux.PromptTokensDetailsSnake
	}
	if len(u.ToolUsePromptTokensDetails) == 0 && len(aux.ToolUsePromptTokensDetailsSnake) > 0 {
		u.ToolUsePromptTokensDetails = aux.ToolUsePromptTokensDetailsSnake
	}
	if len(u.CandidatesTokensDetails) == 0 && len(aux.CandidatesTokensDetailsSnake) > 0 {
		u.CandidatesTokensDetails = aux.CandidatesTokensDetailsSnake
	}
	return nil
}

type googleCandidate struct {
	Index        int            `json:"index"`
	Content      *googleContent `json:"content,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
}

func (c *googleCandidate) UnmarshalJSON(data []byte) error {
	type alias googleCandidate
	var aux struct {
		alias
		FinishReasonSnake string `json:"finish_reason,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*c = googleCandidate(aux.alias)
	if c.FinishReason == "" && aux.FinishReasonSnake != "" {
		c.FinishReason = aux.FinishReasonSnake
	}
	return nil
}

type googlePromptFeedback struct {
	BlockReason string `json:"blockReason,omitempty"`
}

func (f *googlePromptFeedback) UnmarshalJSON(data []byte) error {
	type alias googlePromptFeedback
	var aux struct {
		alias
		BlockReasonSnake string `json:"block_reason,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*f = googlePromptFeedback(aux.alias)
	if f.BlockReason == "" && aux.BlockReasonSnake != "" {
		f.BlockReason = aux.BlockReasonSnake
	}
	return nil
}

type googleError struct {
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
	Status  string `json:"status,omitempty"`
}

type googleResponse struct {
	ResponseID     string                `json:"responseId,omitempty"`
	ModelVersion   string                `json:"modelVersion,omitempty"`
	Candidates     []googleCandidate     `json:"candidates,omitempty"`
	PromptFeedback *googlePromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  *googleUsageMetadata  `json:"usageMetadata,omitempty"`
	Error          *googleError          `json:"error,omitempty"`
}

func (r *googleResponse) UnmarshalJSON(data []byte) error {
	type alias googleResponse
	var aux struct {
		alias
		ResponseIDSnake     string                `json:"response_id,omitempty"`
		ModelVersionSnake   string                `json:"model_version,omitempty"`
		PromptFeedbackSnake *googlePromptFeedback `json:"prompt_feedback,omitempty"`
		UsageMetadataSnake  *googleUsageMetadata  `json:"usage_metadata,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = googleResponse(aux.alias)
	if r.ResponseID == "" && aux.ResponseIDSnake != "" {
		r.ResponseID = aux.ResponseIDSnake
	}
	if r.ModelVersion == "" && aux.ModelVersionSnake != "" {
		r.ModelVersion = aux.ModelVersionSnake
	}
	if r.PromptFeedback == nil && aux.PromptFeedbackSnake != nil {
		r.PromptFeedback = aux.PromptFeedbackSnake
	}
	if r.UsageMetadata == nil && aux.UsageMetadataSnake != nil {
		r.UsageMetadata = aux.UsageMetadataSnake
	}
	return nil
}

// --- Shared helper functions ---

func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

func floatToIntPtr(f *float64) *int {
	if f == nil {
		return nil
	}
	v := int(math.Round(*f))
	return &v
}

// normalizeJSONArgs converts raw Gemini functionCall.args JSON into a compact JSON string,
// defaulting to "{}" when empty or null.
func normalizeJSONArgs(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "{}"
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, trimmed); err == nil {
		return compacted.String()
	}
	return string(trimmed)
}

// parseToolArgsToObject parses a JSON arguments string into a valid JSON object RawMessage
// suitable for Google's functionCall.args (which must always be a JSON object).
func parseToolArgsToObject(args string) json.RawMessage {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("{}")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		if obj == nil {
			return json.RawMessage("{}")
		}
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, []byte(trimmed)); err == nil {
			return json.RawMessage(compacted.Bytes())
		}
		if b, err := json.Marshal(obj); err == nil {
			return json.RawMessage(b)
		}
	}
	// If arguments is a valid non-object JSON value or raw string, wrap in an object.
	var anyVal any
	if err := json.Unmarshal([]byte(trimmed), &anyVal); err == nil && anyVal != nil {
		if b, err := json.Marshal(map[string]any{"input": anyVal}); err == nil {
			return json.RawMessage(b)
		}
	}
	if b, err := json.Marshal(map[string]any{"raw": trimmed}); err == nil {
		return json.RawMessage(b)
	}
	return json.RawMessage("{}")
}

func mapGoogleFinishReason(raw string, hasToolCalls bool) ir.FinishReason {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "", "FINISH_REASON_UNSPECIFIED":
		if hasToolCalls {
			return ir.FinishToolCalls
		}
		return ""
	case "STOP":
		if hasToolCalls {
			return ir.FinishToolCalls
		}
		return ir.FinishStop
	case "MAX_TOKENS":
		return ir.FinishLength
	case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII",
		"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "LANGUAGE":
		return ir.FinishContentFilter
	case "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS":
		return ir.FinishError
	case "OTHER":
		return ir.FinishError
	default:
		if hasToolCalls {
			return ir.FinishToolCalls
		}
		return ir.FinishStop
	}
}

func encodeGoogleFinishReason(fr ir.FinishReason, raw string) string {
	switch fr {
	case ir.FinishStop, ir.FinishToolCalls:
		return "STOP"
	case ir.FinishLength:
		return "MAX_TOKENS"
	case ir.FinishContentFilter:
		upper := strings.ToUpper(strings.TrimSpace(raw))
		switch upper {
		case "SAFETY", "RECITATION", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII",
			"IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "LANGUAGE":
			return upper
		default:
			return "SAFETY"
		}
	case ir.FinishError:
		upper := strings.ToUpper(strings.TrimSpace(raw))
		if upper == "MALFORMED_FUNCTION_CALL" || upper == "UNEXPECTED_TOOL_CALL" || upper == "TOO_MANY_TOOL_CALLS" {
			return upper
		}
		return "OTHER"
	default:
		return ""
	}
}

func decodeUsageMetadata(meta *googleUsageMetadata) *ir.Usage {
	if meta == nil {
		return nil
	}
	u := &ir.Usage{
		InputTokens:      meta.PromptTokenCount + meta.ToolUsePromptTokenCount,
		OutputTokens:     meta.CandidatesTokenCount + meta.ThoughtsTokenCount,
		TotalTokens:      meta.TotalTokenCount,
		ReasoningTokens:  meta.ThoughtsTokenCount,
		CachedReadTokens: meta.CachedContentTokenCount,
		InputExclusive:   false,
	}
	for _, list := range [][]googleModalityTokenCount{meta.PromptTokensDetails, meta.ToolUsePromptTokensDetails} {
		for _, d := range list {
			switch strings.ToUpper(d.Modality) {
			case "AUDIO":
				u.AudioInputTokens += d.TokenCount
			case "IMAGE":
				u.ImageInputTokens += d.TokenCount
			case "TEXT":
				u.TextInputTokens += d.TokenCount
			}
		}
	}
	for _, d := range meta.CandidatesTokensDetails {
		switch strings.ToUpper(d.Modality) {
		case "AUDIO":
			u.AudioOutputTokens += d.TokenCount
		case "IMAGE":
			u.ImageOutputTokens += d.TokenCount
		case "TEXT":
			u.TextOutputTokens += d.TokenCount
		}
	}
	if u.TotalTokens == 0 && (u.InputTokens > 0 || u.OutputTokens > 0) {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	return u
}

func encodeUsageMetadata(u *ir.Usage) *googleUsageMetadata {
	if u == nil {
		return nil
	}
	promptTokens := u.InclusiveInputTokens()
	thoughtsTokens := u.ReasoningTokens
	candidatesTokens := u.OutputTokens - thoughtsTokens
	if candidatesTokens < 0 {
		candidatesTokens = 0
	}
	totalTokens := u.TotalTokens
	if totalTokens < promptTokens+u.OutputTokens {
		totalTokens = promptTokens + u.OutputTokens
	}
	meta := &googleUsageMetadata{
		PromptTokenCount:        promptTokens,
		CandidatesTokenCount:    candidatesTokens,
		ThoughtsTokenCount:      thoughtsTokens,
		CachedContentTokenCount: u.CachedReadTokens,
		TotalTokenCount:         totalTokens,
	}
	if u.TextInputTokens > 0 {
		meta.PromptTokensDetails = append(meta.PromptTokensDetails, googleModalityTokenCount{Modality: "TEXT", TokenCount: u.TextInputTokens})
	}
	if u.ImageInputTokens > 0 {
		meta.PromptTokensDetails = append(meta.PromptTokensDetails, googleModalityTokenCount{Modality: "IMAGE", TokenCount: u.ImageInputTokens})
	}
	if u.AudioInputTokens > 0 {
		meta.PromptTokensDetails = append(meta.PromptTokensDetails, googleModalityTokenCount{Modality: "AUDIO", TokenCount: u.AudioInputTokens})
	}
	if u.TextOutputTokens > 0 {
		meta.CandidatesTokensDetails = append(meta.CandidatesTokensDetails, googleModalityTokenCount{Modality: "TEXT", TokenCount: u.TextOutputTokens})
	}
	if u.ImageOutputTokens > 0 {
		meta.CandidatesTokensDetails = append(meta.CandidatesTokensDetails, googleModalityTokenCount{Modality: "IMAGE", TokenCount: u.ImageOutputTokens})
	}
	if u.AudioOutputTokens > 0 {
		meta.CandidatesTokensDetails = append(meta.CandidatesTokensDetails, googleModalityTokenCount{Modality: "AUDIO", TokenCount: u.AudioOutputTokens})
	}
	return meta
}

func audioFormatFromMime(mime string) string {
	lower := strings.ToLower(strings.TrimSpace(mime))
	switch lower {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "audio/aac":
		return "aac"
	case "audio/webm":
		return "webm"
	default:
		if strings.HasPrefix(lower, "audio/") {
			return strings.TrimPrefix(lower, "audio/")
		}
		return ""
	}
}

func mimeFromAudioFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "mp3", "mpeg":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "pcm16", "pcm":
		return "audio/pcm"
	case "":
		return "audio/wav"
	default:
		return "audio/" + strings.ToLower(strings.TrimSpace(format))
	}
}

func formatExecutableCodeText(ec *googleExecutableCode) string {
	if ec == nil {
		return ""
	}
	lang := strings.ToLower(strings.TrimSpace(ec.Language))
	return fmt.Sprintf("```%s\n%s\n```", lang, ec.Code)
}

func formatCodeExecutionResultText(res *googleCodeExecutionResult) string {
	if res == nil {
		return ""
	}
	return fmt.Sprintf("```output\n%s\n```", res.Output)
}

func isNullOrEmptyRaw(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}
