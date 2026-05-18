package controller

import (
	"archive/zip"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type conversationLogFilterPayload struct {
	StartTimestamp int64  `json:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp"`
	UserId         int    `json:"user_id"`
	Username       string `json:"username"`
	TokenName      string `json:"token_name"`
	ModelName      string `json:"model_name"`
	ChannelId      int    `json:"channel_id"`
	Channel        int    `json:"channel"`
	Group          string `json:"group"`
	RequestId      string `json:"request_id"`
	Exported       string `json:"exported"`
}

func GetConversationLogSummary(c *gin.Context) {
	summary, err := model.GetConversationLogSummary()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"summary": summary,
		"settings": gin.H{
			"retention_days": common.ConversationLogRetentionDays,
			"max_storage_gb": common.ConversationLogMaxStorageGB,
			"max_storage_bytes": int64(common.ConversationLogMaxStorageGB) *
				1024 * 1024 * 1024,
		},
	})
}

func GetConversationLogExportSummary(c *gin.Context) {
	exportSummary, err := model.GetConversationLogExportSummary(parseConversationLogQuery(c))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, exportSummary)
}

func GetConversationLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := parseConversationLogQuery(c)
	logs, total, err := model.GetConversationLogs(query, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetConversationLog(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if id <= 0 {
		common.ApiErrorMsg(c, "invalid id")
		return
	}
	log, err := model.GetConversationLogByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, log)
}

func ExportConversationLogs(c *gin.Context) {
	exportConversationLogs(c, parseConversationLogQuery(c), false)
}

func ExportAndDeleteConversationLogs(c *gin.Context) {
	var payload conversationLogFilterPayload
	if c.Request.Body != nil {
		_ = common.DecodeJson(c.Request.Body, &payload)
	}
	exportConversationLogs(c, queryFromConversationLogPayload(payload), true)
}

func DeleteConversationLogs(c *gin.Context) {
	deleted, err := model.DeleteConversationLogsByQuery(c.Request.Context(), parseConversationLogQuery(c), 200)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}

func UpdateConversationLogSettings(c *gin.Context) {
	var req struct {
		RetentionDays *int `json:"retention_days"`
		MaxStorageGB  *int `json:"max_storage_gb"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.RetentionDays != nil {
		if *req.RetentionDays < 0 {
			common.ApiErrorMsg(c, "retention_days must be >= 0")
			return
		}
		if err := model.UpdateOption("ConversationLogRetentionDays", strconv.Itoa(*req.RetentionDays)); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	if req.MaxStorageGB != nil {
		if *req.MaxStorageGB < 0 {
			common.ApiErrorMsg(c, "max_storage_gb must be >= 0")
			return
		}
		if err := model.UpdateOption("ConversationLogMaxStorageGB", strconv.Itoa(*req.MaxStorageGB)); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, nil)
}

func parseConversationLogQuery(c *gin.Context) model.ConversationLogQuery {
	startTimestamp, _ := strconv.ParseInt(firstQuery(c, "start_timestamp", "start_time"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(firstQuery(c, "end_timestamp", "end_time"), 10, 64)
	userId, _ := strconv.Atoi(c.Query("user_id"))
	channelId, _ := strconv.Atoi(firstQuery(c, "channel_id", "channel"))
	exported := parseExportedFilter(c.Query("exported"))
	return model.ConversationLogQuery{
		StartTime: startTimestamp,
		EndTime:   endTimestamp,
		UserId:    userId,
		Username:  c.Query("username"),
		TokenName: c.Query("token_name"),
		ModelName: c.Query("model_name"),
		ChannelId: channelId,
		Group:     c.Query("group"),
		RequestId: c.Query("request_id"),
		Exported:  exported,
	}
}

func queryFromConversationLogPayload(payload conversationLogFilterPayload) model.ConversationLogQuery {
	channelId := payload.ChannelId
	if channelId == 0 {
		channelId = payload.Channel
	}
	return model.ConversationLogQuery{
		StartTime: payload.StartTimestamp,
		EndTime:   payload.EndTimestamp,
		UserId:    payload.UserId,
		Username:  payload.Username,
		TokenName: payload.TokenName,
		ModelName: payload.ModelName,
		ChannelId: channelId,
		Group:     payload.Group,
		RequestId: payload.RequestId,
		Exported:  parseExportedFilter(payload.Exported),
	}
}

func firstQuery(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := c.Query(key); value != "" {
			return value
		}
	}
	return ""
}

func parseExportedFilter(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return common.GetPointer(true)
	case "false", "0", "no":
		return common.GetPointer(false)
	default:
		return nil
	}
}

func exportConversationLogs(c *gin.Context, query model.ConversationLogQuery, deleteAfterExport bool) {
	exportSummary, err := model.GetConversationLogExportSummary(query)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	batchID := uuid.NewString()
	fileName := fmt.Sprintf("conversation-logs-%s.zip", batchID)
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.Header("X-Conversation-Log-Batch-Id", batchID)
	c.Header("X-Conversation-Log-Record-Count", strconv.FormatInt(exportSummary.RecordCount, 10))
	c.Header("X-Conversation-Log-Storage-Bytes", strconv.FormatInt(exportSummary.StorageBytes, 10))
	c.Header("Access-Control-Expose-Headers", "X-Conversation-Log-Batch-Id, X-Conversation-Log-Record-Count, X-Conversation-Log-Storage-Bytes")
	c.Status(http.StatusOK)

	zipWriter := zip.NewWriter(c.Writer)
	exportedIDs := make([]int, 0)
	writeErr := writeConversationLogZip(c, zipWriter, query, exportSummary, batchID, &exportedIDs)
	closeErr := zipWriter.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		common.SysError("failed to export conversation logs: " + writeErr.Error())
		return
	}
	exportedAt := common.GetTimestamp()
	for _, ids := range chunkInts(exportedIDs, 200) {
		if err := model.MarkConversationLogsExported(ids, batchID, exportedAt); err != nil {
			common.SysError("failed to mark conversation logs exported: " + err.Error())
			return
		}
	}
	if deleteAfterExport {
		for _, ids := range chunkInts(exportedIDs, 200) {
			if _, err := model.DeleteConversationLogsByIDs(ids); err != nil {
				common.SysError("failed to delete exported conversation logs: " + err.Error())
				return
			}
		}
	}
}

func writeConversationLogZip(c *gin.Context, zipWriter *zip.Writer, query model.ConversationLogQuery, exportSummary model.ConversationLogExportSummary, batchID string, exportedIDs *[]int) error {
	metadata := gin.H{
		"batch_id":      batchID,
		"generated_at":  common.GetTimestamp(),
		"filters":       query,
		"record_count":  exportSummary.RecordCount,
		"storage_bytes": exportSummary.StorageBytes,
		"time_range": gin.H{
			"earliest_created_at": exportSummary.EarliestCreatedAt,
			"latest_created_at":   exportSummary.LatestCreatedAt,
		},
	}
	if err := writeZipJSON(zipWriter, "metadata.json", metadata); err != nil {
		return err
	}

	distillWriter, err := zipWriter.Create("distill.jsonl")
	if err != nil {
		return err
	}
	err = model.ForEachConversationLog(c.Request.Context(), query, 100, func(logs []*model.ConversationLog) error {
		for _, item := range logs {
			*exportedIDs = append(*exportedIDs, item.Id)
			data, err := common.Marshal(buildDistillEntry(item))
			if err != nil {
				return err
			}
			if _, err := distillWriter.Write(data); err != nil {
				return err
			}
			if _, err := distillWriter.Write([]byte("\n")); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, ids := range chunkInts(*exportedIDs, 100) {
		logs, err := model.GetConversationLogsByIDs(ids)
		if err != nil {
			return err
		}
		for _, item := range logs {
			if err := writeZipJSON(zipWriter, fmt.Sprintf("raw/%d.json", item.Id), buildRawEntry(item)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeZipJSON(zipWriter *zip.Writer, name string, value interface{}) error {
	writer, err := zipWriter.Create(name)
	if err != nil {
		return err
	}
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(data)
	return err
}

func buildDistillEntry(item *model.ConversationLog) gin.H {
	var clientRequest interface{}
	if item.ClientRequestBody != "" {
		if err := common.Unmarshal([]byte(item.ClientRequestBody), &clientRequest); err != nil {
			clientRequest = item.ClientRequestBody
		}
	}
	var toolCalls interface{} = []interface{}{}
	if item.DerivedToolCalls != "" {
		_ = common.Unmarshal([]byte(item.DerivedToolCalls), &toolCalls)
	}
	return gin.H{
		"id":             item.Id,
		"request_id":     item.RequestId,
		"created_at":     item.CreatedAt,
		"user_id":        item.UserId,
		"group":          item.Group,
		"model":          item.ModelName,
		"upstream_model": item.UpstreamModelName,
		"is_stream":      item.IsStream,
		"client_request": clientRequest,
		"assistant":      item.DerivedAssistantText,
		"tool_calls":     toolCalls,
		"raw_file":       fmt.Sprintf("raw/%d.json", item.Id),
	}
}

func buildRawEntry(item *model.ConversationLog) gin.H {
	return gin.H{
		"id":                     item.Id,
		"created_at":             item.CreatedAt,
		"request_id":             item.RequestId,
		"user_id":                item.UserId,
		"username":               item.Username,
		"token_id":               item.TokenId,
		"token_name":             item.TokenName,
		"channel_id":             item.ChannelId,
		"group":                  item.Group,
		"model_name":             item.ModelName,
		"upstream_model_name":    item.UpstreamModelName,
		"relay_format":           item.RelayFormat,
		"final_request_format":   item.FinalRequestFormat,
		"request_path":           item.RequestPath,
		"is_stream":              item.IsStream,
		"status_code":            item.StatusCode,
		"storage_bytes":          item.StorageBytes,
		"exported_at":            item.ExportedAt,
		"export_batch_id":        item.ExportBatchId,
		"client_request_body":    item.ClientRequestBody,
		"upstream_request_body":  item.UpstreamRequestBody,
		"upstream_response_body": item.UpstreamResponseBody,
		"client_response_body":   item.ClientResponseBody,
		"derived_assistant_text": item.DerivedAssistantText,
		"derived_tool_calls":     item.DerivedToolCalls,
		"metadata":               item.Metadata,
	}
}

func chunkInts(ids []int, batchSize int) [][]int {
	if batchSize <= 0 {
		batchSize = 100
	}
	chunks := make([][]int, 0, (len(ids)+batchSize-1)/batchSize)
	for start := 0; start < len(ids); start += batchSize {
		end := start + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}
