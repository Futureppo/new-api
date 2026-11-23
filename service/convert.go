package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/ir"
	_ "github.com/QuantumNous/new-api/relay/ir/codec/all"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

func irBridgeScope(info *relaycommon.RelayInfo) string {
	return relaycommon.IRBridgeScope(info)
}

func irBridgeForScope(scope string) *ir.TurnBridge {
	if scope == "" {
		return nil
	}
	return ir.DefaultTurnBridge()
}

// ClaudeToOpenAIRequest converts an Anthropic Messages request into an OpenAI Chat Completions request.
// When global.ir_conversion_enabled is true (default), translation goes through the provider-neutral IR engine.
func ClaudeToOpenAIRequest(claudeRequest dto.ClaudeRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	if !model_setting.IsIRConversionEnabled() {
		return claudeToOpenAIRequestLegacy(claudeRequest, info)
	}

	raw, err := common.Marshal(claudeRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal claude request failed: %w", err)
	}

	isOpenRouter := info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter
	scope := irBridgeScope(info)
	p, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       irBridgeForScope(scope),
		BridgeScope:  scope,
		SourceFormat: ir.FormatAnthropic,
		TargetFormat: ir.FormatOpenAIChat,
		Options: map[string]any{
			"disable_stream_options": true, // callers manage StreamOptions based on channel capabilities
			"preserve_cache_control": isOpenRouter,
		},
	})
	if err != nil {
		return nil, err
	}

	outBytes, _, _, err := p.ConvertRequest(raw)
	if err != nil {
		return nil, err
	}

	var openAIRequest dto.GeneralOpenAIRequest
	if err := common.Unmarshal(outBytes, &openAIRequest); err != nil {
		return nil, fmt.Errorf("unmarshal converted openai request failed: %w", err)
	}

	// Preserve OpenRouter-specific reasoning/verbosity extensions and non-OpenRouter -thinking suffix semantics.
	if isOpenRouter {
		if effort := claudeRequest.GetEfforts(); effort != "" {
			effortBytes, _ := json.Marshal(effort)
			openAIRequest.Verbosity = effortBytes
		}
		if claudeRequest.Thinking != nil {
			var reasoning openrouter.RequestReasoning
			if claudeRequest.Thinking.Type == "enabled" {
				reasoning = openrouter.RequestReasoning{
					Enabled:   true,
					MaxTokens: claudeRequest.Thinking.GetBudgetTokens(),
				}
			} else if claudeRequest.Thinking.Type == "adaptive" {
				reasoning = openrouter.RequestReasoning{
					Enabled: true,
				}
			}
			if reasoning.Enabled {
				reasoningJSON, err := json.Marshal(reasoning)
				if err != nil {
					return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
				}
				openAIRequest.Reasoning = reasoningJSON
				// Avoid sending mutually exclusive reasoning_effort alongside OpenRouter's reasoning object.
				openAIRequest.ReasoningEffort = ""
			}
		}
	} else if info != nil {
		const thinkingSuffix = "-thinking"
		if strings.HasSuffix(info.OriginModelName, thinkingSuffix) &&
			!strings.HasSuffix(openAIRequest.Model, thinkingSuffix) {
			openAIRequest.Model = openAIRequest.Model + thinkingSuffix
		}
	}

	return &openAIRequest, nil
}

// GeminiToOpenAIRequest converts a Google GenAI request into an OpenAI Chat Completions request.
// When global.ir_conversion_enabled is true (default), translation goes through the provider-neutral IR engine.
func GeminiToOpenAIRequest(geminiRequest *dto.GeminiChatRequest, info *relaycommon.RelayInfo) (*dto.GeneralOpenAIRequest, error) {
	if !model_setting.IsIRConversionEnabled() {
		return geminiToOpenAIRequestLegacy(geminiRequest, info)
	}
	if geminiRequest == nil {
		return nil, fmt.Errorf("gemini request is nil")
	}

	raw, err := common.Marshal(geminiRequest)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request failed: %w", err)
	}

	scope := irBridgeScope(info)
	p, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       irBridgeForScope(scope),
		BridgeScope:  scope,
		SourceFormat: ir.FormatGoogleGenAI,
		TargetFormat: ir.FormatOpenAIChat,
		Options: map[string]any{
			"disable_stream_options": true,
		},
	})
	if err != nil {
		return nil, err
	}

	outBytes, _, _, err := p.ConvertRequest(raw)
	if err != nil {
		return nil, err
	}

	var openAIRequest dto.GeneralOpenAIRequest
	if err := common.Unmarshal(outBytes, &openAIRequest); err != nil {
		return nil, fmt.Errorf("unmarshal converted openai request failed: %w", err)
	}
	if openAIRequest.Model == "" && info != nil && info.ChannelMeta != nil {
		openAIRequest.Model = info.UpstreamModelName
	}
	if info != nil && info.IsStream && openAIRequest.Stream == nil {
		stream := true
		openAIRequest.Stream = &stream
	}
	return &openAIRequest, nil
}

// ResponseOpenAI2Claude converts a non-streaming OpenAI Chat response into an Anthropic Messages response.
func ResponseOpenAI2Claude(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.ClaudeResponse {
	if openAIResponse == nil {
		return nil
	}
	if !model_setting.IsIRConversionEnabled() {
		return responseOpenAI2ClaudeLegacy(openAIResponse, info)
	}

	raw, err := common.Marshal(openAIResponse)
	if err != nil {
		return responseOpenAI2ClaudeLegacy(openAIResponse, info)
	}
	scope := irBridgeScope(info)
	p, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       irBridgeForScope(scope),
		BridgeScope:  scope,
		SourceFormat: ir.FormatAnthropic,
		TargetFormat: ir.FormatOpenAIChat,
	})
	if err != nil {
		return responseOpenAI2ClaudeLegacy(openAIResponse, info)
	}

	fallbackModel := openAIResponse.Model
	if fallbackModel == "" && info != nil {
		fallbackModel = info.OriginModelName
	}
	outBytes, _, _, err := p.ConvertResponse(raw, fallbackModel)
	if err != nil {
		return responseOpenAI2ClaudeLegacy(openAIResponse, info)
	}

	var claudeResp dto.ClaudeResponse
	if err := common.Unmarshal(outBytes, &claudeResp); err != nil {
		return responseOpenAI2ClaudeLegacy(openAIResponse, info)
	}
	if claudeResp.Model == "" && info != nil {
		claudeResp.Model = info.OriginModelName
	}
	return &claudeResp
}

// ResponseOpenAI2Gemini converts a non-streaming OpenAI Chat response into a Google GenAI response.
func ResponseOpenAI2Gemini(openAIResponse *dto.OpenAITextResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	if openAIResponse == nil {
		return nil
	}
	if !model_setting.IsIRConversionEnabled() {
		return responseOpenAI2GeminiLegacy(openAIResponse, info)
	}

	raw, err := common.Marshal(openAIResponse)
	if err != nil {
		return responseOpenAI2GeminiLegacy(openAIResponse, info)
	}
	scope := irBridgeScope(info)
	p, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       irBridgeForScope(scope),
		BridgeScope:  scope,
		SourceFormat: ir.FormatGoogleGenAI,
		TargetFormat: ir.FormatOpenAIChat,
	})
	if err != nil {
		return responseOpenAI2GeminiLegacy(openAIResponse, info)
	}

	fallbackModel := openAIResponse.Model
	if fallbackModel == "" && info != nil {
		fallbackModel = info.OriginModelName
	}
	outBytes, _, _, err := p.ConvertResponse(raw, fallbackModel)
	if err != nil {
		return responseOpenAI2GeminiLegacy(openAIResponse, info)
	}

	var geminiResp dto.GeminiChatResponse
	if err := common.Unmarshal(outBytes, &geminiResp); err != nil {
		return responseOpenAI2GeminiLegacy(openAIResponse, info)
	}
	return &geminiResp
}

// NormalizeCacheCreationSplit normalizes 5m/1h Claude cache creation token splits.
func NormalizeCacheCreationSplit(totalTokens int, tokens5m int, tokens1h int) (int, int) {
	return ir.NormalizeCacheCreationSplit(totalTokens, tokens5m, tokens1h)
}

// StreamResponseOpenAI2Claude converts a single OpenAI Chat stream chunk to Anthropic SSE chunks.
// Full end-to-end streaming cross-protocol routes run through relay/ir's stateful StreamTranslator;
// this per-chunk entrypoint preserves compatibility for direct adaptor/test callers.
func StreamResponseOpenAI2Claude(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) []*dto.ClaudeResponse {
	return streamResponseOpenAI2ClaudeLegacy(openAIResponse, info)
}

// StreamResponseOpenAI2Gemini converts a single OpenAI Chat stream chunk to a Gemini response chunk.
// Full end-to-end streaming cross-protocol routes run through relay/ir's stateful StreamTranslator;
// this per-chunk entrypoint preserves compatibility for direct adaptor/test callers.
func StreamResponseOpenAI2Gemini(openAIResponse *dto.ChatCompletionsStreamResponse, info *relaycommon.RelayInfo) *dto.GeminiChatResponse {
	return streamResponseOpenAI2GeminiLegacy(openAIResponse, info)
}
