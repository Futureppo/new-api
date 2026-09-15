package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestConsumeLogReasoningEffortVisibleToUser(t *testing.T) {
	truncateTables(t)
	RecordConsumeLog(newLogTestContext("test"), 1, RecordConsumeLogParams{
		ModelName: "grok-4.6", TokenName: "test", Quota: 1,
		Other: map[string]any{"reasoning_effort": "xhigh", "admin_info": map[string]any{"use_channel": []int{1}}},
	})
	var stored Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", 1, LogTypeConsume).First(&stored).Error)
	var other map[string]any
	require.NoError(t, common.UnmarshalJsonStr(stored.Other, &other))
	require.Equal(t, "xhigh", other["reasoning_effort"])
	formatUserLogs([]*Log{&stored}, 0)
	require.NoError(t, common.UnmarshalJsonStr(stored.Other, &other))
	require.Equal(t, "xhigh", other["reasoning_effort"])
	require.NotContains(t, stored.Other, "admin_info")
}
