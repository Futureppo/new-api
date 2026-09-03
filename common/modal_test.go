package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestModalChannelRegistration(t *testing.T) {
	require.Equal(t, 69, constant.ChannelTypeModal)
	require.Empty(t, constant.ChannelBaseURLs[constant.ChannelTypeModal])
	require.Equal(t, "Modal", constant.GetChannelTypeName(constant.ChannelTypeModal))

	apiType, ok := ChannelType2APIType(constant.ChannelTypeModal)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
}

func TestModalEndpointTypes(t *testing.T) {
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeOpenAI},
		GetEndpointTypesByChannelType(constant.ChannelTypeModal, "orcarouter/Qwen3.8-27B-Uncensored-FP8"),
	)
}
