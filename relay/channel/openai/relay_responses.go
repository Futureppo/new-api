package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && (oaiError.Type != "" || constant.IsSelfHostedChannel(info.ChannelType)) {
		if constant.IsSelfHostedChannel(info.ChannelType) && resp.StatusCode == http.StatusOK {
			return nil, types.WithOpenAIError(*oaiError, http.StatusBadGateway)
		}
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// 写入新的 response body
	service.IOCopyBytesGracefully(c, resp, responseBody)

	// compute usage
	usage := normalizeResponsesUsage(responsesResponse.Usage)
	if responsesResponse.Usage == nil && info != nil {
		var text strings.Builder
		for _, item := range responsesResponse.Output {
			for _, content := range item.Content {
				text.WriteString(content.Text)
			}
			text.WriteString(item.Arguments)
		}
		usage = *service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	record := newResponsesToolRecorder(info)
	for _, item := range responsesResponse.Output {
		record(item)
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var usage = &dto.Usage{}
	var responseTextBuilder strings.Builder
	var hasUsage, terminal bool
	record := newResponsesToolRecorder(info)

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {

		// 检查当前数据是否包含 completed 状态和 usage 信息
		var streamResponse dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Stop(err)
			return
		}
		sendResponsesStreamData(c, streamResponse, data)
		switch streamResponse.Type {
		case "response.completed", "response.incomplete", "response.failed":
			terminal = true
			if streamResponse.Response != nil {
				for _, item := range streamResponse.Response.Output {
					record(item)
				}
				if streamResponse.Response.Usage != nil {
					*usage = normalizeResponsesUsage(streamResponse.Response.Usage)
					hasUsage = true
				}
				if streamResponse.Response.HasImageGenerationCall() {
					c.Set("image_generation_call", true)
					c.Set("image_generation_call_quality", streamResponse.Response.GetQuality())
					c.Set("image_generation_call_size", streamResponse.Response.GetSize())
				}
			}
			if streamResponse.Type == "response.failed" {
				failure := fmt.Errorf("upstream response.failed")
				if streamResponse.Response != nil && streamResponse.Response.Error != nil {
					failure = fmt.Errorf("upstream response.failed: %v", streamResponse.Response.Error)
				}
				sr.Stop(failure)
			} else {
				sr.Done()
			}
		case "error", "response.error":
			terminal = true
			// The native error event was already delivered. Do not append JSON or retry.
			sr.Stop(fmt.Errorf("upstream Responses error: %s", data))
		case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta",
			"response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
			// 处理输出文本
			responseTextBuilder.WriteString(streamResponse.Delta)
		case dto.ResponsesOutputTypeItemDone:
			if streamResponse.Item != nil {
				record(*streamResponse.Item)
			}
		}
	})

	if !terminal && !info.StreamStatus.HasErrors() {
		info.StreamStatus.RecordError("Responses stream ended without a terminal event")
	}
	if !hasUsage {
		usage = service.ResponseText2Usage(c, responseTextBuilder.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	return usage, nil
}

func normalizeResponsesUsage(upstream *dto.Usage) dto.Usage {
	if upstream == nil {
		return dto.Usage{}
	}
	usage := *upstream
	usage.PromptTokens = upstream.InputTokens
	usage.CompletionTokens = upstream.OutputTokens
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if upstream.InputTokensDetails != nil {
		usage.PromptTokensDetails = *upstream.InputTokensDetails
	}
	if upstream.OutputTokensDetails != nil {
		usage.CompletionTokenDetails = *upstream.OutputTokensDetails
	}
	return usage
}

// Count executions, not declarations. The item id deduplicates output_item.done
// against the final response output snapshot.
func newResponsesToolRecorder(info *relaycommon.RelayInfo) func(dto.ResponsesOutput) {
	seen := make(map[string]bool)
	return func(item dto.ResponsesOutput) {
		if info == nil {
			return
		}
		if item.Status != "" && item.Status != "completed" {
			return
		}
		toolType := ""
		switch item.Type {
		case "web_search_call":
			toolType = dto.BuildInToolWebSearchPreview
		case "file_search_call":
			toolType = dto.BuildInToolFileSearch
		default:
			return
		}
		if item.ID != "" {
			if seen[item.ID] {
				return
			}
			seen[item.ID] = true
		}
		if info.ResponsesUsageInfo == nil {
			info.ResponsesUsageInfo = &relaycommon.ResponsesUsageInfo{}
		}
		if info.ResponsesUsageInfo.BuiltInTools == nil {
			info.ResponsesUsageInfo.BuiltInTools = make(map[string]*relaycommon.BuildInToolInfo)
		}
		tool := info.ResponsesUsageInfo.BuiltInTools[toolType]
		if tool == nil {
			tool = &relaycommon.BuildInToolInfo{ToolName: toolType, SearchContextSize: "medium"}
			info.ResponsesUsageInfo.BuiltInTools[toolType] = tool
		}
		tool.CallCount++
	}
}
