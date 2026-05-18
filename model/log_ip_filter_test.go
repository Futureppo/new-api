package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetAllLogsFiltersByIP(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{
			UserId:    1,
			Username:  "alice",
			CreatedAt: now,
			Type:      LogTypeConsume,
			Ip:        "203.0.113.10",
		},
		{
			UserId:    2,
			Username:  "bob",
			CreatedAt: now,
			Type:      LogTypeConsume,
			Ip:        "203.0.113.10",
		},
		{
			UserId:    3,
			Username:  "carol",
			CreatedAt: now,
			Type:      LogTypeConsume,
			Ip:        "203.0.113.20",
		},
	}).Error)

	logs, total, err := GetAllLogs(LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "", "203.0.113.10")

	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, "203.0.113.10", log.Ip)
	}
}

func TestGetUserLogsFiltersByIPAndUserID(t *testing.T) {
	truncateTables(t)

	now := common.GetTimestamp()
	require.NoError(t, LOG_DB.Create(&[]Log{
		{
			UserId:    1,
			Username:  "alice",
			CreatedAt: now,
			Type:      LogTypeConsume,
			Ip:        "203.0.113.10",
		},
		{
			UserId:    1,
			Username:  "alice",
			CreatedAt: now,
			Type:      LogTypeConsume,
			Ip:        "203.0.113.20",
		},
		{
			UserId:    2,
			Username:  "bob",
			CreatedAt: now,
			Type:      LogTypeConsume,
			Ip:        "203.0.113.10",
		},
	}).Error)

	logs, total, err := GetUserLogs(1, LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "203.0.113.10")

	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, logs, 1)
	require.Equal(t, 1, logs[0].UserId)
	require.Equal(t, "203.0.113.10", logs[0].Ip)
}

func TestSumUsedQuotaFiltersByIP(t *testing.T) {
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
			Ip:               "203.0.113.10",
		},
		{
			UserId:           1,
			Username:         "alice",
			CreatedAt:        now,
			Type:             LogTypeConsume,
			Quota:            20,
			PromptTokens:     5,
			CompletionTokens: 6,
			Ip:               "203.0.113.20",
		},
	}).Error)

	stat, err := SumUsedQuota(LogTypeConsume, now-10, now+10, "", "alice", "", 0, "", "203.0.113.10")

	require.NoError(t, err)
	require.Equal(t, 10, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 7, stat.Tpm)
}
