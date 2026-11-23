package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/openaimodel"
	"github.com/QuantumNous/new-api/relay/channel"
	awsChannel "github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/ir"
	_ "github.com/QuantumNous/new-api/relay/ir/codec/all"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type irTargetSpec struct {
	Format              ir.Format
	Shim                *ir.Shim
	RelayMode           int
	URLPath             string
	RelayFormat         types.RelayFormat
	ModelRewrite        string
	ClearStreamOptions  bool
	IsBedrockClaude     bool
	ForceUpstreamStream bool
}

func isIRSupportedRelayMode(sourceFormat ir.Format, mode int) bool {
	switch sourceFormat {
	case ir.FormatOpenAIChat:
		return mode == relayconstant.RelayModeChatCompletions
	case ir.FormatOpenAIResponses:
		return mode == relayconstant.RelayModeResponses
	case ir.FormatAnthropic:
		return mode == relayconstant.RelayModeUnknown || mode == relayconstant.RelayModeChatCompletions
	case ir.FormatGoogleGenAI:
		return mode == relayconstant.RelayModeGemini || mode == relayconstant.RelayModeUnknown
	default:
		return false
	}
}

// resolveTargetIRSpec determines the upstream canonical wire protocol and shim for adaptor.
// Returns ok=false when the channel/model should stay on its native or specialized legacy path.
func resolveTargetIRSpec(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, sourceFormat ir.Format) (irTargetSpec, bool) {
	if info == nil || adaptor == nil || sourceFormat == ir.FormatUnknown {
		return irTargetSpec{}, false
	}

	// Channels with bespoke transport / wrapping stay on their dedicated paths.
	switch info.ChannelType {
	case constant.ChannelTypeCline,
		constant.ChannelTypeKilo,
		constant.ChannelTypeGMICloud,
		constant.ChannelTypeXunfeiMaas,
		constant.ChannelTypeBilibiliWeb:
		return irTargetSpec{}, false
	case constant.ChannelTypeOpenCode, constant.ChannelTypeOpenCodeGo:
		// OpenCode free/chat/claude paths have dedicated stream guards and tool handling.
		// Only bridge Gemini -> OpenCode Responses where opencode/adaptor.go otherwise returns 400.
		if sourceFormat == ir.FormatGoogleGenAI {
			ep := constant.GetOpenCodeEndpoint(info.UpstreamModelName)
			if info.ChannelType == constant.ChannelTypeOpenCodeGo {
				ep = constant.GetOpenCodeGoEndpoint(info.UpstreamModelName)
			}
			if ep == constant.OpenCodeEndpointResponses {
				return irTargetSpec{
					Format:      ir.FormatOpenAIResponses,
					RelayMode:   relayconstant.RelayModeResponses,
					URLPath:     "/v1/responses",
					RelayFormat: types.RelayFormatOpenAIResponses,
				}, true
			}
		}
		return irTargetSpec{}, false
	}

	switch info.ApiType {
	case constant.APITypeCodex:
		return irTargetSpec{
			Format:              ir.FormatOpenAIResponses,
			Shim:                ir.CodexResponsesShim,
			RelayMode:           relayconstant.RelayModeResponses,
			URLPath:             "/v1/responses",
			RelayFormat:         types.RelayFormatOpenAIResponses,
			ForceUpstreamStream: true,
		}, true

	case constant.APITypeOpenRouter:
		if !info.ChannelOtherSettings.IsOpenRouterEnterprise() {
			// OpenRouter native natively supports Chat, Responses, and Claude Messages.
			if sourceFormat == ir.FormatOpenAIChat || sourceFormat == ir.FormatOpenAIResponses || sourceFormat == ir.FormatAnthropic {
				return irTargetSpec{}, false
			}
			if shouldChatCompletionsUseResponses(info) {
				return irTargetSpec{
					Format:      ir.FormatOpenAIResponses,
					RelayMode:   relayconstant.RelayModeResponses,
					URLPath:     "/v1/responses",
					RelayFormat: types.RelayFormatOpenAIResponses,
				}, true
			}
			return irTargetSpec{
				Format:      ir.FormatOpenAIChat,
				RelayMode:   relayconstant.RelayModeChatCompletions,
				URLPath:     "/v1/chat/completions",
				RelayFormat: types.RelayFormatOpenAI,
			}, true
		}
		if sourceFormat == ir.FormatOpenAIResponses {
			return irTargetSpec{}, false
		}
		if shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeAnthropic:
		return irTargetSpec{
			Format:      ir.FormatAnthropic,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/messages",
			RelayFormat: types.RelayFormatClaude,
		}, true

	case constant.APITypeGemini:
		model := info.UpstreamModelName
		if strings.HasPrefix(model, "imagen") ||
			strings.HasPrefix(model, "text-embedding") ||
			strings.HasPrefix(model, "embedding") ||
			strings.HasPrefix(model, "gemini-embedding") {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:      ir.FormatGoogleGenAI,
			RelayMode:   relayconstant.RelayModeGemini,
			RelayFormat: types.RelayFormatGemini,
		}, true

	case constant.APITypeVertexAi:
		va, ok := adaptor.(*vertex.Adaptor)
		if !ok {
			return irTargetSpec{}, false
		}
		target, ok := va.ResolveIRTarget(info)
		if !ok {
			return irTargetSpec{}, false
		}
		spec := irTargetSpec{
			Format:             target.Format,
			Shim:               target.Shim,
			RelayMode:          target.RelayMode,
			ModelRewrite:       target.ModelRewrite,
			ClearStreamOptions: target.ClearStream,
		}
		switch target.Format {
		case ir.FormatAnthropic:
			spec.RelayFormat = types.RelayFormatClaude
			spec.URLPath = "/v1/messages"
		case ir.FormatGoogleGenAI:
			spec.RelayFormat = types.RelayFormatGemini
		case ir.FormatOpenAIResponses:
			spec.RelayFormat = types.RelayFormatOpenAIResponses
			spec.URLPath = "/v1/responses"
		default:
			spec.RelayFormat = types.RelayFormatOpenAI
			spec.URLPath = "/v1/chat/completions"
		}
		return spec, true

	case constant.APITypeAws:
		aa, ok := adaptor.(*awsChannel.Adaptor)
		if !ok || !aa.IsBedrockClaudeModel(info) {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:          ir.FormatAnthropic,
			Shim:            ir.BedrockClaudeShim,
			RelayMode:       relayconstant.RelayModeChatCompletions,
			URLPath:         "/v1/messages",
			RelayFormat:     types.RelayFormatClaude,
			IsBedrockClaude: true,
		}, true

	case constant.APITypeOpenAI:
		// Official OpenAI Chat (including Chat->Responses) stays on its official compatibility path
		// to preserve prompt_cache_breakpoint & audio validation contracts.
		if info.ChannelType == constant.ChannelTypeOpenAI && sourceFormat == ir.FormatOpenAIChat {
			return irTargetSpec{}, false
		}
		if sourceFormat == ir.FormatOpenAIResponses {
			// Native OpenAI / Azure / OpenAI-compatible Responses endpoint: same protocol.
			return irTargetSpec{}, false
		}
		endpoint := constant.EndpointType(common.GetContextKeyString(c, constant.ContextKeyPlaygroundEndpoint))
		if endpoint != constant.EndpointTypeOpenAI && endpoint != constant.EndpointTypeCohereChat {
			useResponses := shouldChatCompletionsUseResponses(info)
			if !useResponses && info.ChannelType == constant.ChannelTypeOpenAI {
				if _, _, caps, known := openaimodel.Resolve(info.UpstreamModelName); known && (caps.ResponsesOnly || caps.Tools == openaimodel.ResponsesTools || caps.Tools == openaimodel.ResponsesReasoningTools) {
					useResponses = true
				}
			}
			if useResponses {
				return irTargetSpec{
					Format:      ir.FormatOpenAIResponses,
					RelayMode:   relayconstant.RelayModeResponses,
					URLPath:     "/v1/responses",
					RelayFormat: types.RelayFormatOpenAIResponses,
				}, true
			}
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeDeepSeek:
		// DeepSeek natively supports Chat, Claude Messages, and Responses.
		if sourceFormat != ir.FormatGoogleGenAI {
			return irTargetSpec{}, false
		}
		if shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			Shim:        ir.DeepSeekChatShim,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeVLLM,
		constant.APITypeLiteLLM:
		// Self-hosted vLLM/LiteLLM natively support both OpenAI Chat and Responses endpoints.
		if sourceFormat == ir.FormatOpenAIResponses {
			return irTargetSpec{}, false
		}
		if shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		if sourceFormat == ir.FormatOpenAIChat {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeOllama,
		constant.APITypeCerebras,
		constant.APITypeSiliconFlow,
		constant.APITypeMoonshot,
		constant.APITypeMiniMax,
		constant.APITypeZhipu,
		constant.APITypeZhipuV4,
		constant.APITypeBaiduV2,
		constant.APITypeTencent,
		constant.APITypeSubmodel:
		if sourceFormat != ir.FormatOpenAIResponses && shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeAli,
		constant.APITypeVolcEngine,
		constant.APITypeXai,
		constant.APITypePerplexity,
		constant.APITypeCloudflare,
		constant.APITypeQianwenAI,
		constant.APITypeMiMo,
		constant.APITypeAgnesAI,
		constant.APITypePoe,
		constant.APITypeXinference:
		// When admin explicitly enables ChatCompletionsToResponsesPolicy for this channel/model,
		// translate any non-Responses source format directly into OpenAI Responses via IR.
		if sourceFormat != ir.FormatOpenAIResponses && shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		// These channels natively support OpenAI Chat and (mostly) Responses/Claude;
		// bridge unsupported cross-protocol entries (e.g., Gemini -> Chat) via IR.
		if sourceFormat == ir.FormatGoogleGenAI {
			return irTargetSpec{
				Format:      ir.FormatOpenAIChat,
				RelayMode:   relayconstant.RelayModeChatCompletions,
				URLPath:     "/v1/chat/completions",
				RelayFormat: types.RelayFormatOpenAI,
			}, true
		}
		return irTargetSpec{}, false
	}

	return irTargetSpec{}, false
}

// tryIRCrossProtocolRelay intercepts cross-protocol dialogue requests when global.ir_conversion_enabled
// is enabled and translates both request and response/SSE stream through the neutral IR engine.
// Returns handled=false when the request is same-protocol, IR is disabled, or the channel uses a bespoke path.
func tryIRCrossProtocolRelay(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	adaptor channel.Adaptor,
	sourceFormat ir.Format,
	sourceReq any,
) (handled bool, usage *dto.Usage, apiErr *types.NewAPIError) {
	if !model_setting.IsIRConversionEnabled() {
		return false, nil, nil
	}
	if info == nil || adaptor == nil || sourceReq == nil || !isIRSupportedRelayMode(sourceFormat, info.RelayMode) {
		return false, nil, nil
	}

	spec, ok := resolveTargetIRSpec(c, info, adaptor, sourceFormat)
	if !ok || spec.Format == sourceFormat {
		return false, nil, nil
	}

	// Snapshot per-attempt RelayInfo fields so any pre-commit error cleanly restores state before retries.
	origFinalFmt := info.FinalRequestRelayFormat
	origChain := append([]types.RelayFormat(nil), info.RequestConversionChain...)
	origRespUsage := info.ResponsesUsageInfo
	origIsStream := info.IsStream
	origDisablePing := info.DisablePing
	origUpstreamModel := info.UpstreamModelName
	committed := false
	defer func() {
		if !committed && apiErr != nil {
			info.FinalRequestRelayFormat = origFinalFmt
			info.RequestConversionChain = origChain
			info.ResponsesUsageInfo = origRespUsage
			info.IsStream = origIsStream
			info.DisablePing = origDisablePing
			info.UpstreamModelName = origUpstreamModel
		}
	}()

	// Inject channel SystemPrompt on /v1/responses cross-protocol requests (matching codex/openai adaptors).
	if sourceFormat == ir.FormatOpenAIResponses && info.ChannelSetting.SystemPrompt != "" {
		if respReq, ok := sourceReq.(*dto.OpenAIResponsesRequest); ok && respReq != nil {
			mergeChannelSystemPromptIntoResponses(respReq, info)
		}
	}

	srcBytes, err := common.Marshal(sourceReq)
	if err != nil {
		return true, nil, types.NewError(fmt.Errorf("ir: marshal source request failed: %w", err), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	// Ensure Gemini source JSON has model populated from RelayInfo before IR decoding.
	if sourceFormat == ir.FormatGoogleGenAI {
		srcBytes, err = ir.ApplyJSONTransforms(srcBytes, []ir.JSONTransform{
			ir.SetDefaultField("model", info.UpstreamModelName),
		}, nil)
		if err != nil {
			return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
	}

	scope := relaycommon.IRBridgeScope(info)
	var bridge *ir.TurnBridge
	if scope != "" {
		bridge = ir.DefaultTurnBridge()
	}

	upstreamStream := info.IsStream || spec.ForceUpstreamStream
	opts := map[string]any{
		"attach_thought_signature": model_setting.GetGeminiSettings().FunctionCallThoughtSignatureEnabled,
		"include_usage":            info.ShouldIncludeUsage,
		"disable_stream_options":   !info.SupportStreamOptions || spec.ClearStreamOptions,
		"stream":                   upstreamStream,
		"preserve_cache_control":   info.ChannelType == constant.ChannelTypeOpenRouter,
	}
	if dm := model_setting.GetClaudeSettings().GetDefaultMaxTokens(info.UpstreamModelName); dm > 0 {
		opts["default_max_tokens"] = dm
	}

	pipeline, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       bridge,
		BridgeScope:  scope,
		SourceFormat: sourceFormat,
		TargetFormat: spec.Format,
		TargetShim:   spec.Shim,
		Options:      opts,
	})
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	targetJSON, _, convCtx, err := pipeline.ConvertRequest(srcBytes)
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if convCtx != nil && len(convCtx.Warnings) > 0 && common.DebugEnabled {
		logger.LogDebug(c, fmt.Sprintf("ir request warnings (%s->%s): %+v", sourceFormat, spec.Format, convCtx.Warnings))
	}

	// Align model and stream fields when translating from URL-driven dialects (e.g. Google GenAI),
	// stream-only upstreams (Codex), or applying Vertex OpenSource model rewrites.
	targetJSON, err = finalizeIRTargetRequestJSON(targetJSON, info, spec, upstreamStream)
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	// Apply Claude -thinking/effort and Gemini ThinkingAdaptor model-suffix adaptations.
	targetJSON, err = applyIRModelSuffixAdaptation(targetJSON, info, spec)
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	info.AppendRequestConversion(spec.RelayFormat)
	info.FinalRequestRelayFormat = spec.RelayFormat

	if spec.Format != ir.FormatGoogleGenAI {
		targetJSON, err = relaycommon.RemoveDisabledFields(targetJSON, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
	}

	if len(info.ParamOverride) > 0 {
		targetJSON, err = relaycommon.ApplyParamOverrideWithRelayInfo(targetJSON, info)
		if err != nil {
			return true, nil, newAPIErrorFromParamOverride(err)
		}
		// Vertex rawPredict rejects "model" in body; ensure ParamOverride cannot break Vertex Claude.
		if spec.Shim == ir.VertexClaudeShim {
			targetJSON, err = ir.ApplyJSONTransforms(targetJSON, []ir.JSONTransform{ir.StripFields("model")}, nil)
			if err != nil {
				return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}
	}

	if info.ChannelType == constant.ChannelTypeOpenAI {
		if spec.Format == ir.FormatOpenAIResponses {
			targetJSON, err = normalizeOfficialResponsesRequest(info, targetJSON)
			if err != nil {
				return true, nil, invalidOpenAIModelRequest(err)
			}
		} else if spec.Format == ir.FormatOpenAIChat {
			var requiresResponses bool
			targetJSON, requiresResponses, err = normalizeOfficialChatRequest(info, targetJSON)
			if err != nil {
				return true, nil, invalidOpenAIModelRequest(err)
			}
			if requiresResponses {
				spec.Format = ir.FormatOpenAIResponses
				spec.RelayMode = relayconstant.RelayModeResponses
				spec.URLPath = "/v1/responses"
				spec.RelayFormat = types.RelayFormatOpenAIResponses
				info.AppendRequestConversion(spec.RelayFormat)
				info.FinalRequestRelayFormat = spec.RelayFormat
				upgradePipe, pErr := ir.NewPipeline(ir.PipelineConfig{
					Bridge:       bridge,
					BridgeScope:  scope,
					SourceFormat: ir.FormatOpenAIChat,
					TargetFormat: ir.FormatOpenAIResponses,
					Options:      opts,
				})
				if pErr != nil {
					return true, nil, types.NewError(pErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
				targetJSON, _, _, err = upgradePipe.ConvertRequest(targetJSON)
				if err != nil {
					return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
				targetJSON, err = relaycommon.RemoveDisabledFields(targetJSON, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
				if err != nil {
					return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
				targetJSON, err = normalizeOfficialResponsesRequest(info, targetJSON)
				if err != nil {
					return true, nil, invalidOpenAIModelRequest(err)
				}
				pipeline, err = ir.NewPipeline(ir.PipelineConfig{
					Bridge:       bridge,
					BridgeScope:  scope,
					SourceFormat: sourceFormat,
					TargetFormat: ir.FormatOpenAIResponses,
					Options:      opts,
				})
				if err != nil {
					return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
			}
		}
	}

	// Initialize ResponsesUsageInfo AFTER ParamOverride and dynamic Chat->Responses upgrades so all
	// built-in tools (including search_context_size) are accounted for accurately.
	if spec.Format == ir.FormatOpenAIResponses {
		var respReq dto.OpenAIResponsesRequest
		if uErr := common.Unmarshal(targetJSON, &respReq); uErr == nil {
			info.InitResponsesUsageInfo(&respReq)
		}
	}

	relaycommon.SetReasoningEffortFromRequest(info, targetJSON)
	relaycommon.SetConversationUpstreamRequest(info, targetJSON)

	clientModel := info.ClientModelName
	if clientModel == "" {
		clientModel = info.OriginModelName
	}

	if spec.IsBedrockClaude {
		u, bErr := executeIRBedrockClaude(c, info, adaptor.(*awsChannel.Adaptor), pipeline, targetJSON, clientModel)
		committed = c.GetBool("ir_response_committed") || bErr == nil
		return true, u, bErr
	}

	u, hErr := executeIRHTTPRelay(c, info, adaptor, pipeline, spec, targetJSON, clientModel, upstreamStream)
	committed = c.GetBool("ir_response_committed") || hErr == nil
	return true, u, hErr
}

func mergeChannelSystemPromptIntoResponses(req *dto.OpenAIResponsesRequest, info *relaycommon.RelayInfo) {
	if req == nil || info == nil || info.ChannelSetting.SystemPrompt == "" {
		return
	}
	systemPrompt := info.ChannelSetting.SystemPrompt
	if len(req.Instructions) == 0 {
		if b, err := common.Marshal(systemPrompt); err == nil {
			req.Instructions = b
		}
		return
	}
	if !info.ChannelSetting.SystemPromptOverride {
		return
	}
	var existing string
	if err := common.Unmarshal(req.Instructions, &existing); err == nil {
		existing = strings.TrimSpace(existing)
		if existing == "" {
			if b, err := common.Marshal(systemPrompt); err == nil {
				req.Instructions = b
			}
		} else {
			if b, err := common.Marshal(systemPrompt + "\n" + existing); err == nil {
				req.Instructions = b
			}
		}
	} else {
		if b, err := common.Marshal(systemPrompt); err == nil {
			req.Instructions = b
		}
	}
}

func finalizeIRTargetRequestJSON(raw []byte, info *relaycommon.RelayInfo, spec irTargetSpec, upstreamStream bool) ([]byte, error) {
	if spec.Format == ir.FormatGoogleGenAI {
		return raw, nil
	}
	var transforms []ir.JSONTransform
	if spec.ModelRewrite != "" {
		transforms = append(transforms, ir.SetField("model", spec.ModelRewrite))
	} else if spec.Shim != ir.VertexClaudeShim && spec.Shim != ir.BedrockClaudeShim {
		transforms = append(transforms, ir.SetDefaultField("model", info.UpstreamModelName))
	}
	if spec.Shim != ir.BedrockClaudeShim {
		if upstreamStream {
			transforms = append(transforms, ir.SetField("stream", true))
		} else {
			transforms = append(transforms, ir.SetDefaultField("stream", false))
		}
	}
	if spec.Format == ir.FormatOpenAIChat && upstreamStream && info.SupportStreamOptions && !spec.ClearStreamOptions {
		transforms = append(transforms, func(payload map[string]any, _ *ir.ConversionContext) error {
			if _, exists := payload["stream_options"]; !exists {
				payload["stream_options"] = map[string]any{"include_usage": true}
			}
			return nil
		})
	}
	if len(transforms) == 0 {
		return raw, nil
	}
	return ir.ApplyJSONTransforms(raw, transforms, nil)
}

func applyIRModelSuffixAdaptation(raw []byte, info *relaycommon.RelayInfo, spec irTargetSpec) ([]byte, error) {
	if info == nil || info.ChannelMeta == nil {
		return raw, nil
	}
	modelName := info.UpstreamModelName
	if modelName == "" {
		return raw, nil
	}

	switch spec.Format {
	case ir.FormatAnthropic:
		// 1. Claude Opus 4.6 / 4.7 effort suffix (e.g. claude-opus-4-6-high)
		if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(modelName); ok && effortLevel != "" &&
			(strings.HasPrefix(modelName, "claude-opus-4-6") || strings.HasPrefix(modelName, "claude-opus-4-7")) {
			info.UpstreamModelName = baseModel
			return ir.ApplyJSONTransforms(raw, []ir.JSONTransform{
				func(payload map[string]any, _ *ir.ConversionContext) error {
					if spec.Shim != ir.VertexClaudeShim && spec.Shim != ir.BedrockClaudeShim {
						payload["model"] = baseModel
					}
					th := map[string]any{"type": "adaptive"}
					if strings.HasPrefix(baseModel, "claude-opus-4-7") {
						th["display"] = "summarized"
						delete(payload, "temperature")
						delete(payload, "top_p")
						delete(payload, "top_k")
					} else {
						delete(payload, "top_p")
						payload["temperature"] = 1.0
					}
					payload["thinking"] = th
					payload["output_config"] = map[string]any{"effort": effortLevel}
					return nil
				},
			}, nil)
		}
		// 2. Claude -thinking suffix when ThinkingAdapterEnabled
		if model_setting.GetClaudeSettings().ThinkingAdapterEnabled && strings.HasSuffix(modelName, "-thinking") {
			trimmedModel := strings.TrimSuffix(modelName, "-thinking")
			preserveSuffix := model_setting.ShouldPreserveThinkingSuffix(modelName)
			if !preserveSuffix {
				info.UpstreamModelName = trimmedModel
			}
			return ir.ApplyJSONTransforms(raw, []ir.JSONTransform{
				func(payload map[string]any, _ *ir.ConversionContext) error {
					if !preserveSuffix && spec.Shim != ir.VertexClaudeShim && spec.Shim != ir.BedrockClaudeShim {
						payload["model"] = trimmedModel
					}
					if strings.HasPrefix(trimmedModel, "claude-opus-4-7") {
						payload["thinking"] = map[string]any{"type": "adaptive", "display": "summarized"}
						payload["output_config"] = map[string]any{"effort": "high"}
						delete(payload, "temperature")
						delete(payload, "top_p")
						delete(payload, "top_k")
						return nil
					}
					if _, exists := payload["thinking"]; !exists {
						maxTok := 8192
						if mt, ok := payload["max_tokens"]; ok {
							switch v := mt.(type) {
							case json.Number:
								if n, err := v.Int64(); err == nil && n > 0 {
									maxTok = int(n)
								}
							case float64:
								if v > 0 {
									maxTok = int(v)
								}
							}
						}
						if maxTok < 1280 {
							maxTok = 1280
							payload["max_tokens"] = maxTok
						}
						budget := int(float64(maxTok) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)
						if budget < 1024 {
							budget = 1024
						}
						if budget >= maxTok {
							maxTok = budget + 4096
							payload["max_tokens"] = maxTok
						}
						payload["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
						delete(payload, "top_p")
						delete(payload, "top_k")
						payload["temperature"] = 1.0
					}
					return nil
				},
			}, nil)
		}

	case ir.FormatGoogleGenAI:
		if !model_setting.GetGeminiSettings().ThinkingAdapterEnabled {
			return raw, nil
		}
		isNew25Pro := strings.HasPrefix(modelName, "gemini-2.5-pro") &&
			!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-05-06") &&
			!strings.HasPrefix(modelName, "gemini-2.5-pro-preview-03-25")

		var tcPatch map[string]any
		if strings.Contains(modelName, "-thinking-") {
			parts := strings.SplitN(modelName, "-thinking-", 2)
			if len(parts) == 2 && parts[1] != "" {
				if budgetTokens, err := strconv.Atoi(parts[1]); err == nil && budgetTokens >= 0 {
					tcPatch = map[string]any{
						"thinkingBudget":  budgetTokens,
						"includeThoughts": true,
					}
				}
			}
		} else if strings.HasSuffix(modelName, "-thinking") {
			tcPatch = map[string]any{
				"includeThoughts": true,
			}
		} else if strings.HasSuffix(modelName, "-nothinking") {
			if !isNew25Pro {
				tcPatch = map[string]any{
					"thinkingBudget":  0,
					"includeThoughts": false,
				}
			}
		} else if _, level, ok := reasoning.TrimEffortSuffix(modelName); ok && level != "" {
			tcPatch = map[string]any{
				"includeThoughts": true,
				"thinkingLevel":   strings.ToUpper(level),
			}
			info.ReasoningEffort = level
		}
		if len(tcPatch) > 0 {
			return ir.ApplyJSONTransforms(raw, []ir.JSONTransform{
				func(payload map[string]any, _ *ir.ConversionContext) error {
					genCfg, _ := payload["generationConfig"].(map[string]any)
					if genCfg == nil {
						genCfg = make(map[string]any)
						payload["generationConfig"] = genCfg
					}
					if _, exists := genCfg["thinkingConfig"]; !exists {
						genCfg["thinkingConfig"] = tcPatch
					}
					return nil
				},
			}, nil)
		}

	case ir.FormatOpenAIResponses:
		// Official ChannelTypeOpenAI runs normalizeOfficialResponsesRequest separately;
		// for other Responses-capable channels, mirror openai.Adaptor.ConvertOpenAIResponsesRequest
		// suffix extraction (e.g. gpt-5-high -> model="gpt-5", reasoning.effort="high").
		if info.ChannelType != constant.ChannelTypeOpenAI {
			if effort, originModel := reasoning.ParseOpenAIReasoningEffortFromModelSuffix(modelName); effort != "" && originModel != "" {
				info.UpstreamModelName = originModel
				info.ReasoningEffort = effort
				return ir.ApplyJSONTransforms(raw, []ir.JSONTransform{
					func(payload map[string]any, _ *ir.ConversionContext) error {
						payload["model"] = originModel
						rCfg, _ := payload["reasoning"].(map[string]any)
						if rCfg == nil {
							rCfg = make(map[string]any)
							payload["reasoning"] = rCfg
						}
						rCfg["effort"] = effort
						return nil
					},
				}, nil)
			}
		}
	}

	return raw, nil
}

type irStreamWriter struct {
	c         *gin.Context
	info      *relaycommon.RelayInfo
	mu        sync.Mutex
	committed bool
}

func newIRStreamWriter(c *gin.Context, info *relaycommon.RelayInfo) *irStreamWriter {
	return &irStreamWriter{c: c, info: info}
}

func (w *irStreamWriter) isCommitted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.committed
}

func (w *irStreamWriter) writeFrame(frame ir.SSEFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.committed {
		helper.SetEventStreamHeaders(w.c)
		w.c.Set("ir_response_committed", true)
		w.committed = true
	}
	w.info.SetFirstResponseTime()
	raw := ir.FormatSSEFrame(frame)
	relaycommon.AppendConversationClientResponse(w.c, raw)
	if _, err := w.c.Writer.Write(raw); err != nil {
		return err
	}
	if err := helper.FlushWriter(w.c); err != nil {
		return err
	}
	w.info.SendResponseCount++
	return nil
}

func (w *irStreamWriter) writePing() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.committed {
		return nil
	}
	if _, err := w.c.Writer.Write([]byte(": ping\n\n")); err != nil {
		return err
	}
	return helper.FlushWriter(w.c)
}

func buildIRStreamAPIError(ev *ir.StreamEvent, skipRetry bool) *types.NewAPIError {
	msg := "upstream stream error"
	statusCode := http.StatusBadGateway
	if ev != nil {
		if ev.ErrorMessage != "" {
			msg = ev.ErrorMessage
		}
		if ev.ErrorCode != "" {
			if code, err := strconv.Atoi(strings.TrimSpace(ev.ErrorCode)); err == nil && code >= 400 && code <= 599 {
				statusCode = code
			}
		}
		if strings.Contains(strings.ToLower(ev.ErrorType), "rate_limit") {
			statusCode = http.StatusTooManyRequests
		} else if strings.Contains(strings.ToLower(ev.ErrorType), "overloaded") {
			statusCode = http.StatusServiceUnavailable
		}
	}
	var opts []types.NewAPIErrorOptions
	if skipRetry {
		opts = append(opts, types.ErrOptionWithSkipRetry())
	}
	return types.NewErrorWithStatusCode(errors.New(msg), types.ErrorCodeBadResponseBody, statusCode, opts...)
}

func executeIRBedrockClaude(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	awsAdaptor *awsChannel.Adaptor,
	pipeline *ir.Pipeline,
	targetJSON []byte,
	clientModel string,
) (*dto.Usage, *types.NewAPIError) {
	preparedBody, err := awsAdaptor.PrepareRawClaudePayload(c, info, targetJSON)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.SetConversationUpstreamRequest(info, preparedBody)

	if rpmRes := service.TryAcquireChannelRPM(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection); !rpmRes.Allowed {
		return nil, service.NewChannelRPMLimitError("")
	}

	if info.IsStream {
		if info.StreamStatus == nil {
			info.StreamStatus = relaycommon.NewStreamStatus()
		}
		sw := newIRStreamWriter(c, info)
		st := pipeline.NewStreamTranslator(clientModel)

		stopPing := make(chan struct{})
		var pingWg sync.WaitGroup
		generalSettings := operation_setting.GetGeneralSetting()
		if generalSettings.PingIntervalEnabled && !info.DisablePing {
			interval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = helper.DefaultPingInterval
			}
			pingWg.Add(1)
			go func() {
				defer pingWg.Done()
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-stopPing:
						return
					case <-c.Request.Context().Done():
						return
					case <-ticker.C:
						_ = sw.writePing()
					}
				}
			}()
		}

		var clientWriteFailed atomic.Bool
		emitErr := awsAdaptor.InvokeRawClaudeStream(c, info, preparedBody, func(chunk []byte) *types.NewAPIError {
			if info.ConversationCapture != nil && len(chunk) > 0 {
				info.ConversationCapture.AppendUpstreamResponseBody(chunk)
				info.ConversationCapture.AppendUpstreamResponseBody([]byte("\n"))
			}
			info.ReceivedResponseCount++
			frames, procErr := st.ProcessChunk("", chunk)
			if procErr != nil {
				return types.NewError(procErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
			}
			// If first chunk is an upstream EventError before headers are sent, abort with retryable API error.
			if st.StreamError() != nil && !sw.isCommitted() {
				return buildIRStreamAPIError(st.StreamError(), false)
			}
			for _, f := range frames {
				if wErr := sw.writeFrame(f); wErr != nil {
					clientWriteFailed.Store(true)
					return types.NewError(wErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
				}
			}
			return nil
		})
		close(stopPing)
		pingWg.Wait()

		if emitErr != nil {
			if emitErr.StatusCode == http.StatusTooManyRequests {
				service.RecordChannelRPM429(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
			}
			st.PersistBridgeNow()
			if sw.isCommitted() {
				if clientWriteFailed.Load() || c.Request.Context().Err() != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, emitErr)
				} else {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, emitErr)
					info.StreamStatus.RecordError(emitErr.Error())
				}
				return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), ir.FormatAnthropic), nil
			}
			if c.Request.Context().Err() != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, emitErr)
				return nil, types.NewError(emitErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
			}
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, emitErr)
			info.StreamStatus.RecordError(emitErr.Error())
			return nil, emitErr
		}

		tail, flushErr := st.Flush()
		if flushErr != nil && !sw.isCommitted() {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, flushErr)
			return nil, types.NewError(flushErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		if st.StreamError() != nil && !sw.isCommitted() {
			errObj := buildIRStreamAPIError(st.StreamError(), false)
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errObj)
			return nil, errObj
		}
		for _, f := range tail {
			if wErr := sw.writeFrame(f); wErr != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, wErr)
				return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), ir.FormatAnthropic), nil
			}
		}
		if st.StreamError() != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New(st.StreamError().ErrorMessage))
			info.StreamStatus.RecordError(st.StreamError().ErrorMessage)
			if !st.HasNonErrorOutput() && st.Usage() == nil {
				return &dto.Usage{UsageSemantic: "anthropic"}, nil
			}
		} else {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
		}
		return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), ir.FormatAnthropic), nil
	}

	respBody, apiErr := awsAdaptor.InvokeRawClaudeNonStream(c, info, preparedBody)
	if apiErr != nil {
		if apiErr.StatusCode == http.StatusTooManyRequests {
			service.RecordChannelRPM429(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
		}
		return nil, apiErr
	}
	if info.ConversationCapture != nil && len(respBody) > 0 {
		info.ConversationCapture.AppendUpstreamResponseBody(respBody)
	}
	return handleIRNonStreamResponse(c, info, pipeline, respBody, clientModel, ir.FormatAnthropic)
}

func executeIRHTTPRelay(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	adaptor channel.Adaptor,
	pipeline *ir.Pipeline,
	spec irTargetSpec,
	targetJSON []byte,
	clientModel string,
	upstreamStream bool,
) (*dto.Usage, *types.NewAPIError) {
	origRelayMode := info.RelayMode
	origURLPath := info.RequestURLPath
	origDisablePing := info.DisablePing
	origIsStream := info.IsStream
	info.RelayMode = spec.RelayMode
	if spec.URLPath != "" {
		info.RequestURLPath = spec.URLPath
	}
	// When a non-streaming client targets a stream-only upstream (e.g. Codex), temporarily signal
	// upstream streaming to adaptor.SetupRequestHeader (Accept: text/event-stream) without flipping client mode.
	if upstreamStream && !origIsStream {
		info.IsStream = true
	}
	defer func() {
		info.RelayMode = origRelayMode
		info.RequestURLPath = origURLPath
		info.DisablePing = origDisablePing
		info.IsStream = origIsStream
	}()

	resp, err := doChannelRPMGuardedRequest(c, info, func() (any, error) {
		return adaptor.DoRequest(c, info, bytes.NewReader(targetJSON))
	})
	if err != nil {
		helper.ClearEventStreamHeaders(c)
		if apiErr, ok := err.(*types.NewAPIError); ok {
			return nil, apiErr
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil {
		helper.ClearEventStreamHeaders(c)
		return nil, types.NewError(fmt.Errorf("ir: unexpected response type %T", resp), types.ErrorCodeDoRequestFailed)
	}
	if httpResp.Body != nil {
		defer httpResp.Body.Close()
	}

	if httpResp.StatusCode != http.StatusOK {
		helper.ClearEventStreamHeaders(c)
		newApiErr := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		return nil, newApiErr
	}
	if httpResp.Body == nil {
		helper.ClearEventStreamHeaders(c)
		return nil, types.NewError(errors.New("ir: upstream returned nil response body"), types.ErrorCodeReadResponseBodyFailed)
	}

	relaycommon.WrapConversationUpstreamResponse(info, httpResp)
	respIsSSE := strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")

	// Non-streaming client against a stream-only upstream (e.g. Codex): aggregate upstream SSE into JSON response.
	if !origIsStream && spec.ForceUpstreamStream && respIsSSE {
		outBytes, irResp, streamErrEv, receivedChunks, aggErr := pipeline.TranslateStreamToResponse(httpResp.Body, clientModel)
		info.ReceivedResponseCount += receivedChunks
		if aggErr != nil {
			return nil, types.NewError(aggErr, types.ErrorCodeBadResponseBody)
		}
		if streamErrEv != nil && irResp == nil {
			return nil, buildIRStreamAPIError(streamErrEv, false)
		}
		return writeIRNonStreamResponse(c, info, outBytes, irResp, spec.Format), nil
	}

	clientIsStream := origIsStream || respIsSSE
	if clientIsStream {
		return runIRHTTPStream(c, info, httpResp, pipeline, spec, clientModel)
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadResponseBodyFailed)
	}
	return handleIRNonStreamResponse(c, info, pipeline, respBody, clientModel, spec.Format)
}

func runIRHTTPStream(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	httpResp *http.Response,
	pipeline *ir.Pipeline,
	spec irTargetSpec,
	clientModel string,
) (*dto.Usage, *types.NewAPIError) {
	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	sw := newIRStreamWriter(c, info)
	st := pipeline.NewStreamTranslator(clientModel)

	doneCh := make(chan struct{})
	resetIdleCh := make(chan struct{}, 1)
	var clientGone atomic.Bool
	var timedOut atomic.Bool
	var wg sync.WaitGroup

	// Monitor client cancellation and idle StreamingTimeout to unblock reader.Next() promptly.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var idleTimer *time.Timer
		var idleC <-chan time.Time
		idleDur := time.Duration(constant.StreamingTimeout) * time.Second
		if idleDur > 0 {
			idleTimer = time.NewTimer(idleDur)
			idleC = idleTimer.C
			defer idleTimer.Stop()
		}
		for {
			select {
			case <-doneCh:
				return
			case <-c.Request.Context().Done():
				clientGone.Store(true)
				_ = httpResp.Body.Close()
				return
			case <-idleC:
				timedOut.Store(true)
				_ = httpResp.Body.Close()
				return
			case <-resetIdleCh:
				if idleTimer != nil {
					if !idleTimer.Stop() {
						select {
						case <-idleTimer.C:
						default:
						}
					}
					idleTimer.Reset(idleDur)
				}
			}
		}
	}()

	// Optional SSE keepalive ping goroutine matching helper.StreamScannerHandler semantics.
	generalSettings := operation_setting.GetGeneralSetting()
	if generalSettings.PingIntervalEnabled && !info.DisablePing {
		pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
		if pingInterval <= 0 {
			pingInterval = helper.DefaultPingInterval
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(pingInterval)
			defer ticker.Stop()
			for {
				select {
				case <-doneCh:
					return
				case <-c.Request.Context().Done():
					return
				case <-ticker.C:
					_ = sw.writePing()
				}
			}
		}()
	}

	cleanupWatchers := func() {
		select {
		case <-doneCh:
		default:
			close(doneCh)
		}
		wg.Wait()
	}
	defer cleanupWatchers()

	reader := ir.NewSSEReader(httpResp.Body)
	for {
		evName, data, readErr := reader.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			info.ReceivedResponseCount += st.ReceivedChunks()
			st.PersistBridgeNow()
			if sw.isCommitted() {
				switch {
				case clientGone.Load() || c.Request.Context().Err() != nil:
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, readErr)
				case timedOut.Load():
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, readErr)
				default:
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, readErr)
					info.StreamStatus.RecordError(readErr.Error())
				}
				return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), spec.Format), nil
			}
			if clientGone.Load() || c.Request.Context().Err() != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, readErr)
				return nil, types.NewError(readErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
			}
			if timedOut.Load() {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, readErr)
				return nil, types.NewErrorWithStatusCode(errors.New("upstream stream idle timeout"), types.ErrorCodeBadResponseBody, http.StatusGatewayTimeout)
			}
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, readErr)
			info.StreamStatus.RecordError(readErr.Error())
			return nil, types.NewError(readErr, types.ErrorCodeBadResponseBody)
		}

		select {
		case resetIdleCh <- struct{}{}:
		default:
		}

		frames, procErr := st.ProcessChunk(evName, data)
		if procErr != nil {
			info.ReceivedResponseCount += st.ReceivedChunks()
			st.PersistBridgeNow()
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, procErr)
			info.StreamStatus.RecordError(procErr.Error())
			if sw.isCommitted() {
				return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), spec.Format), nil
			}
			return nil, types.NewError(procErr, types.ErrorCodeBadResponseBody)
		}

		// If the very first semantic event is an upstream EventError before any frames were sent,
		// surface it as a real retryable *types.NewAPIError with zero billing instead of HTTP 200.
		if st.StreamError() != nil && !sw.isCommitted() {
			info.ReceivedResponseCount += st.ReceivedChunks()
			errObj := buildIRStreamAPIError(st.StreamError(), false)
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errObj)
			info.StreamStatus.RecordError(errObj.Error())
			return nil, errObj
		}

		for _, f := range frames {
			if wErr := sw.writeFrame(f); wErr != nil {
				info.ReceivedResponseCount += st.ReceivedChunks()
				st.PersistBridgeNow()
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, wErr)
				// Client disconnected mid-stream: bill all tokens consumed so far rather than refunding 100%.
				return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), spec.Format), nil
			}
		}
	}

	tail, flushErr := st.Flush()
	info.ReceivedResponseCount += st.ReceivedChunks()
	if flushErr != nil && !sw.isCommitted() {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, flushErr)
		return nil, types.NewError(flushErr, types.ErrorCodeBadResponseBody)
	}
	if st.StreamError() != nil && !sw.isCommitted() {
		errObj := buildIRStreamAPIError(st.StreamError(), false)
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errObj)
		return nil, errObj
	}
	for _, f := range tail {
		if wErr := sw.writeFrame(f); wErr != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, wErr)
			return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), spec.Format), nil
		}
	}

	if st.StreamError() != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New(st.StreamError().ErrorMessage))
		info.StreamStatus.RecordError(st.StreamError().ErrorMessage)
		if !st.HasNonErrorOutput() && st.Usage() == nil {
			sem := "openai"
			if spec.Format == ir.FormatAnthropic {
				sem = "anthropic"
			}
			return &dto.Usage{UsageSemantic: sem}, nil
		}
	} else {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	}

	return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), spec.Format), nil
}

func handleIRNonStreamResponse(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	pipeline *ir.Pipeline,
	respBody []byte,
	clientModel string,
	targetFormat ir.Format,
) (*dto.Usage, *types.NewAPIError) {
	outBytes, irResp, _, err := pipeline.ConvertResponse(respBody, clientModel)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	info.ReceivedResponseCount++
	return writeIRNonStreamResponse(c, info, outBytes, irResp, targetFormat), nil
}

func writeIRNonStreamResponse(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	outBytes []byte,
	irResp *ir.Response,
	targetFormat ir.Format,
) *dto.Usage {
	info.SetFirstResponseTime()
	c.Set("ir_response_committed", true)
	helper.ClearEventStreamHeaders(c)
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	service.IOCopyBytesGracefully(c, nil, outBytes)
	info.SendResponseCount++

	estimatedText := ""
	if irResp != nil {
		var sb strings.Builder
		for _, ch := range irResp.Choices {
			for _, p := range ch.Message.Parts {
				if p.Type == ir.PartText && p.Text != "" {
					sb.WriteString(p.Text)
				} else if p.Type == ir.PartReasoning && p.Reasoning != nil {
					sb.WriteString(p.Reasoning.Text)
				} else if p.Type == ir.PartToolCall && p.ToolCall != nil {
					sb.WriteString(p.ToolCall.Arguments)
				}
			}
		}
		estimatedText = sb.String()
	}

	var irUsage *ir.Usage
	if irResp != nil {
		irUsage = irResp.Usage
	}
	return finalizeIRUsage(c, info, irUsage, estimatedText, 1, targetFormat)
}

func finalizeIRUsage(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	irUsage *ir.Usage,
	estimatedText string,
	receivedChunks int,
	targetFormat ir.Format,
) *dto.Usage {
	var dtoUsage *dto.Usage
	if irUsage != nil {
		dtoUsage = irUsage.ToDTOUsage()
		if targetFormat == ir.FormatAnthropic && irUsage.WebSearchRequests > 0 {
			c.Set("claude_web_search_requests", irUsage.WebSearchRequests)
		}
		if targetFormat == ir.FormatOpenAIResponses && (irUsage.WebSearchRequests > 0 || irUsage.FileSearchCalls > 0) {
			if info.ResponsesUsageInfo == nil {
				info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{
					BuiltInTools: make(map[string]*relaycommon.BuildInToolInfo),
				}
			}
			if info.ResponsesUsageInfo.BuiltInTools == nil {
				info.ResponsesUsageInfo.BuiltInTools = make(map[string]*relaycommon.BuildInToolInfo)
			}
			if irUsage.WebSearchRequests > 0 {
				if existing := info.ResponsesUsageInfo.BuiltInTools["web_search_preview"]; existing != nil {
					existing.CallCount = irUsage.WebSearchRequests
				} else {
					info.ResponsesUsageInfo.BuiltInTools["web_search_preview"] = &relaycommon.BuildInToolInfo{
						ToolName:  "web_search_preview",
						CallCount: irUsage.WebSearchRequests,
					}
				}
			}
			if irUsage.FileSearchCalls > 0 {
				if existing := info.ResponsesUsageInfo.BuiltInTools["file_search"]; existing != nil {
					existing.CallCount = irUsage.FileSearchCalls
				} else {
					info.ResponsesUsageInfo.BuiltInTools["file_search"] = &relaycommon.BuildInToolInfo{
						ToolName:  "file_search",
						CallCount: irUsage.FileSearchCalls,
					}
				}
			}
		}
		if irUsage.HasImageGenCall {
			c.Set("image_generation_call", true)
			if irUsage.ImageGenQuality != "" {
				c.Set("image_generation_call_quality", irUsage.ImageGenQuality)
			}
			if irUsage.ImageGenSize != "" {
				c.Set("image_generation_call_size", irUsage.ImageGenSize)
			}
		}
	} else {
		dtoUsage = &dto.Usage{}
		if targetFormat == ir.FormatAnthropic {
			dtoUsage.UsageSemantic = "anthropic"
		} else {
			dtoUsage.UsageSemantic = "openai"
		}
	}

	// Preserve existing cache token fields while backfilling missing prompt/completion estimates.
	if receivedChunks > 0 && (dtoUsage.CompletionTokens == 0 || (dtoUsage.PromptTokens == 0 && dtoUsage.PromptTokensDetails.CachedTokens == 0 && dtoUsage.PromptTokensDetails.GetCacheCreationTokens() == 0)) {
		est := service.ResponseText2Usage(c, estimatedText, info.UpstreamModelName, info.GetEstimatePromptTokens())
		if dtoUsage.PromptTokens == 0 && dtoUsage.PromptTokensDetails.CachedTokens == 0 && dtoUsage.PromptTokensDetails.GetCacheCreationTokens() == 0 {
			dtoUsage.PromptTokens = est.PromptTokens
			dtoUsage.InputTokens = est.PromptTokens
		}
		if dtoUsage.CompletionTokens == 0 {
			dtoUsage.CompletionTokens = est.CompletionTokens
			dtoUsage.OutputTokens = est.CompletionTokens
		}
	}
	inclusivePrompt := dtoUsage.PromptTokens
	if dtoUsage.UsageSemantic == "anthropic" {
		inclusivePrompt += dtoUsage.PromptTokensDetails.CachedTokens + dtoUsage.PromptTokensDetails.GetCacheCreationTokens()
	}
	if dtoUsage.TotalTokens < inclusivePrompt+dtoUsage.CompletionTokens {
		dtoUsage.TotalTokens = inclusivePrompt + dtoUsage.CompletionTokens
	}
	return dtoUsage
}

func billIRUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	if usage == nil {
		usage = &dto.Usage{}
	}
	containAudioTokens := usage.CompletionTokenDetails.AudioTokens > 0 || usage.PromptTokensDetails.AudioTokens > 0
	containsAudioRatios := ratio_setting.ContainsAudioRatio(info.OriginModelName) || ratio_setting.ContainsAudioCompletionRatio(info.OriginModelName)
	hasNonAudioExtras := usage.PromptTokensDetails.CachedTokens > 0 ||
		usage.PromptTokensDetails.GetCacheCreationTokens() > 0 ||
		usage.PromptTokensDetails.ImageTokens > 0 ||
		(info != nil && info.ResponsesUsageInfo != nil && len(info.ResponsesUsageInfo.BuiltInTools) > 0) ||
		c.GetBool("image_generation_call") ||
		c.GetInt("claude_web_search_requests") > 0
	// PostAudioConsumeQuota only accounts for PromptTokensDetails.TextTokens + AudioTokens and skips
	// cache/image/tool surcharges; fall back to PostTextConsumeQuota whenever extras exist or TextTokens is absent.
	if containAudioTokens && containsAudioRatios && !hasNonAudioExtras && usage.PromptTokensDetails.TextTokens > 0 {
		service.PostAudioConsumeQuota(c, info, usage, "")
		return
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
}
