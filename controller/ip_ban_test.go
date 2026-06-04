package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseIPBanBatchLinesUsesDefaultAndInlineReasons(t *testing.T) {
	entries, invalid := parseIPBanBatchLines(`
203.0.113.10
203.0.113.0/24 cidr reason
2001:db8::1 ipv6 reason with spaces
bad-ip bad reason
192.0.2.1
`, "default reason")

	require.Len(t, invalid, 1)
	require.Equal(t, 5, invalid[0].LineNumber)
	require.Len(t, entries, 4)
	require.Equal(t, "203.0.113.10", entries[0].Target)
	require.Equal(t, "default reason", entries[0].Reason)
	require.Equal(t, "203.0.113.0/24", entries[1].Target)
	require.Equal(t, "cidr reason", entries[1].Reason)
	require.Equal(t, "2001:db8::1", entries[2].Target)
	require.Equal(t, "ipv6 reason with spaces", entries[2].Reason)
	require.Equal(t, "192.0.2.1", entries[3].Target)
	require.Equal(t, "default reason", entries[3].Reason)
}

func TestParseIPBanBatchLinesRejectsMissingReason(t *testing.T) {
	entries, invalid := parseIPBanBatchLines("203.0.113.10", "")

	require.Empty(t, entries)
	require.Len(t, invalid, 1)
	require.Equal(t, "封禁原因不能为空", invalid[0].Message)
}

func TestParseIPBanBatchLinesDeduplicatesNormalizedTargets(t *testing.T) {
	entries, invalid := parseIPBanBatchLines(`
203.0.113.1/24 first
203.0.113.2/24 second
`, "")

	require.Empty(t, invalid)
	require.Len(t, entries, 1)
	require.Equal(t, "203.0.113.0/24", entries[0].Target)
	require.Equal(t, "first", entries[0].Reason)
}
