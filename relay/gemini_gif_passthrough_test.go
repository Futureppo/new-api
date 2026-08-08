package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestGeminiGifFilteringTakesPriorityOverPassThrough(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeGemini,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	require.False(t, shouldPassThroughTextRequest(info, false))
	require.False(t, shouldPassThroughTextRequest(info, true))

	info.ChannelOtherSettings.RemoveGifImagesEnabled = common.GetPointer(false)
	require.True(t, shouldPassThroughTextRequest(info, false))
	require.True(t, shouldPassThroughTextRequest(info, true))
}
