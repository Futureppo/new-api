package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestTrimConversationLogsByStorageLimitPrefersExportedThenOldest(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	logs := []*ConversationLog{
		{CreatedAt: now - 30, RequestId: "exported", StorageBytes: 60, ExportedAt: now - 5},
		{CreatedAt: now - 20, RequestId: "oldest", StorageBytes: 60},
		{CreatedAt: now - 10, RequestId: "newest", StorageBytes: 60},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	deleted, err := TrimConversationLogsByStorageLimit(context.Background(), 80, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)

	var remaining []ConversationLog
	require.NoError(t, LOG_DB.Order("created_at asc").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	require.Equal(t, "newest", remaining[0].RequestId)
}

func TestConversationLogSummaryStorageBytes(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]ConversationLog{
		{CreatedAt: now - 1, RequestId: "a", StorageBytes: 11},
		{CreatedAt: now, RequestId: "b", StorageBytes: 22, ExportedAt: now},
	}).Error)

	summary, err := GetConversationLogSummary()
	require.NoError(t, err)
	require.Equal(t, int64(33), summary.StorageBytes)
	require.Equal(t, int64(2), summary.RecordCount)
	require.Equal(t, int64(1), summary.ExportedCount)
	require.Equal(t, now-1, summary.EarliestCreatedAt)
	require.Equal(t, now, summary.LatestCreatedAt)
}
