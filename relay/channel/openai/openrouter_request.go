package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func isOpenRouterNative(info *relaycommon.RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter && !info.ChannelOtherSettings.IsOpenRouterEnterprise()
}

func openRouterInvalid(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(fmt.Errorf("OpenRouter: %s", message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func openRouterRequestURL(info *relaycommon.RelayInfo) (string, error) {
	base := openrouter.NormalizeBaseURL(info.ChannelBaseUrl)
	if info.RelayMode == relayconstant.RelayModeDecisions && info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		return "", openRouterInvalid("native Decisions requires a standard channel")
	}
	if !isOpenRouterNative(info) {
		if info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini {
			if info.RelayMode != relayconstant.RelayModeResponses && info.RelayMode != relayconstant.RelayModeResponsesCompact {
				return base + "/v1/chat/completions", nil
			}
		}
		return base + info.RequestURLPath, nil
	}
	if info.RelayMode == relayconstant.RelayModeDecisions {
		return base + "/alpha/decisions", nil
	}
	var path string
	switch {
	case info.RelayMode == relayconstant.RelayModeResponses:
		path = "/responses"
	case info.RelayMode == relayconstant.RelayModeResponsesCompact:
		return "", openRouterInvalid("responses/compact is not supported")
	case info.GetFinalRequestRelayFormat() == types.RelayFormatClaude:
		path = "/messages"
	case info.RelayMode == relayconstant.RelayModeChatCompletions || info.RelayFormat == types.RelayFormatGemini || (info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAI):
		path = "/chat/completions"
	case info.RelayMode == relayconstant.RelayModeEmbeddings:
		path = "/embeddings"
	case info.RelayMode == relayconstant.RelayModeRerank:
		path = "/rerank"
	case info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits:
		path = "/images"
	case info.RelayMode == relayconstant.RelayModeAudioSpeech:
		path = "/audio/speech"
	case info.RelayMode == relayconstant.RelayModeAudioTranscription:
		path = "/audio/transcriptions"
	default:
		return "", openRouterInvalid("this endpoint is not supported")
	}
	return base + "/v1" + path, nil
}

func convertOpenRouterChat(info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	// Explicit reasoning is authoritative. Legacy suffixes only supply defaults.
	if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) && strings.HasSuffix(info.UpstreamModelName, "-thinking") {
		info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
		request.Model = info.UpstreamModelName
		if len(request.Reasoning) == 0 && request.ReasoningEffort == "" && len(request.THINKING) == 0 {
			request.Reasoning = json.RawMessage(`{"enabled":true}`)
		}
	}
	if len(request.Reasoning) == 0 && request.ReasoningEffort == "" && len(request.THINKING) > 0 && strings.HasPrefix(info.UpstreamModelName, "anthropic/") {
		var thinking dto.Thinking
		if err := common.Unmarshal(request.THINKING, &thinking); err != nil {
			return nil, openRouterInvalid("invalid thinking configuration")
		}
		reasoning := map[string]any{}
		switch thinking.Type {
		case "disabled":
			reasoning["enabled"] = false
		case "adaptive":
			reasoning["enabled"] = true
		case "enabled":
			if thinking.BudgetTokens == nil {
				return nil, openRouterInvalid("thinking.budget_tokens is required when enabled")
			}
			reasoning["enabled"], reasoning["max_tokens"] = true, *thinking.BudgetTokens
		}
		var err error
		request.Reasoning, err = common.Marshal(reasoning)
		if err != nil {
			return nil, err
		}
	}
	request.THINKING = nil
	if info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		if len(request.Usage) == 0 {
			request.Usage = json.RawMessage(`{"include":true}`)
		}
		body, err := common.Marshal(request)
		if err != nil {
			return nil, err
		}
		body, err = validateOpenRouterBody(info, body)
		if err != nil {
			return nil, err
		}
		if err = common.Unmarshal(body, request); err != nil {
			return nil, err
		}
	}
	// Keep reasoning_effort until final validation, after channel overrides.
	return request, nil
}

func openRouterObject(v any) (map[string]json.RawMessage, error) {
	b, err := common.Marshal(v)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	err = common.Unmarshal(b, &fields)
	return fields, err
}

func hasOpenRouterValue(raw json.RawMessage) bool {
	return len(raw) != 0 && string(bytes.TrimSpace(raw)) != "null"
}

// Validate the effective body, including parameter overrides and passthrough.
// Never reload client fields here: that would bypass disabled-field policies.
func validateOpenRouterBody(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, openRouterInvalid("request body must be a JSON object")
	}
	var model string
	if common.Unmarshal(fields["model"], &model) != nil || strings.TrimSpace(model) == "" {
		return nil, openRouterInvalid("model is required")
	}
	if info.RelayMode == relayconstant.RelayModeDecisions {
		var request dto.OpenRouterDecisionsRequest
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, openRouterInvalid(err.Error())
		}
	}
	if info.RelayMode == relayconstant.RelayModeResponses {
		for _, name := range []string{"store", "background"} {
			if hasOpenRouterValue(fields[name]) {
				var enabled bool
				if err := common.Unmarshal(fields[name], &enabled); err != nil || enabled {
					return nil, openRouterInvalid(name + " must be false; only stateless synchronous Responses are supported")
				}
			}
		}
		if hasOpenRouterValue(fields["conversation"]) {
			return nil, openRouterInvalid("conversation storage is not supported; send the full history in input")
		}
		if hasOpenRouterValue(fields["previous_response_id"]) {
			return nil, openRouterInvalid("previous_response_id is not supported; send the full history in input")
		}
	}
	if info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAI || info.RelayMode == relayconstant.RelayModeChatCompletions {
		var effort string
		if hasOpenRouterValue(fields["reasoning_effort"]) {
			if err := common.Unmarshal(fields["reasoning_effort"], &effort); err != nil {
				return nil, openRouterInvalid("reasoning_effort must be a string")
			}
		}
		if effort != "" {
			reasoning := map[string]json.RawMessage{}
			if hasOpenRouterValue(fields["reasoning"]) {
				if err := common.Unmarshal(fields["reasoning"], &reasoning); err != nil || reasoning == nil {
					return nil, openRouterInvalid("reasoning must be an object")
				}
			}
			var nested string
			if hasOpenRouterValue(reasoning["effort"]) {
				if err := common.Unmarshal(reasoning["effort"], &nested); err != nil {
					return nil, openRouterInvalid("reasoning.effort must be a string")
				}
			}
			if nested != "" && nested != effort {
				return nil, openRouterInvalid("reasoning.effort conflicts with reasoning_effort")
			}
			reasoning["effort"], _ = common.Marshal(effort)
			fields["reasoning"], _ = common.Marshal(reasoning)
			delete(fields, "reasoning_effort")
		}
	}
	if info.RelayMode == relayconstant.RelayModeRerank {
		if raw, ok := fields["return_documents"]; ok {
			if err := common.Unmarshal(raw, &info.ReturnDocuments); err != nil {
				return nil, openRouterInvalid("return_documents must be a boolean")
			}
		}
		delete(fields, "return_documents")
	}
	if info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits {
		var prompt string
		if common.Unmarshal(fields["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" {
			return nil, openRouterInvalid("prompt is required")
		}
		if hasOpenRouterValue(fields["mask"]) {
			return nil, openRouterInvalid("mask editing is not supported; use input_references")
		}
		if hasOpenRouterValue(fields["response_format"]) {
			var format string
			if common.Unmarshal(fields["response_format"], &format) != nil || (format != "" && format != "b64_json") {
				return nil, openRouterInvalid("images only support response_format=b64_json")
			}
		}
		delete(fields, "response_format")
		if info.RelayMode == relayconstant.RelayModeImagesEdits {
			var refs []json.RawMessage
			if common.Unmarshal(fields["input_references"], &refs) != nil || len(refs) == 0 {
				return nil, openRouterInvalid("image editing requires input_references")
			}
		}
	}
	if info.RelayMode == relayconstant.RelayModeAudioSpeech || info.RelayMode == relayconstant.RelayModeAudioTranscription {
		var stream bool
		if hasOpenRouterValue(fields["stream"]) {
			if common.Unmarshal(fields["stream"], &stream) != nil || stream {
				return nil, openRouterInvalid("audio endpoints do not support SSE")
			}
		}
		delete(fields, "stream")
		var format string
		if hasOpenRouterValue(fields["response_format"]) && common.Unmarshal(fields["response_format"], &format) != nil {
			return nil, openRouterInvalid("response_format must be a string")
		}
		if info.RelayMode == relayconstant.RelayModeAudioSpeech {
			var input dto.AudioRequest
			if common.Unmarshal(body, &input) != nil || strings.TrimSpace(input.Input) == "" {
				return nil, openRouterInvalid("speech input must be text or non-empty turns")
			}
			if format != "" && format != "pcm" && format != "mp3" {
				return nil, openRouterInvalid("speech response_format must be pcm or mp3")
			}
			var streamFormat string
			if hasOpenRouterValue(fields["stream_format"]) && (common.Unmarshal(fields["stream_format"], &streamFormat) != nil || (streamFormat != "" && streamFormat != "audio")) {
				return nil, openRouterInvalid("speech returns binary audio, not SSE")
			}
			delete(fields, "stream_format")
		} else {
			if hasOpenRouterValue(fields["stream_format"]) {
				return nil, openRouterInvalid("transcriptions do not support stream_format")
			}
			if format != "" && format != "json" && format != "verbose_json" {
				return nil, openRouterInvalid("transcription response_format must be json or verbose_json")
			}
			if !hasOpenRouterValue(fields["input_audio"]) {
				return nil, openRouterInvalid("input_audio is required")
			}
		}
	}
	return common.Marshal(fields)
}

func (a *Adaptor) doOpenRouterRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if _, err := openRouterRequestURL(info); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	data, err = validateOpenRouterBody(info, data)
	if err != nil {
		return nil, err
	}
	// Ping must not commit 200 before an upstream validation failure is known.
	info.DisablePing = true
	relaycommon.SetReasoningEffortFromRequest(info, data)
	relaycommon.SetConversationUpstreamRequest(info, data)
	return channel.DoApiRequest(a, c, info, bytes.NewReader(data))
}
