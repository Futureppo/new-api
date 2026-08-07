package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestShouldChatCompletionsUseResponsesUsesOpenCodeUpstreamModel(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "custom-model-alias",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenCode,
			UpstreamModelName: "gpt-5.6-sol",
		},
	}
	require.True(t, shouldChatCompletionsUseResponses(info))

	info.UpstreamModelName = "big-pickle"
	require.False(t, shouldChatCompletionsUseResponses(info))
}

func TestOpenCodeTextRequestsAlwaysUseAdaptorConversion(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenCode,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}
	require.False(t, shouldPassThroughTextRequest(info, true))
}
