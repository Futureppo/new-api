package setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbeGuardTextRulesAndReasons(t *testing.T) {
	rules, err := ParseProbeGuardTextRules("  SAY OK\nｓａｙ　ｏｋ\n\n回复两个字：可用 ")
	require.NoError(t, err)
	require.Len(t, rules, 2)
	require.Contains(t, rules, "say ok")
	require.Contains(t, rules, "回复两个字:可用")
	rules, err = ParseProbeGuardTextRules("")
	require.NoError(t, err)
	require.Empty(t, rules)
	for _, raw := range []string{strings.Repeat("x", 1025), strings.Repeat("x\n", 257), strings.Repeat("x", ProbeGuardMaxRulesBytes+1)} {
		_, err = ParseProbeGuardTextRules(raw)
		require.Error(t, err)
	}
	for _, value := range []string{"", "{bad}", strings.Repeat("字", 256), strings.Repeat("字", 250) + "{until}"} {
		require.Error(t, CheckProbeGuardOption("first_ban_reason", value))
	}
	for _, value := range []string{DefaultProbeGuardFirstReason, DefaultProbeGuardSecondReason, DefaultProbeGuardPermanentReason, "custom {count} {minutes} {until}"} {
		require.NoError(t, CheckProbeGuardOption("first_ban_reason", value))
	}
	require.Equal(t, "1 / 10 / 1970-01-01 08:00:01", RenderProbeGuardReason("{count} / {minutes} / {until}", 1, 10, 1))
	require.Equal(t, "3 / 永久", RenderProbeGuardReason("{count} / {until}", 3, 0, 0))
	require.NoError(t, CheckProbeGuardOption("probe_ping_enabled", "false"))
	require.Error(t, CheckProbeGuardOption("first_user_ban_minutes", "0"))
	require.Error(t, CheckProbeGuardOption("second_user_ban_minutes", "43201"))
}

func TestProbeGuardBatchDefaultsAndValidation(t *testing.T) {
	// Old IP-guard values must not leak into the new account request threshold.
	cfg := ProbeGuardSetting{WindowSeconds: 120, DistinctModelCount: 5}
	cfg.Normalize()
	require.Equal(t, 30, cfg.BatchWindowSeconds)
	require.Equal(t, 3, cfg.BatchRequestCount)
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"batch_window_seconds", "1", true}, {"batch_window_seconds", "30", true}, {"batch_window_seconds", "3600", true},
		{"batch_window_seconds", "0", false}, {"batch_window_seconds", "3601", false}, {"batch_window_seconds", "1.5", false},
		{"batch_request_count", "1", true}, {"batch_request_count", "3", true}, {"batch_request_count", "1000", true},
		{"batch_request_count", "0", false}, {"batch_request_count", "1001", false}, {"batch_request_count", "3.5", false},
	} {
		err := CheckProbeGuardOption("probe_guard_setting."+tc.key, tc.value)
		require.Equal(t, tc.valid, err == nil, "%s=%s", tc.key, tc.value)
	}
}
