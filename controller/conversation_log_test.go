package controller

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExportConversationLogsZipContainsDatasetMetadataAndRaw(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	log := model.ConversationLog{
		CreatedAt:            100,
		RequestId:            "req-export",
		UserId:               1,
		Username:             "root",
		ModelName:            "gpt-test",
		ClientRequestBody:    `{"messages":[{"role":"user","content":"hi"}]}`,
		ClientResponseBody:   `{"choices":[{"message":{"content":"hello"}}]}`,
		DerivedAssistantText: "hello",
		DerivedToolCalls:     "[]",
		StorageBytes:         128,
	}
	require.NoError(t, db.Create(&log).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/conversation_logs/export.zip", nil)

	ExportConversationLogs(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	reader, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	require.NoError(t, err)
	names := make(map[string]bool)
	for _, file := range reader.File {
		names[file.Name] = true
	}
	require.True(t, names["distill.jsonl"])
	require.True(t, names["metadata.json"])
	require.True(t, names["raw/1.json"])
}

func TestExportAndDeleteConversationLogsDeletesOnlyFilteredRecords(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.ConversationLog{
		{CreatedAt: 100, RequestId: "req-a", ModelName: "model-a", DerivedToolCalls: "[]", StorageBytes: 10},
		{CreatedAt: 101, RequestId: "req-b", ModelName: "model-b", DerivedToolCalls: "[]", StorageBytes: 10},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/conversation_logs/export_and_delete", strings.NewReader(`{"model_name":"model-a"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	ExportAndDeleteConversationLogs(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var remaining []model.ConversationLog
	require.NoError(t, db.Order("request_id asc").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "req-b", remaining[0].RequestId)
}
