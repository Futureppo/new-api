package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

const (
	defaultAnthropicMaxTokens      = 8192
	minAnthropicThinkingBudget     = 1024
	thinkingMaxTokensHeadroom      = 4096
	placeholderNonEmptyTextContent = "..."
)

type wireToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse *bool  `json:"disable_parallel_tool_use,omitempty"`
}

type wireThinking struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

type wireOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type wireToolDefDec struct {
	Type         string            `json:"type,omitempty"`
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	InputSchema  json.RawMessage   `json:"input_schema,omitempty"`
	Strict       *bool             `json:"strict,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireToolDefEnc struct {
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	InputSchema  json.RawMessage   `json:"input_schema"`
	Strict       *bool             `json:"strict,omitempty"`
	CacheControl *wireCacheControl `json:"cache_control,omitempty"`
}

type wireMessageDec struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type wireMessageEnc struct {
	Role    string `json:"role"`
	Content []any  `json:"content"`
}

type wireRequestDec struct {
	Model         string            `json:"model"`
	MaxTokens     *int              `json:"max_tokens,omitempty"`
	System        json.RawMessage   `json:"system,omitempty"`
	Messages      []wireMessageDec  `json:"messages"`
	Tools         []json.RawMessage `json:"tools,omitempty"`
	ToolChoice    *wireToolChoice   `json:"tool_choice,omitempty"`
	Thinking      *wireThinking     `json:"thinking,omitempty"`
	OutputConfig  *wireOutputConfig `json:"output_config,omitempty"`
	Stream        *bool             `json:"stream,omitempty"`
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"top_p,omitempty"`
	TopK          *int              `json:"top_k,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
	ServiceTier   string            `json:"service_tier,omitempty"`
	CacheControl  *wireCacheControl `json:"cache_control,omitempty"`
}

type wireRequestEnc struct {
	Model         string            `json:"model"`
	MaxTokens     int               `json:"max_tokens"`
	System        any               `json:"system,omitempty"`
	Messages      []wireMessageEnc  `json:"messages"`
	Tools         []any             `json:"tools,omitempty"`
	ToolChoice    *wireToolChoice   `json:"tool_choice,omitempty"`
	Thinking      *wireThinking     `json:"thinking,omitempty"`
	OutputConfig  *wireOutputConfig `json:"output_config,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
	Temperature   *float64          `json:"temperature,omitempty"`
	TopP          *float64          `json:"top_p,omitempty"`
	TopK          *int              `json:"top_k,omitempty"`
	StopSequences []string          `json:"stop_sequences,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
	ServiceTier   string            `json:"service_tier,omitempty"`
	CacheControl  *wireCacheControl `json:"cache_control,omitempty"`
}

// DecodeRequest translates an Anthropic /v1/messages JSON request into the provider-neutral IR.
func (*Codec) DecodeRequest(raw []byte, ctx *ir.ConversionContext) (*ir.Request, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("anthropic: empty request body")
	}

	var wire wireRequestDec
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal request: %w", err)
	}

	req := &ir.Request{
		Model: wire.Model,
		Generation: ir.GenerationConfig{
			MaxTokens:     wire.MaxTokens,
			Temperature:   wire.Temperature,
			TopP:          wire.TopP,
			TopK:          wire.TopK,
			StopSequences: wire.StopSequences,
			ServiceTier:   wire.ServiceTier,
		},
		Metadata:     wire.Metadata,
		CacheControl: decodeCacheControl(wire.CacheControl),
	}
	if wire.Stream != nil {
		req.Stream = *wire.Stream
	}
	if wire.Metadata != nil {
		if uid, ok := wire.Metadata["user_id"].(string); ok && uid != "" {
			req.Generation.User = uid
		}
	}

	// System prompt (string or array of text blocks).
	sysParts, err := decodeSystem(wire.System)
	if err != nil {
		return nil, fmt.Errorf("anthropic: decode system: %w", err)
	}
	req.System = sysParts

	// Messages.
	for i, m := range wire.Messages {
		decodedMsgs, err := decodeRequestMessage(m, ctx)
		if err != nil {
			return nil, fmt.Errorf("anthropic: decode message[%d]: %w", i, err)
		}
		req.Messages = append(req.Messages, decodedMsgs...)
	}

	// Tools.
	for i, rawTool := range wire.Tools {
		td, err := decodeToolDefinition(rawTool)
		if err != nil {
			return nil, fmt.Errorf("anthropic: decode tools[%d]: %w", i, err)
		}
		req.Tools = append(req.Tools, td)
	}

	// Tool choice & parallel tool use.
	if wire.ToolChoice != nil {
		tc, parallel := decodeToolChoice(wire.ToolChoice)
		req.ToolChoice = tc
		req.ParallelToolCalls = parallel
	}

	// Thinking & output_config.effort.
	req.Reasoning = decodeReasoningConfig(wire.Thinking, wire.OutputConfig)

	return req, nil
}

func decodeSystem(raw json.RawMessage) ([]ir.ContentPart, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		if s == "" {
			return nil, nil
		}
		return []ir.ContentPart{{Type: ir.PartText, Text: s}}, nil
	}
	if trimmed[0] == '[' {
		var blocks []wireContentBlock
		if err := json.Unmarshal(trimmed, &blocks); err != nil {
			return nil, err
		}
		parts := make([]ir.ContentPart, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "" || b.Type == "text" {
				parts = append(parts, ir.ContentPart{
					Type:         ir.PartText,
					Text:         b.Text,
					CacheControl: decodeCacheControl(b.CacheControl),
				})
			}
		}
		return parts, nil
	}
	return nil, fmt.Errorf("unsupported system JSON token %q", string(trimmed[:1]))
}

func decodeRequestMessage(m wireMessageDec, ctx *ir.ConversionContext) ([]ir.Message, error) {
	role := decodeRole(m.Role)
	trimmed := bytes.TrimSpace(m.Content)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []ir.Message{{Role: role}}, nil
	}

	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		return []ir.Message{{
			Role: role,
			Parts: []ir.ContentPart{{
				Type: ir.PartText,
				Text: text,
			}},
		}}, nil
	}

	if trimmed[0] != '[' {
		return nil, fmt.Errorf("unsupported message content token %q", string(trimmed[:1]))
	}

	var blocks []wireContentBlock
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return nil, err
	}

	parts := make([]ir.ContentPart, 0, len(blocks))
	hasToolResult := false
	hasNonToolResult := false
	for _, b := range blocks {
		part, ok := decodeRequestBlock(b, ctx)
		if !ok {
			continue
		}
		if part.Type == ir.PartToolResult {
			hasToolResult = true
		} else {
			hasNonToolResult = true
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return []ir.Message{{Role: role}}, nil
	}

	// For user messages carrying tool_result blocks:
	// - Pure tool_result messages map cleanly to RoleTool.
	// - Mixed user messages (tool_result + regular text/image) are split into contiguous
	//   RoleTool and RoleUser runs so every part and order is preserved across all target dialects,
	//   and merges cleanly back into a single Anthropic user message on re-encode.
	if role == ir.RoleUser && hasToolResult {
		if !hasNonToolResult {
			return []ir.Message{{Role: ir.RoleTool, Parts: parts}}, nil
		}
		var out []ir.Message
		for _, p := range parts {
			targetRole := ir.RoleUser
			if p.Type == ir.PartToolResult {
				targetRole = ir.RoleTool
			}
			if len(out) > 0 && out[len(out)-1].Role == targetRole {
				out[len(out)-1].Parts = append(out[len(out)-1].Parts, p)
			} else {
				out = append(out, ir.Message{Role: targetRole, Parts: []ir.ContentPart{p}})
			}
		}
		return out, nil
	}

	return []ir.Message{{Role: role, Parts: parts}}, nil
}

func decodeRole(role string) ir.Role {
	switch role {
	case "user":
		return ir.RoleUser
	case "assistant":
		return ir.RoleAssistant
	case "system":
		return ir.RoleSystem
	case "developer":
		return ir.RoleDeveloper
	case "tool":
		return ir.RoleTool
	default:
		return ir.Role(role)
	}
}

func decodeRequestBlock(b wireContentBlock, ctx *ir.ConversionContext) (ir.ContentPart, bool) {
	cc := decodeCacheControl(b.CacheControl)
	switch b.Type {
	case "text":
		return ir.ContentPart{
			Type:         ir.PartText,
			Text:         b.Text,
			CacheControl: cc,
		}, true

	case "image":
		if b.Source == nil {
			return ir.ContentPart{}, false
		}
		img := &ir.ImageSource{}
		switch b.Source.Type {
		case "base64":
			img.Data = b.Source.Data
			img.MediaType = b.Source.MediaType
		case "url":
			img.URL = b.Source.URL
		case "file":
			img.FileID = b.Source.FileID
		default:
			img.Data = b.Source.Data
			img.MediaType = b.Source.MediaType
			img.URL = b.Source.URL
			img.FileID = b.Source.FileID
		}
		return ir.ContentPart{
			Type:         ir.PartImage,
			Image:        img,
			CacheControl: cc,
		}, true

	case "document":
		if b.Source == nil {
			return ir.ContentPart{}, false
		}
		fs := &ir.FileSource{
			Filename:  b.Title,
			MediaType: b.Source.MediaType,
		}
		var meta map[string]any
		switch b.Source.Type {
		case "base64":
			fs.Data = b.Source.Data
		case "url":
			fs.URL = b.Source.URL
		case "text":
			fs.Text = b.Source.Data
			if fs.MediaType == "" {
				fs.MediaType = "text/plain"
			}
			meta = map[string]any{"anthropic_source_type": "text"}
		case "file":
			fs.FileID = b.Source.FileID
		default:
			fs.Data = b.Source.Data
			fs.URL = b.Source.URL
			fs.FileID = b.Source.FileID
		}
		return ir.ContentPart{
			Type:             ir.PartFile,
			File:             fs,
			CacheControl:     cc,
			ProviderMetadata: meta,
		}, true

	case "thinking":
		return ir.ContentPart{
			Type: ir.PartReasoning,
			Reasoning: &ir.Reasoning{
				Text:         b.Thinking,
				Signature:    b.Signature,
				OriginFormat: ir.FormatAnthropic,
			},
			CacheControl: cc,
		}, true

	case "redacted_thinking":
		return ir.ContentPart{
			Type: ir.PartReasoning,
			Reasoning: &ir.Reasoning{
				RedactedData: b.Data,
				OriginFormat: ir.FormatAnthropic,
			},
			CacheControl: cc,
		}, true

	case "tool_use":
		return ir.ContentPart{
			Type: ir.PartToolCall,
			ToolCall: &ir.ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: canonicalizeJSONObject(b.Input),
				Kind:      "function",
			},
			CacheControl: cc,
		}, true

	case "tool_result":
		tr := &ir.ToolResult{
			ToolCallID: b.ToolUseID,
			IsError:    b.IsError,
		}
		decodeToolResultContent(b.Content, tr, ctx)
		return ir.ContentPart{
			Type:         ir.PartToolResult,
			ToolResult:   tr,
			CacheControl: cc,
		}, true

	default:
		ctx.AddWarning("unsupported_content_block", "messages.content", fmt.Sprintf("skipped unsupported anthropic content block type %q", b.Type))
		return ir.ContentPart{}, false
	}
}

func decodeToolResultContent(raw json.RawMessage, tr *ir.ToolResult, ctx *ir.ConversionContext) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			tr.Content = s
		}
		return
	}
	if trimmed[0] == '[' {
		var blocks []wireContentBlock
		if err := json.Unmarshal(trimmed, &blocks); err != nil {
			return
		}
		parts := make([]ir.ContentPart, 0, len(blocks))
		for _, sub := range blocks {
			if p, ok := decodeRequestBlock(sub, ctx); ok {
				parts = append(parts, p)
			}
		}
		tr.Parts = parts
		tr.Content = ir.JoinTextParts(parts, "\n")
	}
}

func decodeToolDefinition(raw json.RawMessage) (ir.ToolDefinition, error) {
	var t wireToolDefDec
	if err := json.Unmarshal(raw, &t); err != nil {
		return ir.ToolDefinition{}, err
	}
	cc := decodeCacheControl(t.CacheControl)

	// Server-side or versioned Anthropic native tools (e.g. web_search_20250305,
	// computer_20250124, bash_20250124, text_editor_20250124) have a non-empty type
	// other than "custom". Preserve full raw JSON verbatim for Anthropic round-trips.
	if t.Type != "" && t.Type != "custom" {
		return ir.ToolDefinition{
			Type:         t.Type,
			Name:         t.Name,
			CacheControl: cc,
			NativeRaw:    bytes.Clone(bytes.TrimSpace(raw)),
			OriginFormat: ir.FormatAnthropic,
		}, nil
	}

	return ir.ToolDefinition{
		Type:         "function",
		Name:         t.Name,
		Description:  t.Description,
		Parameters:   bytes.Clone(bytes.TrimSpace(t.InputSchema)),
		Strict:       t.Strict,
		CacheControl: cc,
	}, nil
}

func decodeToolChoice(tc *wireToolChoice) (*ir.ToolChoice, *bool) {
	out := &ir.ToolChoice{}
	switch tc.Type {
	case "auto":
		out.Mode = "auto"
	case "any":
		out.Mode = "required"
	case "none":
		out.Mode = "none"
	case "tool":
		out.Mode = "tool"
		out.ToolName = tc.Name
	default:
		out.Mode = tc.Type
		out.ToolName = tc.Name
	}

	var parallel *bool
	if tc.DisableParallelToolUse != nil {
		dp := *tc.DisableParallelToolUse
		out.DisableParallel = &dp
		p := !dp
		parallel = &p
	}
	return out, parallel
}

func decodeReasoningConfig(thinking *wireThinking, outputCfg *wireOutputConfig) *ir.ReasoningConfig {
	if thinking == nil && (outputCfg == nil || outputCfg.Effort == "") {
		return nil
	}
	rc := &ir.ReasoningConfig{}
	if thinking != nil {
		rc.Mode = thinking.Type
		rc.BudgetTokens = thinking.BudgetTokens
		rc.Display = thinking.Display
		switch thinking.Type {
		case "enabled", "adaptive":
			v := true
			rc.Enabled = &v
		case "disabled":
			v := false
			rc.Enabled = &v
		}
	}
	if outputCfg != nil && outputCfg.Effort != "" {
		rc.Effort = outputCfg.Effort
	}
	return rc
}

// EncodeRequest translates a provider-neutral IR Request into an Anthropic /v1/messages JSON body,
// enforcing all Anthropic wire invariants (required max_tokens, strict alternating user/assistant
// roles, non-empty content, opaque signature safety, and valid tool input schemas).
func (*Codec) EncodeRequest(req *ir.Request, ctx *ir.ConversionContext) ([]byte, error) {
	if req == nil {
		return nil, errors.New("anthropic: nil request")
	}

	thinking, outputCfg := encodeReasoningConfig(req.Reasoning, req.Model)

	// Tools & tool choice must be resolved before thinking/tool_choice compatibility checks.
	wireTools := encodeTools(req.Tools, ctx)
	wireTC := encodeToolChoice(req.ToolChoice, req.ParallelToolCalls, len(wireTools) > 0)

	// Anthropic rejects manual thinking ("enabled") when tool_choice forces tool use ("any" or "tool").
	if thinking != nil && thinking.Type == "enabled" && wireTC != nil && (wireTC.Type == "any" || wireTC.Type == "tool") {
		ctx.AddWarning("dropped_thinking_forced_tool", "thinking", "disabled thinking.type=enabled because forced tool_choice (any/tool) is incompatible with manual thinking on Anthropic")
		thinking = nil
	}

	// Anthropic REQUIRES max_tokens > 0 and max_tokens > thinking.budget_tokens.
	maxTokens := 0
	if req.Generation.MaxTokens != nil && *req.Generation.MaxTokens > 0 {
		maxTokens = *req.Generation.MaxTokens
	} else {
		maxTokens = ctx.GetIntOption("default_max_tokens", defaultAnthropicMaxTokens)
		if maxTokens <= 0 {
			maxTokens = defaultAnthropicMaxTokens
		}
	}
	if thinking != nil && thinking.Type == "enabled" && thinking.BudgetTokens != nil && *thinking.BudgetTokens >= maxTokens {
		maxTokens = *thinking.BudgetTokens + thinkingMaxTokensHeadroom
		ctx.AddWarning("adjusted_max_tokens", "max_tokens",
			fmt.Sprintf("bumped max_tokens to %d so it exceeds thinking.budget_tokens (%d)", maxTokens, *thinking.BudgetTokens))
	}

	// Temperature / top_p / top_k constraints.
	var temp *float64
	if req.Generation.Temperature != nil {
		v := *req.Generation.Temperature
		if v > 1.0 {
			v = 1.0
			ctx.AddWarning("clamped_temperature", "temperature", "clamped temperature above 1.0 to Anthropic maximum 1.0")
		} else if v < 0.0 {
			v = 0.0
			ctx.AddWarning("clamped_temperature", "temperature", "clamped negative temperature to 0.0")
		}
		temp = &v
	}
	topP := req.Generation.TopP
	topK := req.Generation.TopK
	isAdaptiveOnlyModel := isAnthropicAdaptiveOnlyModel(req.Model)
	thinkingActive := thinking != nil && (thinking.Type == "enabled" || thinking.Type == "adaptive")
	if isAdaptiveOnlyModel {
		// Claude Opus 4.7+ rejects non-default temperature/top_p/top_k unconditionally.
		if temp != nil {
			temp = nil
			ctx.AddWarning("dropped_temperature", "temperature", "dropped temperature for Claude Opus 4.7+ compatibility")
		}
		if topP != nil {
			topP = nil
			ctx.AddWarning("dropped_top_p", "top_p", "dropped top_p for Claude Opus 4.7+ compatibility")
		}
		if topK != nil {
			topK = nil
			ctx.AddWarning("dropped_top_k", "top_k", "dropped top_k for Claude Opus 4.7+ compatibility")
		}
	} else if thinkingActive {
		if temp != nil && *temp != 1.0 {
			one := 1.0
			temp = &one
			ctx.AddWarning("adjusted_temperature", "temperature", "set temperature to 1.0 as required when thinking is active")
		}
		if topP != nil {
			topP = nil
			ctx.AddWarning("dropped_top_p", "top_p", "dropped top_p as required when thinking is active")
		}
		if topK != nil {
			topK = nil
			ctx.AddWarning("dropped_top_k", "top_k", "dropped top_k as required when thinking is active")
		}
	}

	// System blocks (req.System + any RoleSystem/RoleDeveloper messages).
	systemPayload := encodeSystem(req.System, req.Messages)

	// Messages with strict Anthropic invariants.
	wireMessages := encodeMessages(req.Messages, ctx)

	// Metadata (preserve metadata map and inject user_id from Generation.User without mutating caller state).
	var meta map[string]any
	if len(req.Metadata) > 0 || req.Generation.User != "" {
		meta = make(map[string]any, len(req.Metadata)+1)
		for k, v := range req.Metadata {
			meta[k] = v
		}
		if req.Generation.User != "" {
			if _, exists := meta["user_id"]; !exists {
				meta["user_id"] = req.Generation.User
			}
		}
	}

	isStream := req.Stream || (ctx != nil && ctx.GetBoolOption("stream", false))

	out := wireRequestEnc{
		Model:         req.Model,
		MaxTokens:     maxTokens,
		System:        systemPayload,
		Messages:      wireMessages,
		Tools:         wireTools,
		ToolChoice:    wireTC,
		Thinking:      thinking,
		OutputConfig:  outputCfg,
		Stream:        isStream,
		Temperature:   temp,
		TopP:          topP,
		TopK:          topK,
		StopSequences: req.Generation.StopSequences,
		Metadata:      meta,
		ServiceTier:   req.Generation.ServiceTier,
		CacheControl:  encodeCacheControl(req.CacheControl),
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}
	return encoded, nil
}

func encodeSystem(sysParts []ir.ContentPart, messages []ir.Message) any {
	var blocks []wireTextBlockEnc
	hasCacheControl := false

	appendPart := func(p ir.ContentPart, fallbackCC *ir.CacheControl) {
		if p.Type != ir.PartText || p.Text == "" {
			return
		}
		cc := p.CacheControl
		if cc == nil {
			cc = fallbackCC
		}
		wcc := encodeCacheControl(cc)
		if wcc != nil {
			hasCacheControl = true
		}
		blocks = append(blocks, wireTextBlockEnc{
			Type:         "text",
			Text:         p.Text,
			CacheControl: wcc,
		})
	}

	for _, p := range sysParts {
		appendPart(p, nil)
	}
	for _, msg := range messages {
		if msg.Role != ir.RoleSystem && msg.Role != ir.RoleDeveloper {
			continue
		}
		lastTextIdx := -1
		for i, p := range msg.Parts {
			if p.Type == ir.PartText && (p.Text != "" || p.CacheControl != nil) {
				lastTextIdx = i
			}
		}
		for i, p := range msg.Parts {
			var fallbackCC *ir.CacheControl
			if i == lastTextIdx {
				fallbackCC = msg.CacheControl
			}
			appendPart(p, fallbackCC)
		}
	}

	if len(blocks) == 0 {
		return nil
	}
	if len(blocks) == 1 && !hasCacheControl {
		return blocks[0].Text
	}
	return blocks
}

func encodeMessages(messages []ir.Message, ctx *ir.ConversionContext) []wireMessageEnc {
	var merged []wireMessageEnc
	toolFallbackIdx := 0

	for _, msg := range messages {
		if msg.Role == ir.RoleSystem || msg.Role == ir.RoleDeveloper {
			continue
		}

		wireRole := "user"
		if msg.Role == ir.RoleAssistant {
			wireRole = "assistant"
		}

		blocks := make([]any, 0, len(msg.Parts))
		for _, p := range msg.Parts {
			blk, ok := encodeRequestPart(p, &toolFallbackIdx, ctx)
			if ok {
				blocks = append(blocks, blk)
			}
		}

		// Propagate message-level cache_control onto the last cacheable (non-thinking) block if unset.
		if msg.CacheControl != nil && len(blocks) > 0 {
			wcc := encodeCacheControl(msg.CacheControl)
			for bi := len(blocks) - 1; bi >= 0; bi-- {
				if isCacheableBlock(blocks[bi]) {
					blocks[bi] = attachBlockCacheControl(blocks[bi], wcc)
					break
				}
			}
		}

		// Invariant 2: merge adjacent messages with the same wire role.
		if len(merged) > 0 && merged[len(merged)-1].Role == wireRole {
			merged[len(merged)-1].Content = append(merged[len(merged)-1].Content, blocks...)
		} else {
			merged = append(merged, wireMessageEnc{
				Role:    wireRole,
				Content: blocks,
			})
		}
	}

	// Enforce strict Anthropic block ordering inside merged messages:
	// - user messages: tool_result blocks must precede regular text/image/document blocks.
	// - assistant messages: thinking/redacted_thinking blocks must precede text/tool_use blocks.
	for i := range merged {
		if merged[i].Role == "user" {
			merged[i].Content = hoistBlocks(merged[i].Content, func(b any) bool {
				_, ok := b.(wireToolResultBlockEnc)
				return ok
			})
		} else if merged[i].Role == "assistant" {
			merged[i].Content = hoistBlocks(merged[i].Content, isThinkingBlock)
		}
	}

	// Invariant 3: replace empty message content with placeholder text block.
	for i := range merged {
		if len(merged[i].Content) == 0 {
			merged[i].Content = []any{wireTextBlockEnc{
				Type: "text",
				Text: placeholderNonEmptyTextContent,
			}}
		}
	}

	// Guarantee at least one message exists.
	if len(merged) == 0 {
		return []wireMessageEnc{{
			Role: "user",
			Content: []any{wireTextBlockEnc{
				Type: "text",
				Text: placeholderNonEmptyTextContent,
			}},
		}}
	}

	// Invariant 4: first message must be "user".
	if merged[0].Role == "assistant" {
		syntheticUser := wireMessageEnc{
			Role: "user",
			Content: []any{wireTextBlockEnc{
				Type: "text",
				Text: placeholderNonEmptyTextContent,
			}},
		}
		merged = append([]wireMessageEnc{syntheticUser}, merged...)
	}

	return merged
}

func encodeRequestPart(p ir.ContentPart, toolFallbackIdx *int, ctx *ir.ConversionContext) (any, bool) {
	cc := encodeCacheControl(p.CacheControl)

	switch p.Type {
	case ir.PartText:
		if p.Text == "" {
			return nil, false
		}
		return wireTextBlockEnc{
			Type:         "text",
			Text:         p.Text,
			CacheControl: cc,
		}, true

	case ir.PartRefusal:
		if p.Refusal == "" {
			return nil, false
		}
		return wireTextBlockEnc{
			Type:         "text",
			Text:         p.Refusal,
			CacheControl: cc,
		}, true

	case ir.PartReasoning:
		if p.Reasoning == nil {
			return nil, false
		}
		// Invariant 5 (opaque thinking/signature safety):
		// Anthropic rejects thinking blocks without valid Anthropic signatures, foreign encrypted blobs,
		// or cache_control on thinking/redacted_thinking blocks.
		if p.Reasoning.OriginFormat == ir.FormatAnthropic && p.Reasoning.Signature != "" {
			return wireThinkingBlockEnc{
				Type:      "thinking",
				Thinking:  p.Reasoning.Text,
				Signature: p.Reasoning.Signature,
			}, true
		}
		if p.Reasoning.OriginFormat == ir.FormatAnthropic && p.Reasoning.RedactedData != "" {
			return wireRedactedThinkingBlockEnc{
				Type: "redacted_thinking",
				Data: p.Reasoning.RedactedData,
			}, true
		}
		ctx.AddWarning("dropped_foreign_reasoning", "messages.content",
			fmt.Sprintf("dropped reasoning block from origin=%q without valid Anthropic signature or redacted payload", p.Reasoning.OriginFormat))
		return nil, false

	case ir.PartToolCall:
		if p.ToolCall == nil {
			return nil, false
		}
		id := p.ToolCall.ID
		if id == "" {
			id = fmt.Sprintf("toolu_%d", *toolFallbackIdx)
			*toolFallbackIdx++
		}
		return wireToolUseBlockEnc{
			Type:         "tool_use",
			ID:           id,
			Name:         p.ToolCall.Name,
			Input:        ensureJSONObject(p.ToolCall.Arguments),
			CacheControl: cc,
		}, true

	case ir.PartToolResult:
		if p.ToolResult == nil {
			return nil, false
		}
		var content any = p.ToolResult.Content
		if len(p.ToolResult.Parts) > 0 {
			subBlocks := make([]any, 0, len(p.ToolResult.Parts))
			for _, sp := range p.ToolResult.Parts {
				if sb, ok := encodeToolResultSubPart(sp, ctx); ok {
					subBlocks = append(subBlocks, sb)
				}
			}
			if len(subBlocks) > 0 {
				content = subBlocks
			}
		}
		return wireToolResultBlockEnc{
			Type:         "tool_result",
			ToolUseID:    p.ToolResult.ToolCallID,
			Content:      content,
			IsError:      p.ToolResult.IsError,
			CacheControl: cc,
		}, true

	case ir.PartImage:
		return encodeImageBlock(p.Image, cc)

	case ir.PartFile:
		return encodeDocumentBlock(p.File, p.ProviderMetadata, cc)

	case ir.PartAudio:
		ctx.AddWarning("unsupported_part_type", "messages.content", "Anthropic Messages API does not support inline audio content blocks")
		return nil, false

	case ir.PartCitation:
		return nil, false

	default:
		return nil, false
	}
}

func encodeToolResultSubPart(p ir.ContentPart, ctx *ir.ConversionContext) (any, bool) {
	cc := encodeCacheControl(p.CacheControl)
	switch p.Type {
	case ir.PartText:
		if p.Text == "" {
			return nil, false
		}
		return wireTextBlockEnc{
			Type:         "text",
			Text:         p.Text,
			CacheControl: cc,
		}, true
	case ir.PartImage:
		return encodeImageBlock(p.Image, cc)
	case ir.PartFile:
		return encodeDocumentBlock(p.File, p.ProviderMetadata, cc)
	default:
		ctx.AddWarning("unsupported_tool_result_part", "messages.content.tool_result",
			fmt.Sprintf("skipped unsupported tool_result sub-part type %q", p.Type))
		return nil, false
	}
}

func encodeImageBlock(img *ir.ImageSource, cc *wireCacheControl) (any, bool) {
	if img == nil {
		return nil, false
	}
	if img.Data != "" {
		return wireImageBlockEnc{
			Type: "image",
			Source: wireSource{
				Type:      "base64",
				MediaType: nonEmpty(img.MediaType, "image/png"),
				Data:      img.Data,
			},
			CacheControl: cc,
		}, true
	}
	if img.URL != "" {
		if mediaType, payload, ok := ir.ParseDataURL(img.URL); ok {
			return wireImageBlockEnc{
				Type: "image",
				Source: wireSource{
					Type:      "base64",
					MediaType: mediaType,
					Data:      payload,
				},
				CacheControl: cc,
			}, true
		}
		return wireImageBlockEnc{
			Type: "image",
			Source: wireSource{
				Type: "url",
				URL:  img.URL,
			},
			CacheControl: cc,
		}, true
	}
	if img.FileID != "" {
		return wireImageBlockEnc{
			Type: "image",
			Source: wireSource{
				Type:   "file",
				FileID: img.FileID,
			},
			CacheControl: cc,
		}, true
	}
	return nil, false
}

func encodeDocumentBlock(f *ir.FileSource, meta map[string]any, cc *wireCacheControl) (any, bool) {
	if f == nil {
		return nil, false
	}
	sourceType, _ := meta["anthropic_source_type"].(string)
	var src wireSource

	switch {
	case sourceType == "text" || (f.Text != "" && f.Data == "" && f.URL == "" && f.FileID == ""):
		textVal := f.Text
		if textVal == "" {
			textVal = f.Data
		}
		src = wireSource{
			Type:      "text",
			MediaType: nonEmpty(f.MediaType, "text/plain"),
			Data:      textVal,
		}
	case f.Data != "":
		if mediaType, payload, ok := ir.ParseDataURL(f.Data); ok {
			src = wireSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      payload,
			}
		} else {
			src = wireSource{
				Type:      "base64",
				MediaType: nonEmpty(f.MediaType, "application/pdf"),
				Data:      f.Data,
			}
		}
	case f.URL != "":
		if mediaType, payload, ok := ir.ParseDataURL(f.URL); ok {
			src = wireSource{
				Type:      "base64",
				MediaType: mediaType,
				Data:      payload,
			}
		} else {
			src = wireSource{
				Type: "url",
				URL:  f.URL,
			}
		}
	case f.FileID != "":
		src = wireSource{
			Type:   "file",
			FileID: f.FileID,
		}
	default:
		return nil, false
	}

	return wireDocumentBlockEnc{
		Type:         "document",
		Source:       src,
		Title:        f.Filename,
		CacheControl: cc,
	}, true
}

func isThinkingBlock(b any) bool {
	switch b.(type) {
	case wireThinkingBlockEnc, wireRedactedThinkingBlockEnc:
		return true
	default:
		return false
	}
}

func isCacheableBlock(b any) bool {
	return !isThinkingBlock(b)
}

func hoistBlocks(blocks []any, predicate func(any) bool) []any {
	if len(blocks) <= 1 {
		return blocks
	}
	var front []any
	var rest []any
	for _, b := range blocks {
		if predicate(b) {
			front = append(front, b)
		} else {
			rest = append(rest, b)
		}
	}
	if len(front) == 0 || len(rest) == 0 {
		return blocks
	}
	return append(front, rest...)
}

func attachBlockCacheControl(blk any, cc *wireCacheControl) any {
	if cc == nil {
		return blk
	}
	switch v := blk.(type) {
	case wireTextBlockEnc:
		if v.CacheControl == nil {
			v.CacheControl = cc
		}
		return v
	case wireImageBlockEnc:
		if v.CacheControl == nil {
			v.CacheControl = cc
		}
		return v
	case wireDocumentBlockEnc:
		if v.CacheControl == nil {
			v.CacheControl = cc
		}
		return v
	case wireToolUseBlockEnc:
		if v.CacheControl == nil {
			v.CacheControl = cc
		}
		return v
	case wireToolResultBlockEnc:
		if v.CacheControl == nil {
			v.CacheControl = cc
		}
		return v
	default:
		return blk
	}
}

func encodeTools(tools []ir.ToolDefinition, ctx *ir.ConversionContext) []any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		if len(t.NativeRaw) > 0 {
			if t.OriginFormat == ir.FormatAnthropic {
				out = append(out, json.RawMessage(bytes.Clone(t.NativeRaw)))
				continue
			}
			if t.Type != "function" && t.Type != "custom" && t.Type != "" {
				ctx.AddWarning("dropped_foreign_native_tool", "tools",
					fmt.Sprintf("dropped foreign native tool %q from origin=%q", t.Type, t.OriginFormat))
				continue
			}
		}
		if t.Name == "" {
			continue
		}
		out = append(out, wireToolDefEnc{
			Name:         t.Name,
			Description:  t.Description,
			InputSchema:  ensureInputSchemaObject(t.Parameters),
			Strict:       t.Strict,
			CacheControl: encodeCacheControl(t.CacheControl),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ensureInputSchemaObject guarantees input_schema is a JSON object with "type": "object"
// and recursively normalizes uppercase Gemini OpenAPI types (e.g. "STRING" -> "string").
func ensureInputSchemaObject(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	var schema map[string]any
	if err := json.Unmarshal(trimmed, &schema); err != nil || schema == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	ir.NormalizeJSONSchemaTypesInPlace(schema)
	schema["type"] = "object"
	b, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return json.RawMessage(b)
}

func encodeToolChoice(tc *ir.ToolChoice, parallelToolCalls *bool, hasTools bool) *wireToolChoice {
	if !hasTools {
		return nil
	}
	var disableParallel *bool
	if tc != nil && tc.DisableParallel != nil {
		v := *tc.DisableParallel
		disableParallel = &v
	} else if parallelToolCalls != nil {
		v := !(*parallelToolCalls)
		disableParallel = &v
	}

	if tc == nil {
		if disableParallel != nil && *disableParallel {
			v := true
			return &wireToolChoice{
				Type:                   "auto",
				DisableParallelToolUse: &v,
			}
		}
		return nil
	}

	out := &wireToolChoice{}
	switch strings.ToLower(tc.Mode) {
	case "", "auto":
		out.Type = "auto"
	case "required", "any":
		out.Type = "any"
	case "none":
		out.Type = "none"
	case "tool", "function":
		out.Type = "tool"
		out.Name = tc.ToolName
	default:
		out.Type = tc.Mode
		out.Name = tc.ToolName
	}

	// Anthropic rejects disable_parallel_tool_use when tool_choice.type == "none".
	if out.Type != "none" && disableParallel != nil && *disableParallel {
		v := true
		out.DisableParallelToolUse = &v
	}
	return out
}

func isAnthropicAdaptiveOnlyModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(m, "opus-4-7") || strings.Contains(m, "opus-4.7") ||
		strings.Contains(m, "opus-4-8") || strings.Contains(m, "opus-4.8")
}

func supportsAnthropicOutputConfigEffort(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return true
	}
	return strings.Contains(m, "4-6") || strings.Contains(m, "4.6") ||
		strings.Contains(m, "4-7") || strings.Contains(m, "4.7") ||
		strings.Contains(m, "4-8") || strings.Contains(m, "4.8") ||
		strings.Contains(m, "opus-4-5") || strings.Contains(m, "opus-4.5")
}

func encodeReasoningConfig(rc *ir.ReasoningConfig, model string) (*wireThinking, *wireOutputConfig) {
	if rc == nil {
		return nil, nil
	}

	var outputCfg *wireOutputConfig
	effort := normalizeAnthropicEffort(rc.Effort)
	if effort != "" {
		outputCfg = &wireOutputConfig{Effort: effort}
	}

	adaptiveOnly := isAnthropicAdaptiveOnlyModel(model)
	mode := strings.ToLower(strings.TrimSpace(rc.Mode))
	switch {
	case mode == "disabled" || (rc.Enabled != nil && !*rc.Enabled) ||
		(mode == "" && rc.Enabled == nil && strings.EqualFold(rc.Effort, "none")):
		return &wireThinking{Type: "disabled"}, nil

	case mode == "adaptive":
		return &wireThinking{
			Type:    "adaptive",
			Display: rc.Display,
		}, outputCfg

	case mode == "enabled" || (rc.Enabled != nil && *rc.Enabled) || (rc.BudgetTokens != nil && *rc.BudgetTokens > 0):
		if adaptiveOnly {
			return &wireThinking{
				Type:    "adaptive",
				Display: rc.Display,
			}, outputCfg
		}
		budget := 0
		if rc.BudgetTokens != nil && *rc.BudgetTokens > 0 {
			budget = *rc.BudgetTokens
		} else {
			budget = effortToBudgetTokens(rc.Effort)
		}
		if budget < minAnthropicThinkingBudget {
			budget = minAnthropicThinkingBudget
		}
		var oc *wireOutputConfig
		if supportsAnthropicOutputConfigEffort(model) {
			oc = outputCfg
		}
		return &wireThinking{
			Type:         "enabled",
			BudgetTokens: &budget,
			Display:      rc.Display,
		}, oc

	case effort != "":
		// Bare reasoning_effort (e.g. from OpenAI Chat) without explicit Mode/Enabled/BudgetTokens.
		if adaptiveOnly || supportsAnthropicOutputConfigEffort(model) {
			return &wireThinking{
				Type:    "adaptive",
				Display: rc.Display,
			}, outputCfg
		}
		budget := effortToBudgetTokens(rc.Effort)
		if budget < minAnthropicThinkingBudget {
			budget = minAnthropicThinkingBudget
		}
		return &wireThinking{
			Type:         "enabled",
			BudgetTokens: &budget,
			Display:      rc.Display,
		}, nil

	default:
		return nil, outputCfg
	}
}

func normalizeAnthropicEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "none":
		return ""
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "max", "xhigh":
		return "max"
	default:
		return strings.TrimSpace(effort)
	}
}

func effortToBudgetTokens(effort string) int {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 16384
	case "max", "xhigh":
		return 32768
	default:
		return 1024
	}
}
