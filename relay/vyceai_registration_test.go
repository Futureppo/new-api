package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/vyceai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestVyceAIAdaptorRegistration(t *testing.T) {
	adaptor := GetAdaptor(constant.APITypeVyceAI)
	require.NotNil(t, adaptor)
	require.Equal(t, vyceai.ChannelName, adaptor.GetChannelName())
	require.Equal(t, vyceai.ModelList, adaptor.GetModelList())
}

func TestVyceAIImageRequestsNeverPassThrough(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVyceAI}}
	require.False(t, shouldPassThroughImageRequest(info))
}
