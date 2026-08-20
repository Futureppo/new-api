package setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupDescriptionsPersistIndependentlyFromUserUsableGroups(t *testing.T) {
	originalDescriptions := GroupDescriptions2JSONString()
	originalUsableGroups := UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDescriptionsByJSONString(originalDescriptions))
		require.NoError(t, UpdateUserUsableGroupsByJSONString(originalUsableGroups))
	})

	require.NoError(t, UpdateGroupDescriptionsByJSONString(`{"private":"长期保留"}`))
	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{}`))

	require.JSONEq(t, `{"private":"长期保留"}`, GroupDescriptions2JSONString())
	require.JSONEq(t, `{}`, UserUsableGroups2JSONString())
	require.Empty(t, GetUserUsableGroupsCopy())

	require.NoError(t, UpdateUserUsableGroupsByJSONString(`{"private":"旧描述"}`))
	require.Equal(t, "长期保留", GetUserUsableGroupsCopy()["private"])
}

func TestInvalidGroupDescriptionsDoNotReplaceExistingValues(t *testing.T) {
	originalDescriptions := GroupDescriptions2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDescriptionsByJSONString(originalDescriptions))
	})

	require.NoError(t, UpdateGroupDescriptionsByJSONString(`{"default":"默认分组"}`))
	require.Error(t, CheckGroupDescriptions(`{"default":false}`))
	require.Error(t, UpdateGroupDescriptionsByJSONString(`{"default":false}`))
	require.JSONEq(t, `{"default":"默认分组"}`, GroupDescriptions2JSONString())
}
