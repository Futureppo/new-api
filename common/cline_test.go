package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestClineChannelRegistration(t *testing.T) {
	require.Equal(t, 73, constant.ChannelTypeCline)
	require.Equal(t, constant.ChannelTypeDummy, len(constant.ChannelBaseURLs))
	require.Equal(t, "https://api.cline.bot/api", constant.ChannelBaseURLs[constant.ChannelTypeCline])
	require.Equal(t, "Cline", constant.GetChannelTypeName(constant.ChannelTypeCline))
	apiType, ok := ChannelType2APIType(constant.ChannelTypeCline)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, GetEndpointTypesByChannelType(constant.ChannelTypeCline, "cline-free/a"))
}
