package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestDecisionsEndpointDiscoveryAndTestCapabilities(t *testing.T) {
	info, ok := GetDefaultEndpointInfo(constant.EndpointTypeOpenAIDecisions)
	require.True(t, ok)
	require.Equal(t, EndpointInfo{Path: "/v1/decisions", Method: "POST"}, info)
	endpoints := GetEndpointTypesByChannelType(constant.ChannelTypeOpenAI, "gpt-6-luna")
	require.Equal(t, constant.EndpointTypeOpenAIDecisions, endpoints[len(endpoints)-1])
	require.NotEqual(t, constant.EndpointTypeOpenAIDecisions, endpoints[0])
	require.NotContains(t, GetEndpointTypesByChannelType(constant.ChannelTypeOpenAI, "gpt-6-sol"), constant.EndpointTypeOpenAIDecisions)
	require.NotContains(t, GetEndpointTypesByChannelType(constant.ChannelTypeAzure, "gpt-6-luna"), constant.EndpointTypeOpenAIDecisions)
	for _, endpoint := range []constant.EndpointType{constant.EndpointTypeOpenAIDecisions, constant.EndpointTypeModerations, constant.EndpointTypeAudioSpeech, constant.EndpointTypeAudioTranscription, constant.EndpointTypeBatchGeneration} {
		require.False(t, ChannelTestEndpointSupportsStream(endpoint))
	}
	require.True(t, ChannelTestEndpointSupportsStream(constant.EndpointTypeOpenAIResponse))
}
