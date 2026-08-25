package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestVyceAIChannelRegistration(t *testing.T) {
	require.Equal(t, 68, constant.ChannelTypeVyceAI)
	require.Equal(t, "https://vyceai.com", constant.ChannelBaseURLs[constant.ChannelTypeVyceAI])
	require.Equal(t, "VyceAI", constant.GetChannelTypeName(constant.ChannelTypeVyceAI))

	apiType, ok := ChannelType2APIType(constant.ChannelTypeVyceAI)
	require.True(t, ok)
	require.Equal(t, constant.APITypeVyceAI, apiType)
}

func TestVyceAIOnlySupportsImageGenerationEndpoint(t *testing.T) {
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeImageGeneration},
		GetEndpointTypesByChannelType(constant.ChannelTypeVyceAI, "你妈-16x9"),
	)
}
