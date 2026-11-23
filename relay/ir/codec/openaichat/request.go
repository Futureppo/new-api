package openaichat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

type chatRequestWire struct {
	Model                string                 `json:"model"`
	Messages             []chatMessage          `json:"messages"`
	Tools                []json.RawMessage      `json:"tools,omitempty"`
	ToolChoice           json.RawMessage        `json:"tool_choice,omitempty"`
	Functions            []chatToolFunction     `json:"functions,omitempty"`
	FunctionCall         json.RawMessage        `json:"function_call,omitempty"`
	ParallelToolCalls    *bool                  `json:"parallel_tool_calls,omitempty"`
	MaxTokens            *int                   `json:"max_tokens,omitempty"`
	MaxCompletionTokens  *int                   `json:"max_completion_tokens,omitempty"`
	Temperature          *float64               `json:"temperature,omitempty"`
	TopP                 *float64               `json:"top_p,omitempty"`
	TopK                 *int                   `json:"top_k,omitempty"`
	Stop                 json.RawMessage        `json:"stop,omitempty"`
	FrequencyPenalty     *float64               `json:"frequency_penalty,omitempty"`
	PresencePenalty      *float64               `json:"presence_penalty,omitempty"`
	Seed                 *int                   `json:"seed,omitempty"`
	N                    *int                   `json:"n,omitempty"`
	LogProbs             *flexBool              `json:"logprobs,omitempty"`
	TopLogProbs          *int                   `json:"top_logprobs,omitempty"`
	LogitBias            map[string]float64     `json:"logit_bias,omitempty"`
	User                 string                 `json:"user,omitempty"`
	ServiceTier          string                 `json:"service_tier,omitempty"`
	Verbosity            string                 `json:"verbosity,omitempty"`
	Modalities           []string               `json:"modalities,omitempty"`
	Store                *bool                  `json:"store,omitempty"`
	PromptCacheKey       string                 `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string                 `json:"prompt_cache_retention,omitempty"`
	ReasoningEffort      string                 `json:"reasoning_effort,omitempty"`
	Reasoning            *chatReasoningParam    `json:"reasoning,omitempty"`
	ResponseFormat       *chatResponseFormat    `json:"response_format,omitempty"`
	Stream               *bool                  `json:"stream,omitempty"`
	StreamOptions        *chatStreamOptionsWire `json:"stream_options,omitempty"`
	Metadata             map[string]any         `json:"metadata,omitempty"`
}

type chatStreamOptionsWire struct {
	IncludeUsage       *bool           `json:"include_usage,omitempty"`
	IncludeObfuscation json.RawMessage `json:"include_obfuscation,omitempty"`
}

type chatReasoningParam struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens *int   `json:"max_tokens,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type chatResponseFormat struct {
	Type       string                  `json:"type"`
	JSONSchema *chatResponseJSONSchema `json:"json_schema,omitempty"`
}

type chatResponseJSONSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type chatMessage struct {
	Role             string            `json:"role"`
	Content          json.RawMessage   `json:"content,omitempty"`
	Name             string            `json:"name,omitempty"`
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	Reasoning        *string           `json:"reasoning,omitempty"`
	Refusal          *string           `json:"refusal,omitempty"`
	ToolCalls        []chatToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string            `json:"tool_call_id,omitempty"`
	FunctionCall     *chatFunctionCall `json:"function_call,omitempty"`
	CacheControl     *ir.CacheControl  `json:"cache_control,omitempty"`
	Annotations      []chatAnnotation  `json:"annotations,omitempty"`
	Audio            *chatMessageAudio `json:"audio,omitempty"`
}

type chatMessageAudio struct {
	ID         string `json:"id,omitempty"`
	Data       string `json:"data,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type chatContentPart struct {
	Type         string           `json:"type"`
	Text         string           `json:"text,omitempty"`
	Refusal      string           `json:"refusal,omitempty"`
	ImageURL     json.RawMessage  `json:"image_url,omitempty"`
	InputAudio   *chatInputAudio  `json:"input_audio,omitempty"`
	File         *chatFilePart    `json:"file,omitempty"`
	CacheControl *ir.CacheControl `json:"cache_control,omitempty"`
}

type chatImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type chatInputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type chatFilePart struct {
	FileID   string `json:"file_id,omitempty"`
	Filename string `json:"filename,omitempty"`
	FileData string `json:"file_data,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	URL      string `json:"url,omitempty"`
}

type chatToolCall struct {
	ID       string            `json:"id"`
	Type     string            `json:"type,omitempty"`
	Function *chatFunctionCall `json:"function,omitempty"`
	Custom   *chatCustomCall   `json:"custom,omitempty"`
}

type chatFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

type chatCustomCall struct {
	Name  string `json:"name,omitempty"`
	Input string `json:"input"`
}

type chatToolWire struct {
	Type         string            `json:"type"`
	Function     *chatToolFunction `json:"function,omitempty"`
	Custom       *chatToolCustom   `json:"custom,omitempty"`
	CacheControl *ir.CacheControl  `json:"cache_control,omitempty"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type chatToolCustom struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type chatAnnotation struct {
	Type        string           `json:"type,omitempty"`
	URLCitation *chatURLCitation `json:"url_citation,omitempty"`
	URL         string           `json:"url,omitempty"`
	Title       string           `json:"title,omitempty"`
	CitedText   string           `json:"cited_text,omitempty"`
	StartIndex  int              `json:"start_index,omitempty"`
	EndIndex    int              `json:"end_index,omitempty"`
}

type chatURLCitation struct {
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	CitedText  string `json:"cited_text,omitempty"`
	StartIndex int    `json:"start_index,omitempty"`
	EndIndex   int    `json:"end_index,omitempty"`
}

const (
	extStreamIncludeObfuscation = "stream_options.include_obfuscation"
	extUseMaxCompletionTokens   = "_use_max_completion_tokens"
)

var canonicalRequestKeys = map[string]struct{}{
	"model":                  {},
	"messages":               {},
	"tools":                  {},
	"tool_choice":            {},
	"functions":              {},
	"function_call":          {},
	"parallel_tool_calls":    {},
	"max_tokens":             {},
	"max_completion_tokens":  {},
	"temperature":            {},
	"top_p":                  {},
	"top_k":                  {},
	"stop":                   {},
	"frequency_penalty":      {},
	"presence_penalty":       {},
	"seed":                   {},
	"n":                      {},
	"logprobs":               {},
	"top_logprobs":           {},
	"logit_bias":             {},
	"user":                   {},
	"service_tier":           {},
	"verbosity":              {},
	"modalities":             {},
	"store":                  {},
	"prompt_cache_key":       {},
	"prompt_cache_retention": {},
	"reasoning_effort":       {},
	"reasoning":              {},
	"response_format":        {},
	"stream":                 {},
	"stream_options":         {},
	"metadata":               {},
}

var knownOpenAIExtensionKeys = map[string]struct{}{
	"extra_body":           {},
	"prompt_cache_options": {},
	"moderation":           {},
	"web_search_options":   {},
	"audio":                {},
	"prediction":           {},
	"safety_identifier":    {},
}

// DecodeRequest parses an OpenAI Chat Completions JSON request into the provider-neutral IR.
func (c *Codec) DecodeRequest(raw []byte, ctx *ir.ConversionContext) (*ir.Request, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("openaichat: empty request body")
	}

	var wire chatRequestWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("openaichat: decode request: %w", err)
	}

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(raw, &topLevel); err != nil {
		return nil, fmt.Errorf("openaichat: decode request envelope: %w", err)
	}

	req := &ir.Request{
		Model:             wire.Model,
		ParallelToolCalls: wire.ParallelToolCalls,
		Metadata:          wire.Metadata,
	}

	if wire.Stream != nil {
		req.Stream = *wire.Stream
	}
	if wire.StreamOptions != nil {
		if wire.StreamOptions.IncludeUsage != nil {
			v := *wire.StreamOptions.IncludeUsage
			req.StreamIncludeUsage = &v
		}
		if !isNullOrEmptyJSON(wire.StreamOptions.IncludeObfuscation) {
			ensureExtensions(req)[extStreamIncludeObfuscation] = cloneBytes(wire.StreamOptions.IncludeObfuscation)
		}
	}

	// Generation parameters. max_completion_tokens takes precedence over max_tokens and preserves explicit 0.
	gen := ir.GenerationConfig{
		Temperature:          wire.Temperature,
		TopP:                 wire.TopP,
		TopK:                 wire.TopK,
		FrequencyPenalty:     wire.FrequencyPenalty,
		PresencePenalty:      wire.PresencePenalty,
		Seed:                 wire.Seed,
		N:                    wire.N,
		TopLogProbs:          wire.TopLogProbs,
		LogitBias:            wire.LogitBias,
		User:                 wire.User,
		ServiceTier:          wire.ServiceTier,
		Verbosity:            wire.Verbosity,
		Modalities:           wire.Modalities,
		Store:                wire.Store,
		PromptCacheKey:       wire.PromptCacheKey,
		PromptCacheRetention: wire.PromptCacheRetention,
	}
	if wire.MaxCompletionTokens != nil {
		v := *wire.MaxCompletionTokens
		gen.MaxTokens = &v
		ensureExtensions(req)[extUseMaxCompletionTokens] = json.RawMessage("true")
	} else if wire.MaxTokens != nil {
		v := *wire.MaxTokens
		gen.MaxTokens = &v
	}
	if wire.LogProbs != nil {
		b := bool(*wire.LogProbs)
		gen.LogProbs = &b
	}
	if !isNullOrEmptyJSON(wire.Stop) {
		stops, err := decodeStopSequences(wire.Stop)
		if err != nil {
			return nil, fmt.Errorf("openaichat: decode stop: %w", err)
		}
		gen.StopSequences = stops
	}
	req.Generation = gen

	// Reasoning configuration (top-level reasoning_effort and/or OpenRouter/compat reasoning object).
	if wire.ReasoningEffort != "" || wire.Reasoning != nil {
		rc := &ir.ReasoningConfig{}
		if wire.Reasoning != nil {
			rc.Effort = wire.Reasoning.Effort
			rc.BudgetTokens = wire.Reasoning.MaxTokens
			rc.Enabled = wire.Reasoning.Enabled
			rc.Summary = wire.Reasoning.Summary
			if wire.Reasoning.Enabled != nil {
				if *wire.Reasoning.Enabled {
					rc.Mode = "enabled"
				} else {
					rc.Mode = "disabled"
				}
			}
		}
		if wire.ReasoningEffort != "" {
			rc.Effort = wire.ReasoningEffort
		}
		req.Reasoning = rc
	}

	// Response format.
	if wire.ResponseFormat != nil && wire.ResponseFormat.Type != "" {
		rf := &ir.ResponseFormat{
			Type: wire.ResponseFormat.Type,
		}
		if wire.ResponseFormat.JSONSchema != nil {
			rf.Name = wire.ResponseFormat.JSONSchema.Name
			rf.Description = wire.ResponseFormat.JSONSchema.Description
			rf.Schema = cloneBytes(wire.ResponseFormat.JSONSchema.Schema)
			rf.Strict = wire.ResponseFormat.JSONSchema.Strict
		}
		req.ResponseFormat = rf
	}

	// Tools & tool_choice (including legacy functions / function_call).
	if len(wire.Tools) > 0 {
		tools, err := decodeTools(wire.Tools)
		if err != nil {
			return nil, err
		}
		req.Tools = tools
	} else if len(wire.Functions) > 0 {
		tools := make([]ir.ToolDefinition, 0, len(wire.Functions))
		for _, fn := range wire.Functions {
			tools = append(tools, ir.ToolDefinition{
				Type:         "function",
				Name:         fn.Name,
				Description:  fn.Description,
				Parameters:   cloneBytes(fn.Parameters),
				Strict:       fn.Strict,
				OriginFormat: ir.FormatOpenAIChat,
			})
		}
		req.Tools = tools
	}
	if !isNullOrEmptyJSON(wire.ToolChoice) {
		tc, err := decodeToolChoice(wire.ToolChoice)
		if err != nil {
			return nil, err
		}
		req.ToolChoice = tc
	} else if !isNullOrEmptyJSON(wire.FunctionCall) {
		tc, err := decodeLegacyFunctionCallChoice(wire.FunctionCall)
		if err != nil {
			return nil, err
		}
		req.ToolChoice = tc
	}

	// Messages: extract leading pure-text system/developer messages into req.System
	// when they carry no message-level special fields (name / message cache_control).
	leadingSystem := true
	pendingLegacyCalls := make(map[string][]string)
	lastLegacyCallID := make(map[string]string)
	for _, msg := range wire.Messages {
		role := normalizeRole(msg.Role)
		if leadingSystem && (role == ir.RoleSystem || role == ir.RoleDeveloper) {
			if parts, ok, err := tryExtractLeadingSystemParts(msg, role, ctx); err != nil {
				return nil, err
			} else if ok {
				req.System = append(req.System, parts...)
				continue
			}
		}
		leadingSystem = false

		irMsg, err := decodeChatMessageWithLegacy(msg, pendingLegacyCalls, lastLegacyCallID, ctx)
		if err != nil {
			return nil, err
		}
		req.Messages = append(req.Messages, irMsg)
	}

	// Preserve non-canonical provider extension fields.
	for k, v := range topLevel {
		if _, canonical := canonicalRequestKeys[k]; canonical {
			continue
		}
		if isNullOrEmptyJSON(v) {
			continue
		}
		ensureExtensions(req)[k] = cloneBytes(v)
	}

	return req, nil
}

// EncodeRequest converts an IR Request into an OpenAI Chat Completions JSON request.
func (c *Codec) EncodeRequest(req *ir.Request, ctx *ir.ConversionContext) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("openaichat: nil request")
	}

	out := make(map[string]any, 16)
	if req.Model != "" {
		out["model"] = req.Model
	}

	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if len(req.System) > 0 {
		sysMsgs := encodeSystemParts(req.System, ctx)
		messages = append(messages, sysMsgs...)
	}

	// Buffer images extracted from tool results across contiguous tool messages
	// so all role:"tool" messages remain immediately after assistant tool_calls.
	var pendingToolImages []map[string]any
	flushToolImages := func() {
		if len(pendingToolImages) == 0 {
			return
		}
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": pendingToolImages,
		})
		pendingToolImages = nil
	}

	for _, msg := range req.Messages {
		// Separate tool results from non-tool-result parts (e.g. Anthropic mixes tool_result + user text in RoleUser).
		var toolResults []ir.ContentPart
		var otherParts []ir.ContentPart
		for _, p := range msg.Parts {
			if p.Type == ir.PartToolResult && p.ToolResult != nil {
				toolResults = append(toolResults, p)
			} else {
				otherParts = append(otherParts, p)
			}
		}

		if len(toolResults) > 0 {
			for _, trPart := range toolResults {
				toolMsg, imgs := encodeToolResultMessage(trPart, msg.Name, msg.CacheControl, ctx)
				messages = append(messages, toolMsg)
				pendingToolImages = append(pendingToolImages, imgs...)
			}
			if len(otherParts) == 0 {
				continue
			}
		}

		// Guard against an empty/malformed RoleTool message emitting role:"tool" without tool_call_id.
		if msg.Role == ir.RoleTool {
			if len(otherParts) == 0 {
				continue
			}
			// Convert stray non-tool parts to a user follow-up rather than an invalid role:"tool".
			msg.Role = ir.RoleUser
		}

		// When transitioning from tool results to a user message, merge pending tool images into
		// that user message rather than emitting two consecutive role:"user" messages.
		if msg.Role == ir.RoleUser && len(pendingToolImages) > 0 {
			encodedParts := append([]map[string]any{}, pendingToolImages...)
			pendingToolImages = nil
			encodedParts = append(encodedParts, encodeContentPartsArray(otherParts, ctx)...)
			userMsg := map[string]any{
				"role":    "user",
				"content": encodedParts,
			}
			if msg.Name != "" {
				userMsg["name"] = msg.Name
			}
			if msg.CacheControl != nil && shouldEmitChatCacheControl(ctx) {
				userMsg["cache_control"] = msg.CacheControl
			}
			messages = append(messages, userMsg)
			continue
		}

		// Leaving a contiguous run of tool messages -> emit any buffered vision tool images first.
		flushToolImages()

		encodedMsg := encodeNonToolMessage(msg.Role, otherParts, msg.Name, msg.CacheControl, ctx)
		if encodedMsg != nil {
			messages = append(messages, encodedMsg)
		}
	}
	flushToolImages()

	out["messages"] = messages

	// Tools (and record custom tool names for tool_choice fidelity).
	customToolNames := make(map[string]bool)
	for _, t := range req.Tools {
		if t.Type == "custom" && t.Name != "" {
			customToolNames[t.Name] = true
		}
	}
	if len(req.Tools) > 0 {
		encodedTools := encodeTools(req.Tools, ctx)
		if len(encodedTools) > 0 {
			out["tools"] = encodedTools
		}
	}

	// Tool choice.
	if req.ToolChoice != nil {
		if tc := encodeToolChoice(req.ToolChoice, customToolNames); tc != nil {
			out["tool_choice"] = tc
		}
	}

	// Parallel tool calls (explicit field or derived from ToolChoice.DisableParallel).
	if req.ParallelToolCalls != nil {
		out["parallel_tool_calls"] = *req.ParallelToolCalls
	} else if req.ToolChoice != nil && req.ToolChoice.DisableParallel != nil {
		out["parallel_tool_calls"] = !*req.ToolChoice.DisableParallel
	}

	// Generation config.
	gen := req.Generation
	if gen.MaxTokens != nil {
		useMaxCompletion := ctx != nil && ctx.GetBoolOption("use_max_completion_tokens", false)
		if !useMaxCompletion && ctx != nil && ctx.ShouldPreserveMetadata(ir.FormatOpenAIChat) {
			if rawFlag, ok := req.ProviderExtensions[extUseMaxCompletionTokens]; ok && string(bytes.TrimSpace(rawFlag)) == "true" {
				useMaxCompletion = true
			}
		}
		if useMaxCompletion {
			out["max_completion_tokens"] = *gen.MaxTokens
		} else {
			out["max_tokens"] = *gen.MaxTokens
		}
	}
	if gen.Temperature != nil {
		out["temperature"] = *gen.Temperature
	}
	if gen.TopP != nil {
		out["top_p"] = *gen.TopP
	}
	if gen.TopK != nil {
		ctx.AddWarning("unsupported_field", "generation.top_k", "OpenAI Chat Completions does not support top_k")
	}
	if len(gen.StopSequences) > 0 {
		out["stop"] = gen.StopSequences
	}
	if gen.FrequencyPenalty != nil {
		out["frequency_penalty"] = *gen.FrequencyPenalty
	}
	if gen.PresencePenalty != nil {
		out["presence_penalty"] = *gen.PresencePenalty
	}
	if gen.Seed != nil {
		out["seed"] = *gen.Seed
	}
	if gen.N != nil {
		out["n"] = *gen.N
	}
	if gen.LogProbs != nil {
		out["logprobs"] = *gen.LogProbs
	}
	if gen.TopLogProbs != nil {
		out["top_logprobs"] = *gen.TopLogProbs
	}
	if len(gen.LogitBias) > 0 {
		out["logit_bias"] = gen.LogitBias
	}
	if gen.User != "" {
		out["user"] = gen.User
	}
	if gen.ServiceTier != "" {
		out["service_tier"] = gen.ServiceTier
	}
	if gen.Verbosity != "" {
		out["verbosity"] = gen.Verbosity
	}
	if len(gen.Modalities) > 0 {
		out["modalities"] = gen.Modalities
	}
	if gen.Store != nil {
		out["store"] = *gen.Store
	}
	if gen.PromptCacheKey != "" {
		out["prompt_cache_key"] = gen.PromptCacheKey
	}
	if gen.PromptCacheRetention != "" {
		out["prompt_cache_retention"] = gen.PromptCacheRetention
	}
	if gen.Truncation != "" {
		ctx.AddWarning("unsupported_field", "generation.truncation", "OpenAI Chat Completions does not support truncation")
	}
	if gen.PreviousResponseID != "" {
		ctx.AddWarning("unsupported_field", "generation.previous_response_id", "OpenAI Chat Completions does not support previous_response_id")
	}
	if len(req.Include) > 0 {
		ctx.AddWarning("unsupported_field", "include", "OpenAI Chat Completions does not support include")
	}

	// Reasoning config.
	if req.Reasoning != nil {
		if effort := encodeReasoningEffort(req.Reasoning, ctx); effort != "" {
			out["reasoning_effort"] = effort
		}
	}

	// Response format.
	if req.ResponseFormat != nil && req.ResponseFormat.Type != "" {
		rf := map[string]any{
			"type": req.ResponseFormat.Type,
		}
		if req.ResponseFormat.Type == "json_schema" {
			name := req.ResponseFormat.Name
			if name == "" {
				name = "response"
			}
			js := map[string]any{
				"name": name,
			}
			if req.ResponseFormat.Description != "" {
				js["description"] = req.ResponseFormat.Description
			}
			if !isNullOrEmptyJSON(req.ResponseFormat.Schema) {
				js["schema"] = ir.NormalizeJSONSchemaTypes(req.ResponseFormat.Schema)
			}
			if req.ResponseFormat.Strict != nil {
				js["strict"] = *req.ResponseFormat.Strict
			}
			rf["json_schema"] = js
		}
		out["response_format"] = rf
	}

	// Stream and stream_options.
	isStream := req.Stream || (ctx != nil && ctx.GetBoolOption("stream", false))
	if isStream {
		out["stream"] = true
		disableStreamOptions := ctx != nil && ctx.GetBoolOption("disable_stream_options", false)
		if !disableStreamOptions {
			streamOpts := make(map[string]any)
			if req.StreamIncludeUsage == nil || *req.StreamIncludeUsage {
				streamOpts["include_usage"] = true
			}
			if obf, ok := req.ProviderExtensions[extStreamIncludeObfuscation]; ok && !isNullOrEmptyJSON(obf) {
				streamOpts["include_obfuscation"] = obf
			}
			if len(streamOpts) > 0 {
				out["stream_options"] = streamOpts
			}
		}
	}

	// Metadata.
	if len(req.Metadata) > 0 {
		out["metadata"] = req.Metadata
	}

	// Provider extensions.
	if len(req.ProviderExtensions) > 0 {
		preserveAll := ctx != nil && (ctx.MetadataMode == ir.MetadataPreserve || ctx.SourceFormat == ir.FormatOpenAIChat)
		for k, v := range req.ProviderExtensions {
			if k == extStreamIncludeObfuscation || k == extUseMaxCompletionTokens || strings.HasPrefix(k, "_") || isNullOrEmptyJSON(v) {
				continue
			}
			if _, exists := out[k]; exists {
				continue
			}
			_, isKnownExt := knownOpenAIExtensionKeys[k]
			if isKnownExt || preserveAll {
				out[k] = v
			}
		}
	}

	return json.Marshal(out)
}

func ensureExtensions(req *ir.Request) map[string]json.RawMessage {
	if req.ProviderExtensions == nil {
		req.ProviderExtensions = make(map[string]json.RawMessage)
	}
	return req.ProviderExtensions
}

func normalizeRole(role string) ir.Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system":
		return ir.RoleSystem
	case "developer":
		return ir.RoleDeveloper
	case "user":
		return ir.RoleUser
	case "assistant":
		return ir.RoleAssistant
	case "tool", "function":
		return ir.RoleTool
	default:
		return ir.Role(role)
	}
}

func tryExtractLeadingSystemParts(msg chatMessage, role ir.Role, ctx *ir.ConversionContext) ([]ir.ContentPart, bool, error) {
	if msg.Name != "" || msg.CacheControl != nil || len(msg.ToolCalls) > 0 {
		return nil, false, nil
	}
	parts, err := decodeMessageContent(msg.Content, false, ctx)
	if err != nil {
		return nil, false, err
	}
	if len(parts) == 0 {
		return nil, true, nil
	}
	for _, p := range parts {
		if p.Type != ir.PartText {
			return nil, false, nil
		}
	}
	if role == ir.RoleDeveloper {
		for i := range parts {
			if parts[i].ProviderMetadata == nil {
				parts[i].ProviderMetadata = make(map[string]any, 1)
			}
			parts[i].ProviderMetadata["role"] = "developer"
		}
	}
	return parts, true, nil
}

func decodeChatMessage(msg chatMessage, ctx *ir.ConversionContext) (ir.Message, error) {
	return decodeChatMessageWithLegacy(msg, nil, nil, ctx)
}

func decodeChatMessageWithLegacy(msg chatMessage, pendingLegacyCalls map[string][]string, lastLegacyCallID map[string]string, ctx *ir.ConversionContext) (ir.Message, error) {
	role := normalizeRole(msg.Role)
	irMsg := ir.Message{
		Role:         role,
		Name:         msg.Name,
		CacheControl: msg.CacheControl,
	}

	if role == ir.RoleTool {
		callID := msg.ToolCallID
		if callID == "" && msg.Name != "" && pendingLegacyCalls != nil {
			if q := pendingLegacyCalls[msg.Name]; len(q) > 0 {
				callID = q[0]
				pendingLegacyCalls[msg.Name] = q[1:]
			} else if lastLegacyCallID != nil {
				callID = lastLegacyCallID[msg.Name]
			}
		}
		tr := &ir.ToolResult{
			ToolCallID: callID,
			Name:       msg.Name,
		}
		if !isNullOrEmptyJSON(msg.Content) {
			trimmed := bytes.TrimSpace(msg.Content)
			if trimmed[0] == '"' {
				var s string
				if err := json.Unmarshal(trimmed, &s); err != nil {
					return irMsg, fmt.Errorf("openaichat: decode tool content: %w", err)
				}
				tr.Content = s
			} else {
				parts, err := decodeMessageContent(trimmed, false, ctx)
				if err != nil {
					return irMsg, err
				}
				allPlainText := len(parts) > 0
				for _, p := range parts {
					if p.Type != ir.PartText || p.CacheControl != nil {
						allPlainText = false
						break
					}
				}
				if allPlainText {
					tr.Content = ir.JoinTextParts(parts, "\n")
				} else {
					tr.Parts = parts
				}
			}
		}
		irMsg.Parts = []ir.ContentPart{{
			Type:         ir.PartToolResult,
			ToolResult:   tr,
			CacheControl: msg.CacheControl,
		}}
		return irMsg, nil
	}

	// Reasoning content (assistant).
	if rText := extractReasoningText(msg.ReasoningContent, msg.Reasoning); rText != "" {
		irMsg.Parts = append(irMsg.Parts, ir.ContentPart{
			Type: ir.PartReasoning,
			Reasoning: &ir.Reasoning{
				Text:         rText,
				OriginFormat: ir.FormatOpenAIChat,
			},
		})
	}

	// Main content.
	skipEmptyText := role == ir.RoleAssistant && (len(msg.ToolCalls) > 0 || msg.FunctionCall != nil || (msg.Refusal != nil && *msg.Refusal != "") || msg.Audio != nil)
	contentParts, err := decodeMessageContent(msg.Content, skipEmptyText, ctx)
	if err != nil {
		return irMsg, err
	}
	irMsg.Parts = append(irMsg.Parts, contentParts...)

	// Assistant audio output (transcript + optional raw audio data).
	if msg.Audio != nil {
		if msg.Audio.Transcript != "" && len(contentParts) == 0 {
			irMsg.Parts = append(irMsg.Parts, ir.ContentPart{
				Type: ir.PartText,
				Text: msg.Audio.Transcript,
			})
		}
		if msg.Audio.Data != "" {
			irMsg.Parts = append(irMsg.Parts, ir.ContentPart{
				Type: ir.PartAudio,
				Audio: &ir.AudioSource{
					Data:   msg.Audio.Data,
					Format: "wav",
				},
			})
		}
	}

	// Refusal.
	if msg.Refusal != nil && *msg.Refusal != "" {
		irMsg.Parts = append(irMsg.Parts, ir.ContentPart{
			Type:    ir.PartRefusal,
			Refusal: *msg.Refusal,
		})
	}

	// Annotations -> citations.
	for _, ann := range msg.Annotations {
		if cit := decodeAnnotation(ann); cit != nil {
			irMsg.Parts = append(irMsg.Parts, ir.ContentPart{
				Type:     ir.PartCitation,
				Citation: cit,
			})
		}
	}

	// Tool calls (and legacy function_call).
	for _, tc := range decodeToolCalls(msg.ToolCalls, msg.FunctionCall, pendingLegacyCalls, lastLegacyCallID) {
		irMsg.Parts = append(irMsg.Parts, ir.ContentPart{
			Type:     ir.PartToolCall,
			ToolCall: tc,
		})
	}

	return irMsg, nil
}

func extractReasoningText(reasoningContent, reasoning *string) string {
	if reasoningContent != nil && *reasoningContent != "" {
		return *reasoningContent
	}
	if reasoning != nil && *reasoning != "" {
		return *reasoning
	}
	return ""
}

func decodeMessageContent(raw json.RawMessage, skipEmptyString bool, ctx *ir.ConversionContext) ([]ir.ContentPart, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, fmt.Errorf("openaichat: decode string content: %w", err)
		}
		if s == "" && skipEmptyString {
			return nil, nil
		}
		return []ir.ContentPart{{Type: ir.PartText, Text: s}}, nil
	}

	if trimmed[0] != '[' {
		// Fallback for non-standard scalar content.
		return []ir.ContentPart{{Type: ir.PartText, Text: string(trimmed)}}, nil
	}

	var wireParts []chatContentPart
	if err := json.Unmarshal(trimmed, &wireParts); err != nil {
		return nil, fmt.Errorf("openaichat: decode array content: %w", err)
	}

	parts := make([]ir.ContentPart, 0, len(wireParts))
	for _, wp := range wireParts {
		switch wp.Type {
		case "text", "input_text", "output_text":
			parts = append(parts, ir.ContentPart{
				Type:         ir.PartText,
				Text:         wp.Text,
				CacheControl: wp.CacheControl,
			})
		case "refusal":
			refusal := wp.Refusal
			if refusal == "" {
				refusal = wp.Text
			}
			parts = append(parts, ir.ContentPart{
				Type:         ir.PartRefusal,
				Refusal:      refusal,
				CacheControl: wp.CacheControl,
			})
		case "image_url":
			img, err := decodeImageURLPart(wp.ImageURL)
			if err != nil {
				return nil, err
			}
			if img != nil {
				parts = append(parts, ir.ContentPart{
					Type:         ir.PartImage,
					Image:        img,
					CacheControl: wp.CacheControl,
				})
			}
		case "input_audio":
			if wp.InputAudio != nil {
				parts = append(parts, ir.ContentPart{
					Type: ir.PartAudio,
					Audio: &ir.AudioSource{
						Data:      wp.InputAudio.Data,
						Format:    wp.InputAudio.Format,
						MediaType: audioFormatToMediaType(wp.InputAudio.Format),
					},
					CacheControl: wp.CacheControl,
				})
			}
		case "file":
			if wp.File != nil {
				fs := &ir.FileSource{
					FileID:   wp.File.FileID,
					Filename: wp.File.Filename,
					URL:      wp.File.FileURL,
				}
				if fs.URL == "" {
					fs.URL = wp.File.URL
				}
				if wp.File.FileData != "" {
					if mt, data, ok := ir.ParseDataURL(wp.File.FileData); ok {
						fs.MediaType = mt
						fs.Data = data
					} else {
						fs.Data = wp.File.FileData
					}
				}
				parts = append(parts, ir.ContentPart{
					Type:         ir.PartFile,
					File:         fs,
					CacheControl: wp.CacheControl,
				})
			}
		default:
			ctx.AddWarning("unsupported_content_part", "messages.content", fmt.Sprintf("unsupported OpenAI Chat content part type %q", wp.Type))
		}
	}
	return parts, nil
}

func decodeImageURLPart(raw json.RawMessage) (*ir.ImageSource, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var imgURL, detail string
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &imgURL); err != nil {
			return nil, fmt.Errorf("openaichat: decode image_url string: %w", err)
		}
	} else {
		var obj chatImageURL
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, fmt.Errorf("openaichat: decode image_url object: %w", err)
		}
		imgURL = obj.URL
		detail = obj.Detail
	}
	if strings.TrimSpace(imgURL) == "" {
		return nil, nil
	}
	if mediaType, data, ok := ir.ParseDataURL(imgURL); ok {
		return &ir.ImageSource{
			Data:      data,
			MediaType: mediaType,
			Detail:    detail,
		}, nil
	}
	return &ir.ImageSource{
		URL:    imgURL,
		Detail: detail,
	}, nil
}

func decodeToolCalls(calls []chatToolCall, legacy *chatFunctionCall, pendingLegacyCalls map[string][]string, lastLegacyCallID map[string]string) []*ir.ToolCall {
	out := make([]*ir.ToolCall, 0, len(calls))
	for _, tc := range calls {
		if tc.Type == "custom" && tc.Custom != nil {
			out = append(out, &ir.ToolCall{
				ID:        tc.ID,
				Name:      tc.Custom.Name,
				Arguments: tc.Custom.Input,
				Kind:      "custom",
			})
			continue
		}
		kind := tc.Type
		if kind == "" {
			kind = "function"
		}
		var name, args string
		if tc.Function != nil {
			name = tc.Function.Name
			args = tc.Function.Arguments
		}
		out = append(out, &ir.ToolCall{
			ID:        tc.ID,
			Name:      name,
			Arguments: args,
			Kind:      kind,
		})
	}
	if len(out) == 0 && legacy != nil {
		id := ir.SynthesizeToolCallID("call_legacy")
		if legacy.Name != "" {
			if pendingLegacyCalls != nil {
				pendingLegacyCalls[legacy.Name] = append(pendingLegacyCalls[legacy.Name], id)
			}
			if lastLegacyCallID != nil {
				lastLegacyCallID[legacy.Name] = id
			}
		}
		out = append(out, &ir.ToolCall{
			ID:        id,
			Name:      legacy.Name,
			Arguments: legacy.Arguments,
			Kind:      "function",
		})
	}
	return out
}

func decodeAnnotation(ann chatAnnotation) *ir.Citation {
	if ann.URLCitation != nil {
		return &ir.Citation{
			URL:        ann.URLCitation.URL,
			Title:      ann.URLCitation.Title,
			CitedText:  ann.URLCitation.CitedText,
			StartIndex: ann.URLCitation.StartIndex,
			EndIndex:   ann.URLCitation.EndIndex,
		}
	}
	if ann.URL != "" || ann.Title != "" || ann.CitedText != "" {
		return &ir.Citation{
			URL:        ann.URL,
			Title:      ann.Title,
			CitedText:  ann.CitedText,
			StartIndex: ann.StartIndex,
			EndIndex:   ann.EndIndex,
		}
	}
	return nil
}

func decodeStopSequences(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		return []string{s}, nil
	}
	var list []string
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func decodeTools(rawTools []json.RawMessage) ([]ir.ToolDefinition, error) {
	tools := make([]ir.ToolDefinition, 0, len(rawTools))
	for _, rt := range rawTools {
		if isNullOrEmptyJSON(rt) {
			continue
		}
		var tw chatToolWire
		if err := json.Unmarshal(rt, &tw); err != nil {
			return nil, fmt.Errorf("openaichat: decode tool: %w", err)
		}
		switch tw.Type {
		case "function":
			td := ir.ToolDefinition{
				Type:         "function",
				CacheControl: tw.CacheControl,
				OriginFormat: ir.FormatOpenAIChat,
			}
			if tw.Function != nil {
				td.Name = tw.Function.Name
				td.Description = tw.Function.Description
				td.Parameters = cloneBytes(tw.Function.Parameters)
				td.Strict = tw.Function.Strict
			}
			tools = append(tools, td)
		case "custom":
			td := ir.ToolDefinition{
				Type:         "custom",
				CacheControl: tw.CacheControl,
				NativeRaw:    cloneBytes(rt),
				OriginFormat: ir.FormatOpenAIChat,
			}
			if tw.Custom != nil {
				td.Name = tw.Custom.Name
				td.Description = tw.Custom.Description
			}
			tools = append(tools, td)
		default:
			tools = append(tools, ir.ToolDefinition{
				Type:         tw.Type,
				Name:         tw.Type,
				CacheControl: tw.CacheControl,
				NativeRaw:    cloneBytes(rt),
				OriginFormat: ir.FormatOpenAIChat,
			})
		}
	}
	return tools, nil
}

func decodeToolChoice(raw json.RawMessage) (*ir.ToolChoice, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var mode string
		if err := json.Unmarshal(trimmed, &mode); err != nil {
			return nil, fmt.Errorf("openaichat: decode tool_choice string: %w", err)
		}
		return &ir.ToolChoice{Mode: mode}, nil
	}

	var obj struct {
		Type     string `json:"type"`
		Function *struct {
			Name string `json:"name"`
		} `json:"function,omitempty"`
		Custom *struct {
			Name string `json:"name"`
		} `json:"custom,omitempty"`
		AllowedTools *struct {
			Mode  string `json:"mode"`
			Tools []struct {
				Type     string `json:"type"`
				Function *struct {
					Name string `json:"name"`
				} `json:"function,omitempty"`
				Custom *struct {
					Name string `json:"name"`
				} `json:"custom,omitempty"`
			} `json:"tools"`
		} `json:"allowed_tools,omitempty"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("openaichat: decode tool_choice object: %w", err)
	}

	switch obj.Type {
	case "function":
		tc := &ir.ToolChoice{Mode: "tool", ToolType: "function"}
		if obj.Function != nil {
			tc.ToolName = obj.Function.Name
		}
		return tc, nil
	case "custom":
		tc := &ir.ToolChoice{Mode: "tool", ToolType: "custom"}
		if obj.Custom != nil {
			tc.ToolName = obj.Custom.Name
		}
		return tc, nil
	case "allowed_tools":
		tc := &ir.ToolChoice{Mode: "auto"}
		if obj.AllowedTools != nil {
			if obj.AllowedTools.Mode != "" {
				tc.Mode = obj.AllowedTools.Mode
			}
			for _, t := range obj.AllowedTools.Tools {
				if t.Custom != nil && t.Custom.Name != "" {
					tc.AllowedTools = append(tc.AllowedTools, t.Custom.Name)
					if tc.AllowedToolTypes == nil {
						tc.AllowedToolTypes = make(map[string]string)
					}
					tc.AllowedToolTypes[t.Custom.Name] = "custom"
				} else if t.Function != nil && t.Function.Name != "" {
					tc.AllowedTools = append(tc.AllowedTools, t.Function.Name)
				}
			}
		}
		return tc, nil
	default:
		if obj.Type != "" {
			return &ir.ToolChoice{Mode: obj.Type}, nil
		}
		return nil, nil
	}
}

func decodeLegacyFunctionCallChoice(raw json.RawMessage) (*ir.ToolChoice, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var mode string
		if err := json.Unmarshal(trimmed, &mode); err != nil {
			return nil, fmt.Errorf("openaichat: decode function_call string: %w", err)
		}
		return &ir.ToolChoice{Mode: mode}, nil
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("openaichat: decode function_call object: %w", err)
	}
	if obj.Name != "" {
		return &ir.ToolChoice{Mode: "tool", ToolType: "function", ToolName: obj.Name}, nil
	}
	return nil, nil
}

func shouldEmitChatCacheControl(ctx *ir.ConversionContext) bool {
	if ctx == nil {
		return false
	}
	if ctx.GetBoolOption("preserve_cache_control", false) {
		return true
	}
	return ctx.SourceFormat == ir.FormatOpenAIChat && ctx.ShouldPreserveMetadata(ir.FormatOpenAIChat)
}

func encodeSystemParts(parts []ir.ContentPart, ctx *ir.ConversionContext) []map[string]any {
	defaultRole := "system"
	if ctx != nil {
		if optRole := ctx.GetStringOption("system_role", ""); optRole == "developer" || optRole == "system" {
			defaultRole = optRole
		}
	}

	type sysGroup struct {
		role  string
		parts []ir.ContentPart
	}
	var groups []sysGroup
	for _, p := range parts {
		role := defaultRole
		if p.ProviderMetadata != nil {
			if r, ok := p.ProviderMetadata["role"].(string); ok && (r == "developer" || r == "system") {
				role = r
			}
		}
		if len(groups) == 0 || groups[len(groups)-1].role != role {
			groups = append(groups, sysGroup{role: role, parts: []ir.ContentPart{p}})
		} else {
			groups[len(groups)-1].parts = append(groups[len(groups)-1].parts, p)
		}
	}

	emitCC := shouldEmitChatCacheControl(ctx)
	msgs := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		hasSpecial := false
		for _, p := range g.parts {
			if p.Type != ir.PartText || (emitCC && p.CacheControl != nil) {
				hasSpecial = true
				break
			}
		}
		if !hasSpecial {
			msgs = append(msgs, map[string]any{
				"role":    g.role,
				"content": ir.JoinTextParts(g.parts, "\n\n"),
			})
			continue
		}
		encodedParts := encodeContentPartsArray(g.parts, ctx)
		msgs = append(msgs, map[string]any{
			"role":    g.role,
			"content": encodedParts,
		})
	}
	return msgs
}

func encodeToolResultMessage(trPart ir.ContentPart, msgName string, msgCache *ir.CacheControl, ctx *ir.ConversionContext) (map[string]any, []map[string]any) {
	tr := trPart.ToolResult
	emitCC := shouldEmitChatCacheControl(ctx)
	var textParts []string
	var structuredParts []map[string]any
	hasPartCC := false
	var imageParts []map[string]any

	for _, p := range tr.Parts {
		switch p.Type {
		case ir.PartText:
			if p.Text != "" {
				textParts = append(textParts, p.Text)
				item := map[string]any{"type": "text", "text": p.Text}
				if emitCC && p.CacheControl != nil {
					item["cache_control"] = p.CacheControl
					hasPartCC = true
				}
				structuredParts = append(structuredParts, item)
			}
		case ir.PartImage:
			if encoded := encodeImagePart(p.Image, p.CacheControl, emitCC); encoded != nil {
				imageParts = append(imageParts, encoded)
			}
		default:
			ctx.AddWarning("unsupported_tool_result_part", "tool_result.parts", fmt.Sprintf("tool result part type %q cannot be represented inline in OpenAI Chat tool message", p.Type))
		}
	}

	toolMsg := map[string]any{
		"role":         "tool",
		"tool_call_id": tr.ToolCallID,
	}
	if hasPartCC && len(structuredParts) > 0 {
		toolMsg["content"] = structuredParts
	} else {
		var content string
		if len(textParts) > 0 {
			if tr.Content != "" && tr.Content != strings.Join(textParts, "\n") {
				content = tr.Content + "\n" + strings.Join(textParts, "\n")
			} else {
				content = strings.Join(textParts, "\n")
			}
		} else {
			content = tr.Content
		}
		toolMsg["content"] = content
	}
	name := tr.Name
	if name == "" {
		name = msgName
	}
	if name != "" {
		toolMsg["name"] = name
	}
	cc := trPart.CacheControl
	if cc == nil {
		cc = msgCache
	}
	if emitCC && cc != nil {
		toolMsg["cache_control"] = cc
	}

	return toolMsg, imageParts
}

func encodeNonToolMessage(role ir.Role, parts []ir.ContentPart, name string, msgCache *ir.CacheControl, ctx *ir.ConversionContext) map[string]any {
	wireRole := string(role)
	if wireRole == "" {
		wireRole = "user"
	}

	var contentParts []ir.ContentPart
	var reasoningTexts []string
	var refusalTexts []string
	var toolCalls []map[string]any

	for _, p := range parts {
		switch p.Type {
		case ir.PartReasoning:
			if p.Reasoning == nil {
				continue
			}
			if p.Reasoning.Text != "" {
				reasoningTexts = append(reasoningTexts, p.Reasoning.Text)
			}
			if p.Reasoning.Signature != "" && !ctx.ShouldPreserveMetadata(p.Reasoning.OriginFormat) {
				ctx.AddWarning("dropped_reasoning_signature", "reasoning.signature", "OpenAI Chat Completions does not support opaque cross-provider reasoning signatures")
			}
			if p.Reasoning.RedactedData != "" && !ctx.ShouldPreserveMetadata(p.Reasoning.OriginFormat) {
				ctx.AddWarning("dropped_reasoning_redacted", "reasoning.redacted_data", "OpenAI Chat Completions does not support opaque cross-provider redacted reasoning blobs")
			}
		case ir.PartRefusal:
			if p.Refusal != "" {
				refusalTexts = append(refusalTexts, p.Refusal)
			}
		case ir.PartToolCall:
			if p.ToolCall == nil {
				continue
			}
			if p.ToolCall.ThoughtSignature != "" && !ctx.ShouldPreserveMetadata(ir.FormatGoogleGenAI) {
				ctx.AddWarning("dropped_thought_signature", "tool_call.thought_signature", "OpenAI Chat Completions does not support Google thought signatures")
			}
			toolCalls = append(toolCalls, encodeToolCallMap(p.ToolCall))
		case ir.PartCitation:
			// Citations are response-side annotations; skip silently in request history.
		default:
			contentParts = append(contentParts, p)
		}
	}

	emitCC := shouldEmitChatCacheControl(ctx)
	msg := map[string]any{
		"role": wireRole,
	}
	if name != "" {
		msg["name"] = name
	}
	if emitCC && msgCache != nil {
		msg["cache_control"] = msgCache
	}

	if len(contentParts) > 0 {
		if len(contentParts) == 1 && contentParts[0].Type == ir.PartText && (!emitCC || contentParts[0].CacheControl == nil) {
			msg["content"] = contentParts[0].Text
		} else if encoded := encodeContentPartsArray(contentParts, ctx); len(encoded) > 0 {
			msg["content"] = encoded
		} else if len(toolCalls) == 0 && len(refusalTexts) == 0 {
			msg["content"] = ""
		}
	} else if len(toolCalls) == 0 && len(refusalTexts) == 0 {
		msg["content"] = ""
	}

	if len(reasoningTexts) > 0 {
		msg["reasoning_content"] = strings.Join(reasoningTexts, "\n")
	}
	if len(refusalTexts) > 0 {
		msg["refusal"] = strings.Join(refusalTexts, "\n")
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}

	return msg
}

func encodeContentPartsArray(parts []ir.ContentPart, ctx *ir.ConversionContext) []map[string]any {
	emitCC := shouldEmitChatCacheControl(ctx)
	out := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case ir.PartText:
			item := map[string]any{
				"type": "text",
				"text": p.Text,
			}
			if emitCC && p.CacheControl != nil {
				item["cache_control"] = p.CacheControl
			}
			out = append(out, item)
		case ir.PartImage:
			if item := encodeImagePart(p.Image, p.CacheControl, emitCC); item != nil {
				out = append(out, item)
			}
		case ir.PartAudio:
			if p.Audio == nil {
				continue
			}
			if p.Audio.Data == "" {
				if p.Audio.URL != "" {
					ctx.AddWarning("unsupported_field", "audio.url", "OpenAI Chat Completions input_audio requires inline base64 data rather than URL")
				}
				continue
			}
			format := p.Audio.Format
			if format == "" {
				format = mediaTypeToAudioFormat(p.Audio.MediaType)
			}
			if format == "" {
				format = "wav"
			}
			item := map[string]any{
				"type": "input_audio",
				"input_audio": map[string]any{
					"data":   p.Audio.Data,
					"format": format,
				},
			}
			if emitCC && p.CacheControl != nil {
				item["cache_control"] = p.CacheControl
			}
			out = append(out, item)
		case ir.PartFile:
			if p.File == nil {
				continue
			}
			if p.File.Text != "" && p.File.FileID == "" && p.File.Data == "" {
				item := map[string]any{
					"type": "text",
					"text": p.File.Text,
				}
				if emitCC && p.CacheControl != nil {
					item["cache_control"] = p.CacheControl
				}
				out = append(out, item)
				continue
			}
			fileObj := make(map[string]any)
			if p.File.FileID != "" {
				fileObj["file_id"] = p.File.FileID
			}
			if p.File.Filename != "" {
				fileObj["filename"] = p.File.Filename
			}
			if p.File.Data != "" {
				if p.File.MediaType != "" && !strings.HasPrefix(p.File.Data, "data:") {
					fileObj["file_data"] = fmt.Sprintf("data:%s;base64,%s", p.File.MediaType, p.File.Data)
				} else {
					fileObj["file_data"] = p.File.Data
				}
			}
			if p.File.URL != "" && p.File.FileID == "" && p.File.Data == "" {
				// OpenAI Chat Completions file content parts do not support file_url.
				ctx.AddWarning("unsupported_field", "file.url", "OpenAI Chat Completions file part does not support file_url; omitted")
				continue
			}
			if len(fileObj) == 0 {
				continue
			}
			item := map[string]any{
				"type": "file",
				"file": fileObj,
			}
			if emitCC && p.CacheControl != nil {
				item["cache_control"] = p.CacheControl
			}
			out = append(out, item)
		case ir.PartRefusal:
			item := map[string]any{
				"type":    "refusal",
				"refusal": p.Refusal,
			}
			out = append(out, item)
		}
	}
	return out
}

func encodeImagePart(img *ir.ImageSource, cc *ir.CacheControl, emitCC bool) map[string]any {
	if img == nil {
		return nil
	}
	imgURL := img.URL
	if imgURL == "" && img.Data != "" {
		mt := img.MediaType
		if mt == "" {
			mt = "image/png"
		}
		imgURL = fmt.Sprintf("data:%s;base64,%s", mt, img.Data)
	}
	if imgURL == "" {
		return nil
	}
	urlObj := map[string]any{
		"url": imgURL,
	}
	if img.Detail != "" {
		urlObj["detail"] = img.Detail
	}
	item := map[string]any{
		"type":      "image_url",
		"image_url": urlObj,
	}
	if emitCC && cc != nil {
		item["cache_control"] = cc
	}
	return item
}

func encodeToolCallMap(tc *ir.ToolCall) map[string]any {
	if tc.Kind == "custom" {
		return map[string]any{
			"id":   tc.ID,
			"type": "custom",
			"custom": map[string]any{
				"name":  tc.Name,
				"input": tc.Arguments,
			},
		}
	}
	args := tc.Arguments
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	return map[string]any{
		"id":   tc.ID,
		"type": "function",
		"function": map[string]any{
			"name":      tc.Name,
			"arguments": args,
		},
	}
}

func encodeTools(tools []ir.ToolDefinition, ctx *ir.ConversionContext) []any {
	emitCC := shouldEmitChatCacheControl(ctx)
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		if t.Type == "" || t.Type == "function" {
			fn := map[string]any{
				"name": t.Name,
			}
			if t.Description != "" {
				fn["description"] = t.Description
			}
			if !isNullOrEmptyJSON(t.Parameters) {
				fn["parameters"] = ir.NormalizeJSONSchemaTypes(t.Parameters)
			}
			if t.Strict != nil {
				fn["strict"] = *t.Strict
			}
			item := map[string]any{
				"type":     "function",
				"function": fn,
			}
			if emitCC && t.CacheControl != nil {
				item["cache_control"] = t.CacheControl
			}
			out = append(out, item)
			continue
		}

		if t.Type == "custom" {
			if len(t.NativeRaw) > 0 && (t.OriginFormat == ir.FormatOpenAIChat || t.OriginFormat == ir.FormatUnknown) {
				out = append(out, t.NativeRaw)
				continue
			}
			customObj := map[string]any{
				"name": t.Name,
			}
			if t.Description != "" {
				customObj["description"] = t.Description
			}
			if t.ProviderMetadata != nil {
				if format, ok := t.ProviderMetadata["format"]; ok && format != nil {
					customObj["format"] = format
				}
			}
			item := map[string]any{
				"type":   "custom",
				"custom": customObj,
			}
			if emitCC && t.CacheControl != nil {
				item["cache_control"] = t.CacheControl
			}
			out = append(out, item)
			continue
		}

		if len(t.NativeRaw) > 0 && (t.OriginFormat == ir.FormatOpenAIChat || t.OriginFormat == ir.FormatUnknown || ctx.ShouldPreserveMetadata(t.OriginFormat)) {
			out = append(out, t.NativeRaw)
			continue
		}

		ctx.AddWarning("unsupported_tool", "tools."+t.Name, fmt.Sprintf("OpenAI Chat Completions does not support native tool type %q from %q", t.Type, t.OriginFormat))
	}
	return out
}

func encodeToolChoice(tc *ir.ToolChoice, customToolNames map[string]bool) any {
	if tc == nil {
		return nil
	}
	isCustom := func(name string) bool {
		if tc.AllowedToolTypes != nil && tc.AllowedToolTypes[name] == "custom" {
			return true
		}
		return customToolNames[name]
	}
	if len(tc.AllowedTools) > 0 {
		mode := tc.Mode
		if mode == "" || mode == "tool" {
			mode = "auto"
		} else if mode == "any" {
			mode = "required"
		}
		allowed := make([]map[string]any, 0, len(tc.AllowedTools))
		for _, name := range tc.AllowedTools {
			if isCustom(name) {
				allowed = append(allowed, map[string]any{
					"type":   "custom",
					"custom": map[string]any{"name": name},
				})
			} else {
				allowed = append(allowed, map[string]any{
					"type":     "function",
					"function": map[string]any{"name": name},
				})
			}
		}
		return map[string]any{
			"type": "allowed_tools",
			"allowed_tools": map[string]any{
				"mode":  mode,
				"tools": allowed,
			},
		}
	}

	encodeNamedTool := func(name string) map[string]any {
		if tc.ToolType == "custom" || customToolNames[name] {
			return map[string]any{
				"type":   "custom",
				"custom": map[string]any{"name": name},
			}
		}
		return map[string]any{
			"type":     "function",
			"function": map[string]any{"name": name},
		}
	}

	switch tc.Mode {
	case "none", "auto", "required":
		return tc.Mode
	case "any":
		return "required"
	case "tool":
		if tc.ToolName != "" {
			return encodeNamedTool(tc.ToolName)
		}
		return "required"
	default:
		if tc.ToolName != "" {
			return encodeNamedTool(tc.ToolName)
		}
		if tc.Mode != "" {
			return tc.Mode
		}
		return nil
	}
}

func encodeReasoningEffort(rc *ir.ReasoningConfig, ctx *ir.ConversionContext) string {
	if rc == nil {
		return ""
	}
	// Disabled must take precedence over any residual Effort field.
	if rc.Mode == "disabled" || (rc.Enabled != nil && !*rc.Enabled) {
		return "none"
	}
	if rc.Effort != "" {
		effort := strings.ToLower(strings.TrimSpace(rc.Effort))
		if effort == "max" {
			ctx.AddWarning("adjusted_reasoning_effort", "reasoning.effort", "mapped Anthropic effort 'max' to OpenAI reasoning_effort 'high'")
			return "high"
		}
		return effort
	}
	if rc.BudgetTokens != nil && *rc.BudgetTokens > 0 {
		ctx.AddWarning("lossy_mapping", "reasoning.budget_tokens", "mapped reasoning budget_tokens to OpenAI reasoning_effort")
		switch {
		case *rc.BudgetTokens <= 2048:
			return "low"
		case *rc.BudgetTokens <= 8192:
			return "medium"
		default:
			return "high"
		}
	}
	if rc.Mode == "enabled" || rc.Mode == "adaptive" || (rc.Enabled != nil && *rc.Enabled) {
		return "medium"
	}
	return ""
}
