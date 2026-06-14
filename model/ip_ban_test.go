package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNormalizeIPBanTarget(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "ipv4", input: " 203.0.113.10 ", expected: "203.0.113.10"},
		{name: "ipv4 cidr", input: "203.0.113.123/24", expected: "203.0.113.0/24"},
		{name: "ipv6", input: "2001:db8::1", expected: "2001:db8::1"},
		{name: "ipv6 cidr", input: "2001:db8::abcd/64", expected: "2001:db8::/64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := NormalizeIPBanTarget(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func TestMatchIPBanSupportsIPv4IPv6AndCIDR(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	require.NoError(t, CreateIPBan(&IPBan{Target: "203.0.113.10", Reason: "single ipv4"}))
	require.NoError(t, CreateIPBan(&IPBan{Target: "198.51.100.0/24", Reason: "cidr ipv4"}))
	require.NoError(t, CreateIPBan(&IPBan{Target: "2001:db8::1", Reason: "single ipv6"}))
	require.NoError(t, CreateIPBan(&IPBan{Target: "2001:db8:abcd::/48", Reason: "cidr ipv6"}))
	require.NoError(t, CreateIPBan(&IPBan{Target: "192.0.2.55", Reason: "expired", ExpiresAt: now - 1}))
	InitIPBanCache()

	ban, ok := MatchIPBan("203.0.113.10")
	require.True(t, ok)
	require.Equal(t, "single ipv4", ban.Reason)

	ban, ok = MatchIPBan("198.51.100.88")
	require.True(t, ok)
	require.Equal(t, "cidr ipv4", ban.Reason)

	ban, ok = MatchIPBan("2001:db8::1")
	require.True(t, ok)
	require.Equal(t, "single ipv6", ban.Reason)

	ban, ok = MatchIPBan("2001:db8:abcd::123")
	require.True(t, ok)
	require.Equal(t, "cidr ipv6", ban.Reason)

	_, ok = MatchIPBan("192.0.2.55")
	require.False(t, ok)

	_, ok = MatchIPBan("192.0.2.56")
	require.False(t, ok)
}

func TestIsIPBanTargetMatchClient(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		clientIP string
		expected bool
	}{
		{name: "same ipv4", target: "203.0.113.10", clientIP: "203.0.113.10", expected: true},
		{name: "different ipv4", target: "203.0.113.10", clientIP: "203.0.113.11", expected: false},
		{name: "ipv4 cidr", target: "203.0.113.0/24", clientIP: "203.0.113.88", expected: true},
		{name: "same ipv6", target: "2001:db8::1", clientIP: "2001:db8::1", expected: true},
		{name: "ipv6 cidr", target: "2001:db8::/64", clientIP: "2001:db8::abcd", expected: true},
		{name: "ipv6 cidr first hextet only", target: "2602::/16", clientIP: "2602:feda:f39f:c871:1b89:b115:c8a6:e8a1", expected: true},
		{name: "ipv6 cidr first two hextets", target: "2602:feda::/32", clientIP: "2602:feda:f396:d312:84ad:4993:10ae:95aa", expected: true},
		{name: "ipv6 cidr exact routed /64", target: "2602:feda:f39f:c871::/64", clientIP: "2602:feda:f39f:c871:1b89:b115:c8a6:e8a1", expected: true},
		{name: "ipv6 /64 does not wildcard omitted hextets", target: "2602::/64", clientIP: "2602:feda:f39f:c871:1b89:b115:c8a6:e8a1", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := IsIPBanTargetMatchClient(tt.target, tt.clientIP)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}
