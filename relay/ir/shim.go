package ir

import (
	"bytes"
	"encoding/json"
)

// JSONTransform mutates a raw JSON object map in-place to adapt provider dialect quirks
// without polluting core protocol codecs.
type JSONTransform func(payload map[string]any, ctx *ConversionContext) error

// Shim declares a provider dialect layered on top of a canonical wire Format.
type Shim struct {
	Name                     string
	BaseFormat               Format
	PreIRRequestTransforms   []JSONTransform
	PostIRRequestTransforms  []JSONTransform
	PreIRResponseTransforms  []JSONTransform
	PostIRResponseTransforms []JSONTransform
}

// ApplyJSONTransforms runs a transform chain over a JSON object payload.
// If transforms is empty, raw is returned untouched without parsing.
// Uses json.Decoder.UseNumber() to preserve 64-bit integer precision (e.g. seed).
func ApplyJSONTransforms(raw []byte, transforms []JSONTransform, ctx *ConversionContext) ([]byte, error) {
	if len(transforms) == 0 || len(raw) == 0 {
		return raw, nil
	}
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if obj == nil {
		obj = make(map[string]any)
	}
	for _, fn := range transforms {
		if fn == nil {
			continue
		}
		if err := fn(obj, ctx); err != nil {
			return nil, err
		}
	}
	return json.Marshal(obj)
}

// StripFields removes top-level keys unsupported by a specific vendor dialect.
func StripFields(fields ...string) JSONTransform {
	return func(payload map[string]any, _ *ConversionContext) error {
		for _, f := range fields {
			delete(payload, f)
		}
		return nil
	}
}

// SetField unconditionally sets a top-level field on the JSON object.
func SetField(field string, value any) JSONTransform {
	return func(payload map[string]any, _ *ConversionContext) error {
		payload[field] = value
		return nil
	}
}

// SetDefaultField sets a top-level field if it is absent, nil, or an empty string.
func SetDefaultField(field string, value any) JSONTransform {
	return func(payload map[string]any, _ *ConversionContext) error {
		existing, ok := payload[field]
		if !ok || existing == nil {
			payload[field] = value
			return nil
		}
		if s, isStr := existing.(string); isStr && s == "" {
			payload[field] = value
		}
		return nil
	}
}

// RenameField moves a top-level field when present.
func RenameField(from, to string) JSONTransform {
	return func(payload map[string]any, _ *ConversionContext) error {
		if val, ok := payload[from]; ok {
			payload[to] = val
			delete(payload, from)
		}
		return nil
	}
}

// StripGeminiFunctionResponseID removes functionResponse.id and functionCall.id from Google GenAI
// contents for Vertex AI compatibility.
func StripGeminiFunctionResponseID() JSONTransform {
	return func(payload map[string]any, _ *ConversionContext) error {
		contents, ok := payload["contents"].([]any)
		if !ok {
			return nil
		}
		for _, item := range contents {
			contentMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			parts, ok := contentMap["parts"].([]any)
			if !ok {
				continue
			}
			for _, p := range parts {
				partMap, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if fr, ok := partMap["functionResponse"].(map[string]any); ok {
					delete(fr, "id")
				}
				if fr, ok := partMap["function_response"].(map[string]any); ok {
					delete(fr, "id")
				}
				if fc, ok := partMap["functionCall"].(map[string]any); ok {
					delete(fc, "id")
				}
				if fc, ok := partMap["function_call"].(map[string]any); ok {
					delete(fc, "id")
				}
			}
		}
		return nil
	}
}

// Built-in provider shims for Vertex AI and AWS Bedrock dialects.
var (
	// VertexClaudeShim adapts standard Anthropic Messages requests for Vertex rawPredict/streamRawPredict.
	VertexClaudeShim = &Shim{
		Name:       "vertex--anthropic_messages",
		BaseFormat: FormatAnthropic,
		PostIRRequestTransforms: []JSONTransform{
			StripFields("model"),
			SetField("anthropic_version", "vertex-2023-10-16"),
		},
	}

	// VertexGeminiShim adapts standard Google GenAI requests for Vertex AI Gemini endpoints.
	VertexGeminiShim = &Shim{
		Name:       "vertex--google_genai",
		BaseFormat: FormatGoogleGenAI,
		PostIRRequestTransforms: []JSONTransform{
			StripGeminiFunctionResponseID(),
		},
	}

	// BedrockClaudeShim adapts standard Anthropic Messages requests for AWS Bedrock InvokeModel.
	BedrockClaudeShim = &Shim{
		Name:       "bedrock--anthropic_messages",
		BaseFormat: FormatAnthropic,
		PostIRRequestTransforms: []JSONTransform{
			StripFields("model", "stream"),
			SetField("anthropic_version", "bedrock-2023-05-31"),
		},
	}

	// DeepSeekChatShim strips OpenAI fields unsupported by DeepSeek Chat API.
	DeepSeekChatShim = &Shim{
		Name:       "deepseek--openai_chat",
		BaseFormat: FormatOpenAIChat,
		PostIRRequestTransforms: []JSONTransform{
			StripFields("n", "logit_bias", "seed"),
		},
	}

	// CodexResponsesShim adapts OpenAI Responses payloads for ChatGPT/Codex backend constraints.
	CodexResponsesShim = &Shim{
		Name:       "codex--openai_responses",
		BaseFormat: FormatOpenAIResponses,
		PostIRRequestTransforms: []JSONTransform{
			SetDefaultField("instructions", ""),
			SetField("store", false),
			StripFields("max_output_tokens", "temperature"),
		},
	}
)
