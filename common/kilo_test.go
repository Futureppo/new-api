package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestKiloChannelRegistration(t *testing.T) {
	require.Equal(t, 70, constant.ChannelTypeKilo)
	require.Len(t, constant.ChannelBaseURLs, constant.ChannelTypeDummy)
	require.Equal(t, "https://api.kilo.ai/api/gateway", constant.ChannelBaseURLs[constant.ChannelTypeKilo])
	require.Equal(t, "Kilo", constant.GetChannelTypeName(constant.ChannelTypeKilo))
	apiType, ok := ChannelType2APIType(constant.ChannelTypeKilo)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, GetEndpointTypesByChannelType(constant.ChannelTypeKilo, "openrouter/free"))
}
