package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateInviteCode(t *testing.T) {
	seen := make(map[string]struct{}, 4096)
	for i := 0; i < 4096; i++ {
		code, err := GenerateInviteCode()
		require.NoError(t, err)
		require.Len(t, code, InviteCodeLength)
		require.True(t, IsValidInviteCode(code))
		_, duplicated := seen[code]
		require.False(t, duplicated)
		seen[code] = struct{}{}
	}
}

func TestNormalizeAndValidateInviteCode(t *testing.T) {
	code := "ABCDEFGHJKLMNPQR"
	require.Equal(t, code, NormalizeInviteCode("  "+strings.ToLower(code)+"  "))
	require.True(t, IsValidInviteCode(code))
	require.False(t, IsValidInviteCode(strings.ToLower(code)))
	require.False(t, IsValidInviteCode("ABCD"))
	require.False(t, IsValidInviteCode("ABCDEFGHJKLMNPQ0"))
}
