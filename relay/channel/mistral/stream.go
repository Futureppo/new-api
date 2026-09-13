package mistral

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Mistral puts usage on the terminal choices chunk, sometimes alongside the
// entire tool call. Send those choices immediately and emit usage separately.
func streamResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var last dto.ChatCompletionsStreamResponse
	var usage *dto.Usage
	var failure *types.NewAPIError
	var text strings.Builder
	finished := make(map[int]bool)
	toolIDs := make(map[string]bool)
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		stop := func(apiErr *types.NewAPIError) { failure = apiErr; sr.Stop(apiErr) }
		if apiErr := errorFromBody([]byte(data), http.StatusBadGateway); apiErr != nil {
			stop(apiErr)
			return
		}
		normalized, err := normalizeMistralResponseData([]byte(data))
		if err != nil {
			stop(badResponse(err))
			return
		}
		var chunk dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(normalized, &chunk); err != nil {
			stop(badResponse(err))
			return
		}
		if chunk.Id != "" {
			last.Id = chunk.Id
		}
		if chunk.Model != "" {
			last.Model = chunk.Model
		}
		if chunk.Created != 0 {
			last.Created = chunk.Created
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				finished[choice.Index] = true
			} else if _, ok := finished[choice.Index]; !ok {
				finished[choice.Index] = false
			}
			text.WriteString(choice.Delta.GetContentString())
			text.WriteString(choice.Delta.GetReasoningContent())
			for _, call := range choice.Delta.ToolCalls {
				if call.ID != "" {
					toolIDs[call.ID] = true
				}
				text.WriteString(call.Function.Name)
				text.WriteString(call.Function.Arguments)
			}
		}
		if len(chunk.Choices) == 0 {
			return
		}
		var fields map[string]any
		if err := common.Unmarshal(normalized, &fields); err != nil {
			stop(badResponse(err))
			return
		}
		delete(fields, "usage")
		delete(fields, "p")
		normalized, err = common.Marshal(fields)
		if err != nil {
			stop(badResponse(err))
			return
		}
		if err := openai.HandleStreamFormat(c, info, string(normalized), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent); err != nil {
			stop(badResponse(err))
		}
	})
	if usage == nil {
		usage = service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		usage.CompletionTokens += len(toolIDs) * 7
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	normalizeUsage(usage)
	if c.Request.Context().Err() != nil {
		return usage, nil
	}
	if failure == nil {
		completed := len(finished) > 0
		if request, ok := info.Request.(*dto.GeneralOpenAIRequest); ok && request.N != nil && *request.N > 0 {
			completed = completed && len(finished) == *request.N
		}
		for _, done := range finished {
			completed = completed && done
		}
		if !completed || !info.StreamStatus.IsNormalEnd() || info.StreamStatus.HasErrors() {
			failure = badResponse(errors.New("Mistral stream ended before completion: " + info.StreamStatus.Summary()))
			info.StreamStatus.RecordError(failure.Error())
		}
	}
	if failure != nil {
		if !c.Writer.Written() {
			return nil, failure
		}
		// A partial stream cannot be retried or followed by a JSON HTTP error. Emit
		// one SSE error and settle any measured usage; do not emit a success [DONE].
		_ = helper.ObjectData(c, map[string]any{"error": failure.ToOpenAIError()})
		return usage, nil
	}
	if info.ShouldIncludeUsage {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(last.Id, last.Created, last.Model, *usage)); err != nil {
			info.StreamStatus.RecordError(err.Error())
			return usage, nil
		}
	}
	helper.Done(c)
	return usage, nil
}
