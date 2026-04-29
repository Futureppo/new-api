package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupDisplayRequiresExplicitTrue(t *testing.T) {
	original := GroupDisplay2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupDisplayByJSONString(original))
	})

	require.NoError(t, UpdateGroupDisplayByJSONString(`{"visible":true,"hidden":false}`))

	require.True(t, IsGroupDisplayed("visible"))
	require.False(t, IsGroupDisplayed("hidden"))
	require.False(t, IsGroupDisplayed("missing"))
}

func TestCheckGroupDisplayRejectsNonBoolValues(t *testing.T) {
	require.NoError(t, CheckGroupDisplay(`{"visible":true,"hidden":false}`))
	require.Error(t, CheckGroupDisplay(`{"visible":"true"}`))
}
