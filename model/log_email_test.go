package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestRecordEmailLogRecordsContextMetadata(t *testing.T) {
	truncateTables(t)

	c := newLogTestContext("  MailProbe/1.0  ")
	c.Request.RemoteAddr = "198.51.100.10:1234"
	c.Set(common.RequestIdKey, "req-email-1")

	RecordEmailLog(c, 0, RecordEmailLogParams{
		Receiver: "test@example.com",
		Subject:  "验证码",
		Source:   "email_verification",
		Success:  false,
		Error:    "smtp rejected",
	})

	var log Log
	require.NoError(t, LOG_DB.Where("type = ?", LogTypeEmail).First(&log).Error)
	require.Equal(t, 0, log.UserId)
	require.Equal(t, "alice", log.Username)
	require.Equal(t, "198.51.100.10", log.Ip)
	require.Equal(t, "MailProbe/1.0", log.UserAgent)
	require.Equal(t, "req-email-1", log.RequestId)
	require.Contains(t, log.Content, "test@example.com")
	require.Contains(t, log.Content, "验证码")
	require.Contains(t, log.Content, "失败")

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, "test@example.com", other["receiver"])
	require.Equal(t, "验证码", other["subject"])
	require.Equal(t, "email_verification", other["source"])
	require.Equal(t, false, other["success"])
	require.Equal(t, "smtp rejected", other["error"])
}

func TestRecordEmailLogWithoutContextHasNoRequestMetadata(t *testing.T) {
	truncateTables(t)

	RecordEmailLog(nil, 0, RecordEmailLogParams{
		Receiver: "notify@example.com",
		Subject:  "通知",
		Source:   "notify:quota_exceed",
		Success:  true,
	})

	var log Log
	require.NoError(t, LOG_DB.Where("type = ?", LogTypeEmail).First(&log).Error)
	require.Empty(t, log.Ip)
	require.Empty(t, log.UserAgent)
	require.Empty(t, log.RequestId)
	require.Contains(t, log.Content, "成功")

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, true, other["success"])
	require.NotContains(t, log.Other, "error")
}
