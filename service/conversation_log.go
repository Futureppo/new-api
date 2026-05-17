package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const conversationLogCleanupBatchSize = 200

func StartConversationCapture(c *gin.Context, relayInfo *relaycommon.RelayInfo) {
	if c == nil || relayInfo == nil || relayInfo.ChannelMeta == nil {
		return
	}
	if !relayInfo.ChannelOtherSettings.ConversationLogEnabled || !isConversationLogRelayFormat(relayInfo.RelayFormat) {
		return
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		logger.LogError(c, "failed to read conversation capture request body: "+err.Error())
		return
	}
	body, err := storage.Bytes()
	if err != nil {
		logger.LogError(c, "failed to snapshot conversation capture request body: "+err.Error())
		return
	}
	capture := relaycommon.NewConversationCapture()
	capture.SetClientRequestBody(body)
	relayInfo.ConversationCapture = capture
	relaycommon.SetConversationCapture(c, capture)
}

func isConversationLogRelayFormat(format types.RelayFormat) bool {
	switch format {
	case types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini,
		types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return true
	default:
		return false
	}
}

func RecordConversationLogAfterConsume(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, usage *dto.Usage, logModel string, other map[string]interface{}) {
	if ctx == nil || relayInfo == nil || relayInfo.ConversationCapture == nil {
		return
	}
	snapshot := relayInfo.ConversationCapture.Snapshot()
	if len(snapshot.ClientRequestBody) == 0 && len(snapshot.ClientResponseBody) == 0 {
		return
	}

	assistantText, toolCallsJSON := deriveConversationLogPayload(snapshot.ClientResponseBody, relayInfo.RelayFormat, relayInfo.IsStream)
	metadataBytes, _ := common.Marshal(map[string]interface{}{
		"usage":                    usage,
		"quota":                    summary.Quota,
		"prompt_tokens":            summary.PromptTokens,
		"completion_tokens":        summary.CompletionTokens,
		"total_tokens":             summary.TotalTokens,
		"use_time_seconds":         summary.UseTimeSeconds,
		"request_conversion_chain": relayInfo.RequestConversionChain,
		"final_request_format":     relayInfo.GetFinalRequestRelayFormat(),
		"other":                    other,
		"node_name":                common.NodeName,
	})

	log := &model.ConversationLog{
		CreatedAt:            common.GetTimestamp(),
		RequestId:            ctx.GetString(common.RequestIdKey),
		UserId:               relayInfo.UserId,
		Username:             ctx.GetString("username"),
		TokenId:              relayInfo.TokenId,
		TokenName:            summary.TokenName,
		ChannelId:            relayInfo.ChannelId,
		Group:                relayInfo.UsingGroup,
		ModelName:            logModel,
		UpstreamModelName:    relayInfo.UpstreamModelName,
		RelayFormat:          string(relayInfo.RelayFormat),
		FinalRequestFormat:   string(relayInfo.GetFinalRequestRelayFormat()),
		RequestPath:          relayInfo.RequestURLPath,
		IsStream:             relayInfo.IsStream,
		StatusCode:           200,
		ClientRequestBody:    string(snapshot.ClientRequestBody),
		UpstreamRequestBody:  string(snapshot.UpstreamRequestBody),
		UpstreamResponseBody: string(snapshot.UpstreamResponseBody),
		ClientResponseBody:   string(snapshot.ClientResponseBody),
		DerivedAssistantText: assistantText,
		DerivedToolCalls:     toolCallsJSON,
		Metadata:             string(metadataBytes),
	}
	log.StorageBytes = int64(len(snapshot.ClientRequestBody) +
		len(snapshot.UpstreamRequestBody) +
		len(snapshot.UpstreamResponseBody) +
		len(snapshot.ClientResponseBody) +
		len(log.DerivedAssistantText) +
		len(log.DerivedToolCalls) +
		len(log.Metadata))

	if err := model.CreateConversationLog(log); err != nil {
		logger.LogError(ctx, "failed to record conversation log: "+err.Error())
	}
}

func StartConversationLogCleanupTask() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		time.Sleep(time.Minute)
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			cleanupConversationLogs(context.Background())
			<-ticker.C
		}
	}()
}

func cleanupConversationLogs(ctx context.Context) {
	if common.ConversationLogRetentionDays > 0 {
		cutoff := common.GetTimestamp() - int64(common.ConversationLogRetentionDays)*24*3600
		if deleted, err := model.DeleteConversationLogsOlderThan(ctx, cutoff, conversationLogCleanupBatchSize); err != nil {
			common.SysError("failed to cleanup old conversation logs: " + err.Error())
		} else if deleted > 0 {
			common.SysLog(fmt.Sprintf("cleaned %d expired conversation logs", deleted))
		}
	}
	if common.ConversationLogMaxStorageGB > 0 {
		maxBytes := int64(common.ConversationLogMaxStorageGB) * 1024 * 1024 * 1024
		if deleted, err := model.TrimConversationLogsByStorageLimit(ctx, maxBytes, conversationLogCleanupBatchSize); err != nil {
			common.SysError("failed to trim conversation logs by storage limit: " + err.Error())
		} else if deleted > 0 {
			common.SysLog(fmt.Sprintf("trimmed %d conversation logs by storage limit", deleted))
		}
	}
}

func deriveConversationLogPayload(responseBody []byte, format types.RelayFormat, isStream bool) (string, string) {
	texts := make([]string, 0)
	toolCalls := make([]interface{}, 0)
	if len(responseBody) == 0 {
		return "", "[]"
	}

	if isStream {
		for _, payload := range ssePayloads(responseBody) {
			var obj map[string]interface{}
			if err := common.Unmarshal([]byte(payload), &obj); err != nil {
				continue
			}
			deriveFromObject(obj, format, true, &texts, &toolCalls)
		}
	} else {
		var obj map[string]interface{}
		if err := common.Unmarshal(responseBody, &obj); err == nil {
			deriveFromObject(obj, format, false, &texts, &toolCalls)
		}
	}

	toolCallsBytes, err := common.Marshal(toolCalls)
	if err != nil {
		return strings.Join(texts, ""), "[]"
	}
	return strings.Join(texts, ""), string(toolCallsBytes)
}

func ssePayloads(data []byte) []string {
	lines := strings.Split(string(data), "\n")
	payloads := make([]string, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		payloads = append(payloads, payload)
	}
	return payloads
}

func deriveFromObject(obj map[string]interface{}, format types.RelayFormat, stream bool, texts *[]string, toolCalls *[]interface{}) {
	switch format {
	case types.RelayFormatClaude:
		deriveClaude(obj, stream, texts, toolCalls)
	case types.RelayFormatGemini:
		deriveGemini(obj, texts, toolCalls)
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		deriveResponses(obj, stream, texts, toolCalls)
	default:
		deriveOpenAI(obj, texts, toolCalls)
	}
}

func deriveOpenAI(obj map[string]interface{}, texts *[]string, toolCalls *[]interface{}) {
	for _, choice := range getArray(obj, "choices") {
		choiceMap, ok := choice.(map[string]interface{})
		if !ok {
			continue
		}
		if delta, ok := choiceMap["delta"].(map[string]interface{}); ok {
			appendString(texts, delta["content"])
			appendString(texts, delta["reasoning_content"])
			appendToolCalls(toolCalls, delta["tool_calls"])
		}
		if message, ok := choiceMap["message"].(map[string]interface{}); ok {
			appendString(texts, message["content"])
			appendString(texts, message["reasoning_content"])
			appendToolCalls(toolCalls, message["tool_calls"])
		}
	}
}

func deriveResponses(obj map[string]interface{}, stream bool, texts *[]string, toolCalls *[]interface{}) {
	appendString(texts, obj["output_text"])
	if stream {
		if delta, ok := obj["delta"].(string); ok {
			appendString(texts, delta)
		}
		if response, ok := obj["response"].(map[string]interface{}); ok {
			deriveResponses(response, false, texts, toolCalls)
		}
	}
	for _, output := range getArray(obj, "output") {
		outputMap, ok := output.(map[string]interface{})
		if !ok {
			continue
		}
		outputType, _ := outputMap["type"].(string)
		if strings.Contains(outputType, "function_call") || strings.Contains(outputType, "tool") {
			*toolCalls = append(*toolCalls, outputMap)
		}
		for _, content := range getArray(outputMap, "content") {
			contentMap, ok := content.(map[string]interface{})
			if !ok {
				continue
			}
			appendString(texts, contentMap["text"])
		}
	}
	if item, ok := obj["item"].(map[string]interface{}); ok {
		itemType, _ := item["type"].(string)
		if strings.Contains(itemType, "function_call") || strings.Contains(itemType, "tool") {
			*toolCalls = append(*toolCalls, item)
		}
	}
}

func deriveClaude(obj map[string]interface{}, stream bool, texts *[]string, toolCalls *[]interface{}) {
	for _, content := range getArray(obj, "content") {
		contentMap, ok := content.(map[string]interface{})
		if !ok {
			continue
		}
		contentType, _ := contentMap["type"].(string)
		if contentType == "tool_use" {
			*toolCalls = append(*toolCalls, contentMap)
			continue
		}
		appendString(texts, contentMap["text"])
	}
	if stream {
		if delta, ok := obj["delta"].(map[string]interface{}); ok {
			appendString(texts, delta["text"])
			appendToolCalls(toolCalls, delta["tool_calls"])
		}
		if block, ok := obj["content_block"].(map[string]interface{}); ok {
			blockType, _ := block["type"].(string)
			if blockType == "tool_use" {
				*toolCalls = append(*toolCalls, block)
			}
			appendString(texts, block["text"])
		}
	}
}

func deriveGemini(obj map[string]interface{}, texts *[]string, toolCalls *[]interface{}) {
	for _, candidate := range getArray(obj, "candidates") {
		candidateMap, ok := candidate.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := candidateMap["content"].(map[string]interface{})
		for _, part := range getArray(content, "parts") {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			appendString(texts, partMap["text"])
			if functionCall, ok := partMap["functionCall"]; ok {
				*toolCalls = append(*toolCalls, functionCall)
			}
		}
	}
}

func getArray(obj map[string]interface{}, key string) []interface{} {
	if obj == nil {
		return nil
	}
	items, _ := obj[key].([]interface{})
	return items
}

func appendString(texts *[]string, value interface{}) {
	switch v := value.(type) {
	case string:
		if v != "" {
			*texts = append(*texts, v)
		}
	case []interface{}:
		for _, item := range v {
			itemMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			appendString(texts, itemMap["text"])
		}
	}
}

func appendToolCalls(toolCalls *[]interface{}, value interface{}) {
	if value == nil {
		return
	}
	switch v := value.(type) {
	case []interface{}:
		*toolCalls = append(*toolCalls, v...)
	default:
		*toolCalls = append(*toolCalls, v)
	}
}
