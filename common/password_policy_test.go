package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginPasswordPolicy(t *testing.T) {
	for _, password := range []string{"Ab1!cdef", "Ab1!" + strings.Repeat("x", 68), "Ab1!" + strings.Repeat("中", 22) + "xy", " Ab1!xyz "} {
		require.NoError(t, ValidateLoginPassword(password), "valid: %q", password)
	}
	for _, password := range []string{"", "Ab1!xyz", "abcdefgh1!", "ABCDEFGH1!", "Abcdefgh!", "Abcdefgh1", "Abcdefg1中", "Abcdefg1 ", "Ab1!" + strings.Repeat("x", 69), "Ab1!" + strings.Repeat("中", 23), "Ab1!abc\xff"} {
		require.ErrorIs(t, ValidateLoginPassword(password), ErrPasswordPolicy, "invalid: %q", password)
	}
}

func TestGenerateLoginPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		password, err := GenerateLoginPassword()
		require.NoError(t, err)
		require.Len(t, password, 16)
		require.NoError(t, ValidateLoginPassword(password))
		require.False(t, seen[password])
		seen[password] = true
	}
}
