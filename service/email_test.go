package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSendEmailWithLogRecordsFailureAndOmitsBody(t *testing.T) {
	truncate(t)

	oldSMTPServer := common.SMTPServer
	oldSMTPPort := common.SMTPPort
	oldSMTPAccount := common.SMTPAccount
	oldSMTPFrom := common.SMTPFrom
	oldSMTPToken := common.SMTPToken
	t.Cleanup(func() {
		common.SMTPServer = oldSMTPServer
		common.SMTPPort = oldSMTPPort
		common.SMTPAccount = oldSMTPAccount
		common.SMTPFrom = oldSMTPFrom
		common.SMTPToken = oldSMTPToken
	})

	common.SMTPServer = ""
	common.SMTPAccount = ""
	common.SMTPFrom = ""
	common.SMTPToken = ""

	err := SendEmailWithLog(nil, 42, "test_source", "敏感邮件", "target@example.com", "SECRET-CODE-123456")
	require.Error(t, err)

	var log model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeEmail).First(&log).Error)
	require.Equal(t, 42, log.UserId)
	require.Empty(t, log.Ip)
	require.Empty(t, log.UserAgent)
	require.Contains(t, log.Content, "target@example.com")
	require.Contains(t, log.Content, "失败")
	require.NotContains(t, log.Content, "SECRET-CODE-123456")
	require.NotContains(t, log.Other, "SECRET-CODE-123456")

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, "target@example.com", other["receiver"])
	require.Equal(t, "敏感邮件", other["subject"])
	require.Equal(t, "test_source", other["source"])
	require.Equal(t, false, other["success"])
	require.NotEmpty(t, other["error"])
}
