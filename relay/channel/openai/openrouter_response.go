package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const openRouterNativeUsage = "openrouter_native_messages"

func openRouterResponseError(raw json.RawMessage) *types.NewAPIError {
	if !hasOpenRouterValue(raw) {
		return nil
	}
	var detail types.OpenAIError
	if common.Unmarshal(raw, &detail) != nil || detail.Message == "" {
		detail.Message = "OpenRouter returned an upstream error"
	}
	status := http.StatusBadGateway
	if n, ok := detail.Code.(float64); ok && n >= 400 && n <= 599 {
		status = int(n)
	}
	return types.WithOpenAIError(detail, status)
}

func openRouterUsage(c *gin.Context, info *relaycommon.RelayInfo, raw json.RawMessage) *dto.Usage {
	if !hasOpenRouterValue(raw) {
		return nil
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil
	}
	if info.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		if _, input := fields["input_tokens"]; !input {
			if _, output := fields["output_tokens"]; !output {
				return nil
			}
		}
		var native dto.ClaudeUsage
		if common.Unmarshal(raw, &native) != nil {
			return nil
		}
		u := &dto.Usage{PromptTokens: native.InputTokens, CompletionTokens: native.OutputTokens, TotalTokens: native.InputTokens + native.OutputTokens, UsageSemantic: "anthropic", UsageSource: openRouterNativeUsage}
		u.PromptTokensDetails.CachedTokens = native.CacheReadInputTokens
		u.PromptTokensDetails.CachedCreationTokens = native.GetCacheCreationTotalTokens()
		u.ClaudeCacheCreation5mTokens = native.GetCacheCreation5mTokens()
		u.ClaudeCacheCreation1hTokens = native.GetCacheCreation1hTokens()
		// Older Messages responses report only the aggregate cache write count.
		u.ClaudeCacheCreation5mTokens = max(u.ClaudeCacheCreation5mTokens, u.PromptTokensDetails.CachedCreationTokens-u.ClaudeCacheCreation1hTokens)
		_ = common.Unmarshal(fields["cost"], &u.Cost)
		if native.ServerToolUse != nil {
			c.Set("claude_web_search_requests", native.ServerToolUse.WebSearchRequests)
		}
		return u
	}
	usage := parseMediaUsage(raw)
	if usage != nil {
		usage.UsageSource = "upstream"
		if info.RelayMode == relayconstant.RelayModeRerank {
			usage.PromptTokens = usage.TotalTokens
			usage.CompletionTokens = 0
		}
		return usage
	}
	if info.RelayMode == relayconstant.RelayModeAudioTranscription {
		if value, ok := fields["seconds"]; ok {
			var seconds float64
			if common.Unmarshal(value, &seconds) == nil && seconds >= 0 && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
				tokens := int(math.Round(math.Ceil(seconds) / 60 * 1000))
				usage = &dto.Usage{PromptTokens: tokens, TotalTokens: tokens, UsageSource: "duration"}
				usage.PromptTokensDetails.AudioTokens = tokens
				_ = common.Unmarshal(fields["cost"], &usage.Cost)
				return usage
			}
		}
	}
	return nil
}

func openRouterEstimate(c *gin.Context, info *relaycommon.RelayInfo, output string) *dto.Usage {
	u := service.ResponseText2Usage(c, output, info.UpstreamModelName, info.GetEstimatePromptTokens())
	u.UsageSource = "local_estimate"
	return u
}

func isOpenRouterImage(info *relaycommon.RelayInfo) bool {
	return info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits
}

func openRouterResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewError(fmt.Errorf("empty OpenRouter response"), types.ErrorCodeBadResponse)
	}
	info.IsStream = strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")
	if generationID := resp.Header.Get("X-Generation-Id"); generationID != "" {
		c.Header("X-Generation-Id", generationID)
	}
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		return openRouterSpeechResponse(c, resp, info)
	}
	// Explicit protocol conversions retain the existing conversion handlers.
	if info.RelayFormat == types.RelayFormatGemini || (info.RelayFormat == types.RelayFormatClaude && info.GetFinalRequestRelayFormat() != types.RelayFormatClaude) {
		if info.IsStream {
			c.Set("openrouter_response_committed", true)
			usage, apiErr := OaiStreamHandler(c, info, resp)
			if apiErr == nil && info.StreamStatus != nil && (info.StreamStatus.HasErrors() || !info.StreamStatus.IsNormalEnd()) {
				return nil, types.NewErrorWithStatusCode(fmt.Errorf("OpenRouter converted stream failed: %s", info.StreamStatus.Summary()), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
			}
			return usage, apiErr
		}
		return OpenaiHandler(c, info, resp)
	}
	if !info.IsStream && info.RelayFormat == types.RelayFormatOpenAI && info.ChannelSetting.ForceFormat {
		return OpenaiHandler(c, info, resp)
	}
	if info.IsStream {
		return openRouterStreamResponse(c, resp, info)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	var envelope struct {
		Usage  json.RawMessage   `json:"usage"`
		Error  json.RawMessage   `json:"error"`
		Text   string            `json:"text"`
		Status string            `json:"status"`
		Data   []json.RawMessage `json:"data"`
	}
	if err = common.Unmarshal(body, &envelope); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if apiErr := openRouterResponseError(envelope.Error); apiErr != nil {
		return nil, apiErr
	}
	if envelope.Status == "failed" {
		return nil, types.NewOpenAIError(fmt.Errorf("OpenRouter response failed"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	usage := openRouterUsage(c, info, envelope.Usage)
	if usage == nil {
		output := envelope.Text
		if info.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
			var message struct {
				Content []struct {
					Text     string          `json:"text"`
					Thinking string          `json:"thinking"`
					Input    json.RawMessage `json:"input"`
				} `json:"content"`
			}
			if common.Unmarshal(body, &message) == nil {
				for _, part := range message.Content {
					output += part.Text + part.Thinking
					if hasOpenRouterValue(part.Input) {
						output += string(part.Input)
					}
				}
			}
		}
		var chat dto.OpenAITextResponse
		if common.Unmarshal(body, &chat) == nil {
			for _, choice := range chat.Choices {
				output += choice.Message.StringContent() + choice.Message.GetReasoningContent() + choice.Message.Reasoning
				for _, call := range choice.Message.ParseToolCalls() {
					output += call.Function.Name + call.Function.Arguments
				}
			}
		}
		var response dto.OpenAIResponsesResponse
		if info.RelayMode == relayconstant.RelayModeResponses && common.Unmarshal(body, &response) == nil {
			for _, item := range response.Output {
				output += item.Arguments
				for _, content := range item.Content {
					output += content.Text
				}
			}
		}
		usage = openRouterEstimate(c, info, output)
	}
	if info.RelayMode == relayconstant.RelayModeResponses {
		var response dto.OpenAIResponsesResponse
		if common.Unmarshal(body, &response) == nil {
			record := openRouterToolRecorder(c, info)
			for _, item := range response.Output {
				record(item)
			}
		}
	}
	if isOpenRouterImage(info) {
		for _, raw := range envelope.Data {
			var item struct {
				B64 string `json:"b64_json"`
			}
			if common.Unmarshal(raw, &item) != nil || item.B64 == "" {
				return nil, types.NewOpenAIError(fmt.Errorf("OpenRouter returned an image without data"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		if len(envelope.Data) == 0 {
			return nil, types.NewOpenAIError(fmt.Errorf("OpenRouter returned no images"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
		}
		if info.PriceData.UsePrice {
			info.PriceData.AddOtherRatio("n", float64(len(envelope.Data)))
		}
		c.Set("openrouter_completed_images", len(envelope.Data))
		c.Set("openrouter_media_usage_reported", usage.UsageSource == "upstream")
		completeMediaTokenDetails(usage, info.RelayMode)
	}
	if info.RelayMode == relayconstant.RelayModeRerank && !info.ReturnDocuments {
		var object map[string]json.RawMessage
		if common.Unmarshal(body, &object) != nil {
			return nil, types.NewError(fmt.Errorf("invalid rerank response"), types.ErrorCodeBadResponseBody)
		}
		var results []map[string]json.RawMessage
		if err = common.Unmarshal(object["results"], &results); err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		for _, result := range results {
			delete(result, "document")
		}
		object["results"], _ = common.Marshal(results)
		body, err = common.Marshal(object)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}

func openRouterToolRecorder(c *gin.Context, info *relaycommon.RelayInfo) func(dto.ResponsesOutput) {
	record := newResponsesToolRecorder(info)
	return func(item dto.ResponsesOutput) {
		record(item)
		if item.Type == dto.ResponsesOutputTypeImageGenerationCall && (item.Status == "" || item.Status == "completed") {
			c.Set("image_generation_call", true)
			c.Set("image_generation_call_quality", item.Quality)
			c.Set("image_generation_call_size", item.Size)
		}
	}
}

func openRouterStreamResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var usage *dto.Usage
	var output strings.Builder
	var images int
	var nativeUsage = map[string]json.RawMessage{}
	record := openRouterToolRecorder(c, info)
	c.Set("openrouter_response_committed", true)
	var emit func(string, []byte) error
	if info.RelayFormat == types.RelayFormatOpenAI && (info.ChannelSetting.ForceFormat || info.ChannelSetting.ThinkingToContent) {
		emit = func(frame string, data []byte) error {
			if string(data) == "[DONE]" {
				_, err := c.Writer.Write([]byte(frame))
				return err
			}
			return sendStreamData(c, info, string(data), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
		}
	}
	relayMediaSSE(c, resp, info, func(event string, data []byte) (bool, error) {
		if string(data) == "[DONE]" {
			if isOpenRouterImage(info) && images == 0 {
				return true, fmt.Errorf("image stream ended without a completed image")
			}
			return true, nil
		}
		var chunk struct {
			Type    string          `json:"type"`
			Error   json.RawMessage `json:"error"`
			Usage   json.RawMessage `json:"usage"`
			Message struct {
				Usage json.RawMessage `json:"usage"`
			} `json:"message"`
			Response json.RawMessage     `json:"response"`
			Item     dto.ResponsesOutput `json:"item"`
			Delta    json.RawMessage     `json:"delta"`
			B64      string              `json:"b64_json"`
		}
		if err := common.Unmarshal(data, &chunk); err != nil {
			return false, fmt.Errorf("invalid OpenRouter SSE event: %w", err)
		}
		if chunk.Type != "" {
			event = chunk.Type
		}
		if apiErr := openRouterResponseError(chunk.Error); apiErr != nil {
			return true, apiErr
		}
		if event == "error" || event == "response.error" || event == "response.failed" {
			return true, fmt.Errorf("OpenRouter stream failed (%s)", event)
		}
		rawUsage := chunk.Usage
		if info.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
			if event == "message_start" {
				rawUsage = chunk.Message.Usage
			}
			if hasOpenRouterValue(rawUsage) {
				var update map[string]json.RawMessage
				if err := common.Unmarshal(rawUsage, &update); err != nil {
					return false, err
				}
				for key, value := range update {
					nativeUsage[key] = value
				}
				rawUsage, _ = common.Marshal(nativeUsage)
			}
		}
		if hasOpenRouterValue(chunk.Response) {
			var response struct {
				Usage  json.RawMessage       `json:"usage"`
				Error  json.RawMessage       `json:"error"`
				Output []dto.ResponsesOutput `json:"output"`
			}
			if err := common.Unmarshal(chunk.Response, &response); err != nil {
				return false, err
			}
			if apiErr := openRouterResponseError(response.Error); apiErr != nil {
				return true, apiErr
			}
			rawUsage = response.Usage
			for _, item := range response.Output {
				record(item)
			}
		}
		if event == "response.output_item.done" {
			record(chunk.Item)
		}
		if parsed := openRouterUsage(c, info, rawUsage); parsed != nil {
			usage = parsed
		}
		if event == "image_generation.completed" {
			if chunk.B64 == "" {
				return false, fmt.Errorf("completed image has no data")
			}
			images++
		}
		var chat dto.ChatCompletionsStreamResponse
		if common.Unmarshal(data, &chat) == nil {
			for _, choice := range chat.Choices {
				output.WriteString(choice.Delta.GetContentString())
				output.WriteString(choice.Delta.GetReasoningContent())
				for _, call := range choice.Delta.ToolCalls {
					output.WriteString(call.Function.Name + call.Function.Arguments)
				}
			}
		}
		if strings.HasSuffix(event, "text.delta") || event == "response.function_call_arguments.delta" || event == "response.custom_tool_call_input.delta" {
			var text string
			if common.Unmarshal(chunk.Delta, &text) == nil {
				output.WriteString(text)
			}
		} else if event == "content_block_delta" {
			var delta struct {
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			}
			if common.Unmarshal(chunk.Delta, &delta) == nil {
				output.WriteString(delta.Text + delta.Thinking + delta.PartialJSON)
			}
		}
		return event == "message_stop" || event == "response.completed" || event == "response.incomplete", nil
	}, emit)
	reportedUsage := usage != nil
	if usage == nil {
		usage = openRouterEstimate(c, info, output.String())
	}
	if info.StreamStatus.HasErrors() || !info.StreamStatus.IsNormalEnd() {
		// Text already delivered remains billable; failed images are all-or-nothing.
		// Settling here makes the controller's subsequent RefundBilling a no-op.
		if !isOpenRouterImage(info) && info.Billing != nil && (output.Len() > 0 || (reportedUsage && usage.TotalTokens > 0)) {
			service.PostTextConsumeQuota(c, info, usage, nil)
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("OpenRouter stream failed: %s", info.StreamStatus.Summary()), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if isOpenRouterImage(info) {
		if info.PriceData.UsePrice {
			info.PriceData.AddOtherRatio("n", float64(images))
		}
		c.Set("openrouter_completed_images", images)
		c.Set("openrouter_media_usage_reported", usage.UsageSource == "upstream")
		completeMediaTokenDetails(usage, info.RelayMode)
	}
	return usage, nil
}

func openRouterSpeechResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !strings.HasPrefix(mediaType, "audio/") && mediaType != "application/octet-stream" {
		data, _ := io.ReadAll(resp.Body)
		var envelope struct {
			Error json.RawMessage `json:"error"`
		}
		if common.Unmarshal(data, &envelope) == nil {
			if apiErr := openRouterResponseError(envelope.Error); apiErr != nil {
				return nil, apiErr
			}
		}
		return nil, types.NewOpenAIError(fmt.Errorf("OpenRouter speech response is not binary audio"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	info.IsStream = false
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	if len(data) == 0 {
		return nil, types.NewOpenAIError(fmt.Errorf("OpenRouter returned empty audio"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
	}
	u := &dto.Usage{PromptTokens: info.GetEstimatePromptTokens(), UsageSource: "local_estimate"}
	u.PromptTokensDetails.TextTokens = u.PromptTokens
	format := "pcm"
	if req, ok := info.Request.(*dto.AudioRequest); ok && req.ResponseFormat != "" {
		format = req.ResponseFormat
	}
	if mediaType == "audio/mpeg" {
		format = "mp3"
	} else if mediaType == "audio/pcm" || mediaType == "audio/l16" {
		format = "pcm"
	} else if extension, ok := map[string]string{"audio/wav": "wav", "audio/x-wav": "wav", "audio/flac": "flac", "audio/ogg": "ogg", "audio/aac": "aac", "audio/mp4": "m4a"}[mediaType]; ok {
		format = extension
	}
	var duration float64
	if format == "pcm" {
		duration = float64(len(data)) / 48000
	} else {
		duration, err = common.GetAudioDuration(c.Request.Context(), bytes.NewReader(data), "."+format)
	}
	if err == nil {
		u.CompletionTokens = int(math.Round(math.Ceil(duration) / 60 * 1000))
	} else {
		u.CompletionTokens = int(math.Ceil(float64(len(data)) / 1000))
	}
	u.CompletionTokenDetails.AudioTokens = u.CompletionTokens
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
	service.IOCopyBytesGracefully(c, resp, data)
	return u, nil
}
