package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGMICloudChannelRegistration(t *testing.T) {
	require.Equal(t, 67, constant.ChannelTypeGMICloud)
	require.Equal(t, "https://api.gmi-serving.com", constant.ChannelBaseURLs[constant.ChannelTypeGMICloud])
	require.Equal(t, "GMI Cloud", constant.GetChannelTypeName(constant.ChannelTypeGMICloud))

	apiType, ok := ChannelType2APIType(constant.ChannelTypeGMICloud)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
}

func TestGMICloudEndpointTypes(t *testing.T) {
	for _, modelName := range []string{"MiniMaxAI/MiniMax-M3", "o1-pro"} {
		require.Equal(
			t,
			[]constant.EndpointType{constant.EndpointTypeOpenAI},
			GetEndpointTypesByChannelType(constant.ChannelTypeGMICloud, modelName),
		)
	}
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeBatchGeneration},
		GetEndpointTypesByChannelType(constant.ChannelTypeGMICloud, "Gemini-batch-inference"),
	)
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeBatchGeneration},
		GetEndpointTypesByChannelType(constant.ChannelTypeGMICloud, "gemini-batch-inference"),
	)
	endpoint, ok := GetDefaultEndpointInfo(constant.EndpointTypeBatchGeneration)
	require.True(t, ok)
	require.Equal(t, "/v1/batch/generations", endpoint.Path)
	require.Equal(t, "POST", endpoint.Method)
}
