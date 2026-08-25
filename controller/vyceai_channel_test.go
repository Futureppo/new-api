package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/vyceai"
	"github.com/stretchr/testify/require"
)

func TestVyceAIChannelTestUsesImageGeneration(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeVyceAI}
	require.Equal(
		t,
		string(constant.EndpointTypeImageGeneration),
		normalizeChannelTestEndpoint(channel, "你妈-1x1", ""),
	)
}

func TestVyceAIDashboardModels(t *testing.T) {
	require.Equal(t, vyceai.ModelList, channelId2Models[constant.ChannelTypeVyceAI])
}
