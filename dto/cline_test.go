package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestClineSyncDefaultsAndExplicitFalse(t *testing.T) {
	for _, payload := range []string{`{}`, `{"cline_auto_sync_free_models_enabled":null}`, `{"cline_auto_sync_free_models_enabled":true}`, `{"cline_auto_sync_free_models_enabled":false}`} {
		var settings ChannelOtherSettings
		require.NoError(t, common.UnmarshalJsonStr(payload, &settings))
		expected := payload != `{"cline_auto_sync_free_models_enabled":false}`
		require.Equal(t, expected, settings.ShouldSyncClineFreeModels())
		body, err := common.Marshal(settings)
		require.NoError(t, err)
		var decoded ChannelOtherSettings
		require.NoError(t, common.Unmarshal(body, &decoded))
		require.Equal(t, expected, decoded.ShouldSyncClineFreeModels())
	}
}
