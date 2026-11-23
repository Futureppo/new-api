package google

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

// DecodeResponse parses a non-streaming Google GenAI GenerateContentResponse JSON payload into IR.
func (c *Codec) DecodeResponse(raw []byte, ctx *ir.ConversionContext) (*ir.Response, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("google: empty response body")
	}
	var gResp googleResponse
	if err := json.Unmarshal(raw, &gResp); err != nil {
		return nil, fmt.Errorf("google: decode response: %w", err)
	}
	if gResp.Error != nil {
		return nil, fmt.Errorf("google: upstream error (%d %s): %s", gResp.Error.Code, gResp.Error.Status, gResp.Error.Message)
	}

	resp := &ir.Response{
		ID:      gResp.ResponseID,
		Model:   gResp.ModelVersion,
		Choices: make([]ir.Choice, 0, len(gResp.Candidates)),
		Usage:   decodeUsageMetadata(gResp.UsageMetadata),
	}

	for ci, cand := range gResp.Candidates {
		choiceIdx := cand.Index
		if ci > 0 && choiceIdx == 0 {
			choiceIdx = ci
		}

		var parts []ir.ContentPart
		hasToolCalls := false
		if cand.Content != nil {
			for pi, p := range cand.Content.Parts {
				if p.FunctionCall != nil {
					hasToolCalls = true
					id := p.FunctionCall.ID
					if id == "" {
						id = ir.SynthesizeToolCallID("call")
					}
					args := normalizeJSONArgs(p.FunctionCall.Args)
					tc := &ir.ToolCall{
						ID:               id,
						Name:             p.FunctionCall.Name,
						Arguments:        args,
						Kind:             "function",
						ThoughtSignature: p.ThoughtSignature,
					}
					parts = append(parts, ir.ContentPart{
						Type:     ir.PartToolCall,
						ToolCall: tc,
					})
					if ctx != nil && ctx.BridgeState != nil {
						ctx.BridgeState.RecordToolCall(ir.ToolCallBridgeEntry{
							ToolCallID:       tc.ID,
							ToolName:         tc.Name,
							ThoughtSignature: tc.ThoughtSignature,
							OriginFormat:     ir.FormatGoogleGenAI,
						})
					}
					continue
				}
				if part, ok := decodeBasicPart(p, choiceIdx, pi); ok {
					parts = append(parts, part)
				}
			}
		}

		resp.Choices = append(resp.Choices, ir.Choice{
			Index: choiceIdx,
			Message: ir.Message{
				Role:  ir.RoleAssistant,
				Parts: parts,
			},
			FinishReason:    mapGoogleFinishReason(cand.FinishReason, hasToolCalls),
			RawFinishReason: cand.FinishReason,
		})
	}

	if len(resp.Choices) == 0 && gResp.PromptFeedback != nil && gResp.PromptFeedback.BlockReason != "" {
		resp.Choices = append(resp.Choices, ir.Choice{
			Index: 0,
			Message: ir.Message{
				Role: ir.RoleAssistant,
			},
			FinishReason:    ir.FinishContentFilter,
			RawFinishReason: gResp.PromptFeedback.BlockReason,
		})
	}

	return resp, nil
}

// EncodeResponse serializes an IR Response into a canonical Google GenAI GenerateContentResponse JSON payload.
func (c *Codec) EncodeResponse(resp *ir.Response, ctx *ir.ConversionContext) ([]byte, error) {
	if resp == nil {
		return nil, errors.New("google: nil IR response")
	}

	gResp := googleResponse{
		ResponseID:    resp.ID,
		ModelVersion:  resp.Model,
		Candidates:    make([]googleCandidate, 0, len(resp.Choices)),
		UsageMetadata: encodeUsageMetadata(resp.Usage),
	}

	for _, ch := range resp.Choices {
		parts := make([]googlePart, 0, len(ch.Message.Parts))
		for _, p := range ch.Message.Parts {
			if encoded, ok := encodeResponsePart(p, ctx); ok {
				parts = append(parts, encoded)
			}
		}
		fr := encodeGoogleFinishReason(ch.FinishReason, ch.RawFinishReason)
		if fr == "" {
			fr = "STOP"
		}
		gResp.Candidates = append(gResp.Candidates, googleCandidate{
			Index: ch.Index,
			Content: &googleContent{
				Role:  "model",
				Parts: parts,
			},
			FinishReason: fr,
		})
	}

	return json.Marshal(gResp)
}

func encodeResponsePart(p ir.ContentPart, ctx *ir.ConversionContext) (googlePart, bool) {
	switch p.Type {
	case ir.PartText:
		part := googlePart{Text: strPtr(p.Text)}
		if p.ProviderMetadata != nil {
			if sig, ok := p.ProviderMetadata["thought_signature"].(string); ok && sig != "" {
				if ctx == nil || ctx.ShouldPreserveMetadata(ir.FormatGoogleGenAI) {
					part.ThoughtSignature = sig
				}
			}
		}
		return part, true

	case ir.PartRefusal:
		if p.Refusal == "" {
			return googlePart{}, false
		}
		return googlePart{Text: strPtr(p.Refusal)}, true

	case ir.PartReasoning:
		if p.Reasoning == nil {
			return googlePart{}, false
		}
		sig := ""
		if p.Reasoning.Signature != "" && (p.Reasoning.OriginFormat == ir.FormatGoogleGenAI || (ctx != nil && ctx.ShouldPreserveMetadata(p.Reasoning.OriginFormat))) {
			sig = p.Reasoning.Signature
		}
		if p.Reasoning.Text == "" && sig == "" {
			return googlePart{}, false
		}
		return googlePart{
			Text:             strPtr(p.Reasoning.Text),
			Thought:          true,
			ThoughtSignature: sig,
		}, true

	case ir.PartToolCall:
		if p.ToolCall == nil {
			return googlePart{}, false
		}
		return googlePart{
			FunctionCall: &googleFunctionCall{
				ID:   p.ToolCall.ID,
				Name: p.ToolCall.Name,
				Args: parseToolArgsToObject(p.ToolCall.Arguments),
			},
			ThoughtSignature: p.ToolCall.ThoughtSignature,
		}, true

	case ir.PartImage:
		if p.Image == nil {
			return googlePart{}, false
		}
		if p.Image.Data != "" {
			mime := p.Image.MediaType
			if mime == "" {
				mime = "image/png"
			}
			return googlePart{
				InlineData: &googleInlineData{
					MimeType: mime,
					Data:     p.Image.Data,
				},
			}, true
		}
		if p.Image.URL != "" {
			if mime, data, ok := ir.ParseDataURL(p.Image.URL); ok {
				return googlePart{
					InlineData: &googleInlineData{
						MimeType: mime,
						Data:     data,
					},
				}, true
			}
			return googlePart{
				FileData: &googleFileData{
					MimeType: p.Image.MediaType,
					FileURI:  p.Image.URL,
				},
			}, true
		}
		return googlePart{}, false

	case ir.PartAudio:
		if p.Audio == nil {
			return googlePart{}, false
		}
		mime := p.Audio.MediaType
		if mime == "" {
			mime = mimeFromAudioFormat(p.Audio.Format)
		}
		if p.Audio.Data != "" {
			return googlePart{
				InlineData: &googleInlineData{
					MimeType: mime,
					Data:     p.Audio.Data,
				},
			}, true
		}
		if p.Audio.URL != "" {
			return googlePart{
				FileData: &googleFileData{
					MimeType: mime,
					FileURI:  p.Audio.URL,
				},
			}, true
		}
		return googlePart{}, false

	case ir.PartFile:
		if p.File == nil {
			return googlePart{}, false
		}
		if p.File.Text != "" && p.File.Data == "" && p.File.URL == "" && p.File.FileID == "" {
			return googlePart{Text: strPtr(p.File.Text)}, true
		}
		if p.File.Data != "" {
			mime := p.File.MediaType
			if mime == "" {
				mime = "application/octet-stream"
			}
			return googlePart{
				InlineData: &googleInlineData{
					MimeType: mime,
					Data:     p.File.Data,
				},
			}, true
		}
		uri := p.File.URL
		if uri == "" {
			uri = p.File.FileID
		}
		if uri != "" {
			return googlePart{
				FileData: &googleFileData{
					MimeType: p.File.MediaType,
					FileURI:  uri,
				},
			}, true
		}
		return googlePart{}, false

	default:
		if strings.TrimSpace(p.Text) != "" {
			return googlePart{Text: strPtr(p.Text)}, true
		}
		return googlePart{}, false
	}
}
