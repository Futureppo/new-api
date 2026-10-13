package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func TestRecordSessionIdentitySwitchAndLogFilters(t *testing.T) {
	truncateTables(t)
	original := setting.GetEnhancementSetting().SessionRiskRecordingEnabled
	originalConsume := common.LogConsumeEnabled
	t.Cleanup(func() {
		setting.GetEnhancementSetting().SessionRiskRecordingEnabled = original
		common.LogConsumeEnabled = originalConsume
	})
	common.LogConsumeEnabled = true
	c := newLogTestContext("pi")
	c.Set(common.RequestSessionIdentityKey, common.RequestSessionIdentity{SessionID: "Case-Sensitive", SessionSource: "header:session-id", CacheKey: "cache", CacheSource: "body:prompt_cache_key"})
	setting.GetEnhancementSetting().SessionRiskRecordingEnabled = false
	RecordConsumeLog(c, 1, RecordConsumeLogParams{TokenId: 1})
	setting.GetEnhancementSetting().SessionRiskRecordingEnabled = true
	RecordConsumeLog(c, 1, RecordConsumeLogParams{TokenId: 1, Quota: 10})
	RecordErrorLog(c, 2, 1, "test", "token", "error", 2, 0, false, "default", nil)
	setting.GetEnhancementSetting().SessionRiskRecordingEnabled = false
	RecordErrorLog(c, 2, 1, "test", "token", "error", 2, 0, false, "default", nil)
	var stored []Log
	require.NoError(t, LOG_DB.Order("id asc").Find(&stored).Error)
	require.Len(t, stored, 4)
	require.False(t, stored[0].SessionRecorded)
	require.True(t, stored[1].SessionRecorded)
	require.Equal(t, "Case-Sensitive", stored[1].SessionId)
	require.Equal(t, stored[1].SessionHash, stored[2].SessionHash)
	require.Equal(t, "cache", stored[2].CacheKey)
	require.False(t, stored[3].SessionRecorded)
	require.Empty(t, stored[3].SessionHash)
	logs, total, err := GetAllLogs(0, 0, 0, "", "", "", 0, 20, 0, "", "", "", "", LogSessionFilter{SessionID: "Case-Sensitive"})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	_, total, err = GetAllLogs(0, 0, 0, "", "", "", 0, 20, 0, "", "", "", "", LogSessionFilter{SessionID: "case-sensitive"})
	require.NoError(t, err)
	require.Zero(t, total)
	logs, total, err = GetUserLogs(1, 0, 0, 0, "", "", 0, 20, "", "", "", "", LogSessionFilter{CacheKey: "cache"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, 1, logs[0].UserId)
	encoded, err := common.Marshal(stored[1])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), stored[1].SessionHash)
	stat, err := SumUsedQuota(0, 0, 0, "", "", "", 0, "", "", "", LogSessionFilter{SessionID: "Case-Sensitive"})
	require.NoError(t, err)
	require.Equal(t, 10, stat.Quota)
	require.Equal(t, 2, stat.RpmTotal)
	_, total, err = GetAllLogs(0, 0, 0, "", "", "", 0, 20, 0, "", "", "", "", LogSessionFilter{SessionID: "bad\x00value"})
	require.NoError(t, err)
	require.Zero(t, total)
}
