package openaichat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relay/ir"
)

type chatResponseWire struct {
	ID                string                   `json:"id"`
	Object            string                   `json:"object,omitempty"`
	Created           flexInt64                `json:"created,omitempty"`
	Model             string                   `json:"model"`
	SystemFingerprint string                   `json:"system_fingerprint,omitempty"`
	ServiceTier       string                   `json:"service_tier,omitempty"`
	Choices           []chatResponseChoiceWire `json:"choices"`
	Usage             *chatUsageWire           `json:"usage,omitempty"`
	Error             *chatErrorPayload        `json:"error,omitempty"`
}

type chatResponseChoiceWire struct {
	Index        int             `json:"index"`
	Message      chatMessage     `json:"message"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type chatUsageWire struct {
	PromptTokens                int                        `json:"prompt_tokens"`
	CompletionTokens            int                        `json:"completion_tokens"`
	TotalTokens                 int                        `json:"total_tokens"`
	PromptCacheHitTokens        int                        `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens       int                        `json:"prompt_cache_miss_tokens,omitempty"`
	ClaudeCacheCreation5mTokens int                        `json:"claude_cache_creation_5_m_tokens,omitempty"`
	ClaudeCacheCreation1hTokens int                        `json:"claude_cache_creation_1_h_tokens,omitempty"`
	PromptTokensDetails         *chatPromptTokenDetails    `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails     *chatCompletionTokenDetail `json:"completion_tokens_details,omitempty"`
}

type chatPromptTokenDetails struct {
	CachedTokens             int  `json:"cached_tokens,omitempty"`
	CacheWriteTokens         *int `json:"cache_write_tokens,omitempty"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CachedCreationTokens     int  `json:"cached_creation_tokens,omitempty"`
	TextTokens               int  `json:"text_tokens,omitempty"`
	AudioTokens              int  `json:"audio_tokens,omitempty"`
	ImageTokens              int  `json:"image_tokens,omitempty"`
}

type chatCompletionTokenDetail struct {
	ReasoningTokens          int `json:"reasoning_tokens,omitempty"`
	TextTokens               int `json:"text_tokens,omitempty"`
	AudioTokens              int `json:"audio_tokens,omitempty"`
	ImageTokens              int `json:"image_tokens,omitempty"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens,omitempty"`
}

type outChatResponse struct {
	ID                string          `json:"id"`
	Object            string          `json:"object"`
	Created           int64           `json:"created"`
	Model             string          `json:"model"`
	SystemFingerprint string          `json:"system_fingerprint,omitempty"`
	ServiceTier       string          `json:"service_tier,omitempty"`
	Choices           []outChatChoice `json:"choices"`
	Usage             *chatUsageWire  `json:"usage,omitempty"`
}

type outChatChoice struct {
	Index        int            `json:"index"`
	Message      outChatMessage `json:"message"`
	FinishReason string         `json:"finish_reason"`
	Logprobs     any            `json:"logprobs,omitempty"`
}

type outChatMessage struct {
	Role             string           `json:"role"`
	Content          *string          `json:"content"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	Refusal          *string          `json:"refusal,omitempty"`
	ToolCalls        []chatToolCall   `json:"tool_calls,omitempty"`
	Annotations      []chatAnnotation `json:"annotations,omitempty"`
}

// DecodeResponse parses an OpenAI Chat Completions non-streaming JSON response into IR.
func (c *Codec) DecodeResponse(raw []byte, ctx *ir.ConversionContext) (*ir.Response, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("openaichat: empty response body")
	}

	var wire chatResponseWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("openaichat: decode response: %w", err)
	}
	if wire.Error != nil && len(wire.Choices) == 0 {
		return nil, fmt.Errorf("openaichat: upstream error (%s): %s", wire.Error.Type, wire.Error.Message)
	}

	resp := &ir.Response{
		ID:                wire.ID,
		Model:             wire.Model,
		Created:           int64(wire.Created),
		SystemFingerprint: wire.SystemFingerprint,
		ServiceTier:       wire.ServiceTier,
		Choices:           make([]ir.Choice, 0, len(wire.Choices)),
	}

	for _, ch := range wire.Choices {
		msgWire := ch.Message
		if msgWire.Role == "" {
			msgWire.Role = "assistant"
		}
		irMsg, err := decodeChatMessage(msgWire, ctx)
		if err != nil {
			return nil, err
		}
		var rawFinish string
		if ch.FinishReason != nil {
			rawFinish = *ch.FinishReason
		}
		irChoice := ir.Choice{
			Index:           ch.Index,
			Message:         irMsg,
			FinishReason:    normalizeFinishReason(rawFinish),
			RawFinishReason: rawFinish,
		}
		if !isNullOrEmptyJSON(ch.Logprobs) {
			irChoice.ProviderMetadata = map[string]any{
				"logprobs": cloneBytes(ch.Logprobs),
			}
		}
		resp.Choices = append(resp.Choices, irChoice)
	}

	if wire.Usage != nil {
		resp.Usage = decodeUsage(wire.Usage)
	}

	return resp, nil
}

// EncodeResponse serializes an IR Response into an OpenAI Chat Completions JSON response.
func (c *Codec) EncodeResponse(resp *ir.Response, ctx *ir.ConversionContext) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("openaichat: nil response")
	}

	id := resp.ID
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano())
	}
	created := resp.Created
	if created == 0 {
		created = time.Now().Unix()
	}

	out := outChatResponse{
		ID:                id,
		Object:            "chat.completion",
		Created:           created,
		Model:             resp.Model,
		SystemFingerprint: resp.SystemFingerprint,
		ServiceTier:       resp.ServiceTier,
		Choices:           make([]outChatChoice, 0, len(resp.Choices)),
	}

	for _, ch := range resp.Choices {
		role := string(ch.Message.Role)
		if role == "" {
			role = "assistant"
		}

		var textBuf strings.Builder
		var hasText bool
		var reasoningBuf strings.Builder
		var refusalBuf strings.Builder
		var toolCalls []chatToolCall
		var annotations []chatAnnotation

		for _, p := range ch.Message.Parts {
			switch p.Type {
			case ir.PartText:
				textBuf.WriteString(p.Text)
				hasText = true
			case ir.PartImage:
				if p.Image != nil {
					if md := formatImageMarkdown(p.Image); md != "" {
						if textBuf.Len() > 0 {
							textBuf.WriteByte('\n')
						}
						textBuf.WriteString(md)
						hasText = true
					}
				}
			case ir.PartAudio, ir.PartFile:
				ctx.AddWarning("unsupported_response_part", "choices.message.content", fmt.Sprintf("OpenAI Chat Completions text response cannot carry %q part natively", p.Type))
			case ir.PartReasoning:
				if p.Reasoning == nil {
					continue
				}
				rText := p.Reasoning.Text
				if rText == "" {
					rText = p.Reasoning.Summary
				}
				if rText != "" {
					if reasoningBuf.Len() > 0 {
						reasoningBuf.WriteByte('\n')
					}
					reasoningBuf.WriteString(rText)
				}
				if p.Reasoning.Signature != "" && !ctx.ShouldPreserveMetadata(p.Reasoning.OriginFormat) {
					ctx.AddWarning("dropped_reasoning_signature", "reasoning.signature", "OpenAI Chat Completions response does not carry opaque cross-provider reasoning signatures")
				}
				if p.Reasoning.RedactedData != "" && !ctx.ShouldPreserveMetadata(p.Reasoning.OriginFormat) {
					ctx.AddWarning("dropped_reasoning_redacted", "reasoning.redacted_data", "OpenAI Chat Completions response does not carry opaque cross-provider redacted reasoning blobs")
				}
			case ir.PartRefusal:
				if p.Refusal != "" {
					refusalBuf.WriteString(p.Refusal)
				}
			case ir.PartToolCall:
				if p.ToolCall == nil {
					continue
				}
				if p.ToolCall.ThoughtSignature != "" && !ctx.ShouldPreserveMetadata(ir.FormatGoogleGenAI) {
					ctx.AddWarning("dropped_thought_signature", "tool_call.thought_signature", "OpenAI Chat Completions response does not carry Google thought signatures")
				}
				toolCalls = append(toolCalls, encodeToolCallWire(p.ToolCall))
			case ir.PartCitation:
				if p.Citation != nil {
					annotations = append(annotations, chatAnnotation{
						Type: "url_citation",
						URLCitation: &chatURLCitation{
							URL:        p.Citation.URL,
							Title:      p.Citation.Title,
							CitedText:  p.Citation.CitedText,
							StartIndex: p.Citation.StartIndex,
							EndIndex:   p.Citation.EndIndex,
						},
					})
				}
			}
		}

		msgOut := outChatMessage{
			Role:        role,
			ToolCalls:   toolCalls,
			Annotations: annotations,
		}
		if hasText && textBuf.Len() > 0 {
			s := textBuf.String()
			msgOut.Content = &s
		} else if len(toolCalls) == 0 && refusalBuf.Len() == 0 {
			s := ""
			msgOut.Content = &s
		}
		if reasoningBuf.Len() > 0 {
			s := reasoningBuf.String()
			msgOut.ReasoningContent = &s
		}
		if refusalBuf.Len() > 0 {
			s := refusalBuf.String()
			msgOut.Refusal = &s
		}

		finishReason := encodeFinishReason(ch.FinishReason)
		if ch.FinishReason == "" {
			if len(toolCalls) > 0 {
				finishReason = "tool_calls"
			} else {
				finishReason = "stop"
			}
		}

		choiceOut := outChatChoice{
			Index:        ch.Index,
			Message:      msgOut,
			FinishReason: finishReason,
		}
		if ch.ProviderMetadata != nil {
			if lp, ok := ch.ProviderMetadata["logprobs"]; ok && lp != nil {
				choiceOut.Logprobs = lp
			}
		}
		out.Choices = append(out.Choices, choiceOut)
	}

	if resp.Usage != nil {
		out.Usage = encodeUsage(resp.Usage)
	}

	return json.Marshal(out)
}

func encodeToolCallWire(tc *ir.ToolCall) chatToolCall {
	if tc.Kind == "custom" {
		return chatToolCall{
			ID:   tc.ID,
			Type: "custom",
			Custom: &chatCustomCall{
				Name:  tc.Name,
				Input: tc.Arguments,
			},
		}
	}
	args := tc.Arguments
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	return chatToolCall{
		ID:   tc.ID,
		Type: "function",
		Function: &chatFunctionCall{
			Name:      tc.Name,
			Arguments: args,
		},
	}
}

func formatImageMarkdown(img *ir.ImageSource) string {
	if img == nil {
		return ""
	}
	if img.URL != "" {
		return fmt.Sprintf("![image](%s)", img.URL)
	}
	if img.Data != "" {
		mt := img.MediaType
		if mt == "" {
			mt = "image/png"
		}
		return fmt.Sprintf("![image](data:%s;base64,%s)", mt, img.Data)
	}
	return ""
}

func decodeUsage(u *chatUsageWire) *ir.Usage {
	if u == nil {
		return nil
	}
	out := &ir.Usage{
		InputTokens:           u.PromptTokens,
		OutputTokens:          u.CompletionTokens,
		TotalTokens:           u.TotalTokens,
		InputExclusive:        false,
		CacheCreation5mTokens: u.ClaudeCacheCreation5mTokens,
		CacheCreation1hTokens: u.ClaudeCacheCreation1hTokens,
	}
	if u.PromptTokensDetails != nil {
		d := u.PromptTokensDetails
		out.CachedReadTokens = d.CachedTokens
		out.TextInputTokens = d.TextTokens
		out.AudioInputTokens = d.AudioTokens
		out.ImageInputTokens = d.ImageTokens
		switch {
		case d.CacheWriteTokens != nil:
			out.CacheCreationTokens = *d.CacheWriteTokens
		case d.CacheCreationInputTokens != nil:
			out.CacheCreationTokens = *d.CacheCreationInputTokens
		default:
			out.CacheCreationTokens = d.CachedCreationTokens
		}
	}
	if out.CacheCreationTokens == 0 && (out.CacheCreation5mTokens > 0 || out.CacheCreation1hTokens > 0) {
		out.CacheCreationTokens = out.CacheCreation5mTokens + out.CacheCreation1hTokens
	}
	// DeepSeek compatibility fallback when prompt_tokens_details.cached_tokens is absent.
	if out.CachedReadTokens == 0 && u.PromptCacheHitTokens > 0 {
		out.CachedReadTokens = u.PromptCacheHitTokens
	}
	if u.CompletionTokensDetails != nil {
		d := u.CompletionTokensDetails
		out.ReasoningTokens = d.ReasoningTokens
		out.TextOutputTokens = d.TextTokens
		out.AudioOutputTokens = d.AudioTokens
		out.ImageOutputTokens = d.ImageTokens
	}
	if out.TotalTokens < out.InputTokens+out.OutputTokens {
		out.TotalTokens = out.InputTokens + out.OutputTokens
	}
	return out
}

func encodeUsage(u *ir.Usage) *chatUsageWire {
	if u == nil {
		return nil
	}
	promptTokens := u.InclusiveInputTokens()
	completionTokens := u.OutputTokens
	totalTokens := u.TotalTokens
	if totalTokens < promptTokens+completionTokens {
		totalTokens = promptTokens + completionTokens
	}

	out := &chatUsageWire{
		PromptTokens:                promptTokens,
		CompletionTokens:            completionTokens,
		TotalTokens:                 totalTokens,
		ClaudeCacheCreation5mTokens: u.CacheCreation5mTokens,
		ClaudeCacheCreation1hTokens: u.CacheCreation1hTokens,
	}

	if u.CachedReadTokens > 0 || u.CacheCreationTokens > 0 || u.TextInputTokens > 0 || u.AudioInputTokens > 0 || u.ImageInputTokens > 0 {
		out.PromptTokensDetails = &chatPromptTokenDetails{
			CachedTokens:         u.CachedReadTokens,
			CachedCreationTokens: u.CacheCreationTokens,
			TextTokens:           u.TextInputTokens,
			AudioTokens:          u.AudioInputTokens,
			ImageTokens:          u.ImageInputTokens,
		}
	}
	if u.ReasoningTokens > 0 || u.TextOutputTokens > 0 || u.AudioOutputTokens > 0 || u.ImageOutputTokens > 0 {
		out.CompletionTokensDetails = &chatCompletionTokenDetail{
			ReasoningTokens: u.ReasoningTokens,
			TextTokens:      u.TextOutputTokens,
			AudioTokens:     u.AudioOutputTokens,
			ImageTokens:     u.ImageOutputTokens,
		}
	}
	return out
}
