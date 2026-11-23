package anthropic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/relay/ir"
)

type wireResponseDec struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Role         string            `json:"role"`
	Model        string            `json:"model"`
	Content      []json.RawMessage `json:"content"`
	StopReason   *string           `json:"stop_reason"`
	StopSequence *string           `json:"stop_sequence"`
	Usage        *wireUsage        `json:"usage,omitempty"`
	ServiceTier  string            `json:"service_tier,omitempty"`
}

type wireResponseEnc struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Model        string    `json:"model"`
	Content      []any     `json:"content"`
	StopReason   string    `json:"stop_reason"`
	StopSequence *string   `json:"stop_sequence"`
	Usage        wireUsage `json:"usage"`
}

// DecodeResponse translates an Anthropic non-streaming Message JSON response into IR.
func (*Codec) DecodeResponse(raw []byte, ctx *ir.ConversionContext) (*ir.Response, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("anthropic: empty response body")
	}

	var wire wireResponseDec
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("anthropic: unmarshal response: %w", err)
	}

	parts := make([]ir.ContentPart, 0, len(wire.Content))
	var passthrough []ir.PassthroughItem

	for idx, rawBlk := range wire.Content {
		var blk wireContentBlock
		if err := json.Unmarshal(rawBlk, &blk); err != nil {
			return nil, fmt.Errorf("anthropic: unmarshal response content[%d]: %w", idx, err)
		}

		switch blk.Type {
		case "text":
			parts = append(parts, ir.ContentPart{
				Type: ir.PartText,
				Text: blk.Text,
			})
			for _, c := range blk.Citations {
				parts = append(parts, ir.ContentPart{
					Type:     ir.PartCitation,
					Citation: decodeCitation(c),
				})
			}

		case "thinking":
			parts = append(parts, ir.ContentPart{
				Type: ir.PartReasoning,
				Reasoning: &ir.Reasoning{
					Text:         blk.Thinking,
					Signature:    blk.Signature,
					OriginFormat: ir.FormatAnthropic,
				},
			})

		case "redacted_thinking":
			parts = append(parts, ir.ContentPart{
				Type: ir.PartReasoning,
				Reasoning: &ir.Reasoning{
					RedactedData: blk.Data,
					OriginFormat: ir.FormatAnthropic,
				},
			})

		case "tool_use":
			parts = append(parts, ir.ContentPart{
				Type: ir.PartToolCall,
				ToolCall: &ir.ToolCall{
					ID:        blk.ID,
					Name:      blk.Name,
					Arguments: canonicalizeJSONObject(blk.Input),
					Kind:      "function",
				},
			})

		default:
			passthrough = append(passthrough, ir.PassthroughItem{
				OriginFormat: ir.FormatAnthropic,
				Position:     idx,
				Kind:         blk.Type,
				Payload:      bytes.Clone(bytes.TrimSpace(rawBlk)),
			})
		}
	}

	rawStopReason := ""
	if wire.StopReason != nil {
		rawStopReason = *wire.StopReason
	}
	stopSeq := ""
	if wire.StopSequence != nil {
		stopSeq = *wire.StopSequence
	}

	serviceTier := wire.ServiceTier
	if serviceTier == "" && wire.Usage != nil {
		serviceTier = wire.Usage.ServiceTier
	}

	resp := &ir.Response{
		ID:    wire.ID,
		Model: wire.Model,
		Choices: []ir.Choice{{
			Index: 0,
			Message: ir.Message{
				Role:  ir.RoleAssistant,
				Parts: parts,
			},
			FinishReason:    decodeFinishReason(rawStopReason),
			RawFinishReason: rawStopReason,
			StopSequence:    stopSeq,
		}},
		Usage:            decodeUsage(wire.Usage, true),
		ServiceTier:      serviceTier,
		PassthroughItems: passthrough,
	}
	return resp, nil
}

func decodeCitation(c wireCitation) *ir.Citation {
	title := c.Title
	if title == "" {
		title = c.DocumentTitle
	}
	startIdx := c.StartCharIdx
	endIdx := c.EndCharIdx
	if startIdx == 0 && endIdx == 0 && (c.StartBlockIdx != 0 || c.EndBlockIdx != 0) {
		startIdx = c.StartBlockIdx
		endIdx = c.EndBlockIdx
	}
	return &ir.Citation{
		URL:        c.URL,
		Title:      title,
		CitedText:  c.CitedText,
		StartIndex: startIdx,
		EndIndex:   endIdx,
	}
}

func encodeCitation(c *ir.Citation) wireCitation {
	wc := wireCitation{
		URL:          c.URL,
		Title:        c.Title,
		CitedText:    c.CitedText,
		StartCharIdx: c.StartIndex,
		EndCharIdx:   c.EndIndex,
	}
	if c.URL != "" {
		wc.Type = "web_search_result_location"
	} else {
		wc.Type = "char_location"
	}
	return wc
}

// EncodeResponse translates an IR Response into a canonical Anthropic Message JSON payload.
func (*Codec) EncodeResponse(resp *ir.Response, ctx *ir.ConversionContext) ([]byte, error) {
	if resp == nil {
		return nil, errors.New("anthropic: nil response")
	}

	content := make([]any, 0)
	var finishReason ir.FinishReason
	rawFinishReason := ""
	var stopSequence *string
	hasToolCall := false
	toolFallbackIdx := 0

	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		finishReason = ch.FinishReason
		rawFinishReason = ch.RawFinishReason
		if ch.StopSequence != "" {
			s := ch.StopSequence
			stopSequence = &s
		}

		for _, p := range ch.Message.Parts {
			switch p.Type {
			case ir.PartReasoning:
				if p.Reasoning == nil {
					continue
				}
				r := p.Reasoning
				if r.OriginFormat == ir.FormatAnthropic && r.RedactedData != "" && r.Text == "" && r.Signature == "" {
					content = append(content, wireRedactedThinkingBlockEnc{
						Type: "redacted_thinking",
						Data: r.RedactedData,
					})
					continue
				}
				thinkingText := r.Text
				if thinkingText == "" {
					thinkingText = r.Summary
				}
				sig := ""
				if r.OriginFormat == ir.FormatAnthropic || r.OriginFormat == ir.FormatUnknown {
					sig = r.Signature
				}
				if thinkingText != "" || sig != "" {
					content = append(content, wireThinkingBlockEnc{
						Type:      "thinking",
						Thinking:  thinkingText,
						Signature: sig,
					})
				}

			case ir.PartText:
				if p.Text == "" {
					continue
				}
				content = append(content, wireTextBlockEnc{
					Type: "text",
					Text: p.Text,
				})

			case ir.PartImage:
				if p.Image != nil {
					if md := formatAnthropicImageMarkdown(p.Image); md != "" {
						content = append(content, wireTextBlockEnc{
							Type: "text",
							Text: md,
						})
					}
				}

			case ir.PartCitation:
				if p.Citation == nil || len(content) == 0 {
					continue
				}
				if tb, ok := content[len(content)-1].(wireTextBlockEnc); ok {
					tb.Citations = append(tb.Citations, encodeCitation(p.Citation))
					content[len(content)-1] = tb
				}

			case ir.PartRefusal:
				if p.Refusal != "" {
					content = append(content, wireTextBlockEnc{
						Type: "text",
						Text: p.Refusal,
					})
				}

			case ir.PartToolCall:
				if p.ToolCall == nil {
					continue
				}
				id := p.ToolCall.ID
				if id == "" {
					id = fmt.Sprintf("toolu_%d", toolFallbackIdx)
					toolFallbackIdx++
				}
				hasToolCall = true
				content = append(content, wireToolUseBlockEnc{
					Type:  "tool_use",
					ID:    id,
					Name:  p.ToolCall.Name,
					Input: ensureJSONObject(p.ToolCall.Arguments),
				})
			}
		}
	}

	// Preserve same-dialect passthrough blocks in their original Position order.
	if ctx != nil && len(resp.PassthroughItems) > 0 {
		var nativePT []ir.PassthroughItem
		for _, item := range resp.PassthroughItems {
			if item.OriginFormat == ir.FormatAnthropic && ctx.ShouldPreserveMetadata(item.OriginFormat) && len(item.Payload) > 0 {
				nativePT = append(nativePT, item)
			}
		}
		sort.SliceStable(nativePT, func(i, j int) bool {
			return nativePT[i].Position < nativePT[j].Position
		})
		for _, item := range nativePT {
			rawBlk := json.RawMessage(bytes.Clone(item.Payload))
			pos := item.Position
			if pos < 0 || pos >= len(content) {
				content = append(content, rawBlk)
				continue
			}
			content = append(content, nil)
			copy(content[pos+1:], content[pos:])
			content[pos] = rawBlk
		}
	}

	stopReason := ""
	if finishReason != "" {
		stopReason = encodeFinishReason(finishReason, rawFinishReason)
	} else if hasToolCall {
		stopReason = "tool_use"
	} else {
		stopReason = "end_turn"
	}

	out := wireResponseEnc{
		ID:           ensureMsgIDPrefix(resp.ID),
		Type:         "message",
		Role:         "assistant",
		Model:        resp.Model,
		Content:      content,
		StopReason:   stopReason,
		StopSequence: stopSequence,
		Usage:        encodeUsage(resp.Usage),
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal response: %w", err)
	}
	return encoded, nil
}

func formatAnthropicImageMarkdown(img *ir.ImageSource) string {
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
