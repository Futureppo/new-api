package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTokenGroupsLegacyAndMultiGroupCompatibility(t *testing.T) {
	legacy := &Token{Group: "default"}
	require.Equal(t, []string{"default"}, legacy.GetGroups())
	require.Equal(t, "default", legacy.GetRequestGroup())

	multi := &Token{}
	require.NoError(t, multi.SetGroups([]string{" vip ", "default", "vip", ""}))
	require.Equal(t, "vip", multi.Group)
	require.NotEmpty(t, multi.Groups)
	require.Equal(t, []string{"vip", "default"}, multi.GetGroups())
	require.Equal(t, "auto", multi.GetRequestGroup())

	require.NoError(t, multi.SetGroups([]string{"default"}))
	require.Equal(t, "default", multi.Group)
	require.Empty(t, multi.Groups)
	require.Equal(t, []string{"default"}, multi.GetGroups())

	require.NoError(t, multi.SetGroups([]string{}))
	require.Empty(t, multi.Group)
	require.Empty(t, multi.Groups)
	require.Empty(t, multi.GetGroups())
}

func TestTokenGroupsMalformedJSONFallsBackToLegacyGroup(t *testing.T) {
	token := &Token{Group: "default", Groups: "not-json"}
	require.Equal(t, []string{"default"}, token.GetGroups())
}
