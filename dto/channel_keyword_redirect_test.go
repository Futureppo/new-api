package dto_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelKeywordRedirectSettings(t *testing.T) {
	var legacy dto.ChannelSettings
	require.NoError(t, common.Unmarshal([]byte(`{"force_format":true}`), &legacy))
	require.False(t, legacy.KeywordRedirect.Active())
	require.NoError(t, legacy.Validate())

	for _, enabled := range []bool{false, true} {
		want := dto.ChannelSettings{KeywordRedirect: &dto.ChannelKeywordRedirectSettings{
			Enabled: enabled, Rules: map[string]string{"苹果": "橘子", "delete": "", "  \n": "  ", "0": "false"},
		}}
		data, err := common.Marshal(want)
		require.NoError(t, err)
		var got dto.ChannelSettings
		require.NoError(t, common.Unmarshal(data, &got))
		require.NoError(t, got.Validate())
		require.Equal(t, want, got)
		require.Equal(t, enabled, got.KeywordRedirect.Active())
	}
	for _, input := range []string{
		`{"rules":{"":"replacement"}}`,
		`{"rules":{"word":null}}`,
		`{"rules":{"word":false}}`,
		`{"rules":{"word":0}}`,
		`{"rules":{"word":{}}}`,
		`{"rules":{"word":[]}}`,
		`{"rules":[]}`,
	} {
		var got dto.ChannelKeywordRedirectSettings
		require.Error(t, common.Unmarshal([]byte(input), &got), input)
	}
	settings := &dto.ChannelKeywordRedirectSettings{Enabled: true}
	require.NoError(t, settings.Validate())
	require.False(t, settings.Active())
}
