package model

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newLogTestContext(userAgent string) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("User-Agent", userAgent)
	c.Set("username", "alice")
	return c
}

func TestRecordConsumeLogRecordsUserAgent(t *testing.T) {
	truncateTables(t)

	c := newLogTestContext("  Codex CLI/1.0  ")
	RecordConsumeLog(c, 1, RecordConsumeLogParams{
		ModelName: "gpt-test",
		TokenName: "test-token",
		Quota:     1,
	})

	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", 1, LogTypeConsume).First(&log).Error)
	require.Equal(t, "Codex CLI/1.0", log.UserAgent)
}

func TestRecordErrorLogRecordsTruncatedUserAgent(t *testing.T) {
	truncateTables(t)

	longUserAgent := strings.Repeat("A", maxUserAgentLogLength+10)
	c := newLogTestContext(longUserAgent)
	RecordErrorLog(c, 1, 2, "gpt-test", "test-token", "upstream error", 3, 4, false, "default", nil)

	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", 1, LogTypeError).First(&log).Error)
	require.Len(t, []rune(log.UserAgent), maxUserAgentLogLength)
	require.Equal(t, strings.Repeat("A", maxUserAgentLogLength), log.UserAgent)
}

func TestRecordLogWithContextRecordsIPAndTruncatedUserAgent(t *testing.T) {
	truncateTables(t)

	longUserAgent := strings.Repeat("A", maxUserAgentLogLength+10)
	c := newLogTestContext("  " + longUserAgent + "  ")
	c.Request.RemoteAddr = "203.0.113.10:1234"
	RecordLogWithContext(c, 1, LogTypeSystem, "用户签到，获得额度 $ 1.000000")

	var log Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", 1, LogTypeSystem).First(&log).Error)
	require.Equal(t, "alice", log.Username)
	require.Equal(t, "203.0.113.10", log.Ip)
	require.Len(t, []rune(log.UserAgent), maxUserAgentLogLength)
	require.Equal(t, strings.Repeat("A", maxUserAgentLogLength), log.UserAgent)
}

func TestGetAllLogsFiltersByUserAgent(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{
			UserId:    1,
			Username:  "alice",
			CreatedAt: now,
			Type:      LogTypeConsume,
			UserAgent: "Codex CLI/1.0",
		},
		{
			UserId:    2,
			Username:  "bob",
			CreatedAt: now,
			Type:      LogTypeConsume,
			UserAgent: "codex-desktop/2.0",
		},
		{
			UserId:    3,
			Username:  "carol",
			CreatedAt: now,
			Type:      LogTypeConsume,
			UserAgent: "curl/8.0",
		},
	}).Error)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "", "", "CODEX")

	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Contains(t, strings.ToLower(log.UserAgent), "codex")
	}
}

func TestGetUserLogsFiltersByUserAgentAndUserID(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{
			UserId:    1,
			Username:  "alice",
			CreatedAt: now,
			Type:      LogTypeConsume,
			UserAgent: "Codex CLI/1.0",
		},
		{
			UserId:    1,
			Username:  "alice",
			CreatedAt: now,
			Type:      LogTypeConsume,
			UserAgent: "curl/8.0",
		},
		{
			UserId:    2,
			Username:  "bob",
			CreatedAt: now,
			Type:      LogTypeConsume,
			UserAgent: "Codex CLI/1.0",
		},
	}).Error)

	logs, total, err := GetUserLogs(1, LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "", "codex")

	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	require.Equal(t, 1, logs[0].UserId)
	require.Equal(t, "Codex CLI/1.0", logs[0].UserAgent)
}

func TestSumUsedQuotaFiltersByUserAgent(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{
			UserId:           1,
			Username:         "alice",
			CreatedAt:        now,
			Type:             LogTypeConsume,
			Quota:            10,
			PromptTokens:     3,
			CompletionTokens: 4,
			UserAgent:        "Codex CLI/1.0",
		},
		{
			UserId:           1,
			Username:         "alice",
			CreatedAt:        now,
			Type:             LogTypeConsume,
			Quota:            20,
			PromptTokens:     5,
			CompletionTokens: 6,
			UserAgent:        "curl/8.0",
		},
	}).Error)

	stat, err := SumUsedQuota(LogTypeConsume, now-10, now+10, "", "alice", "", 0, "", "", "codex")

	require.NoError(t, err)
	require.Equal(t, 10, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 7, stat.Tpm)
}
