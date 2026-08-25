package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestVercelChannelRegistration(t *testing.T) {
	require.Equal(t, 66, constant.ChannelTypeVercel)
	require.Equal(t, "https://ai-gateway.vercel.sh", constant.ChannelBaseURLs[constant.ChannelTypeVercel])
	require.Equal(t, "Vercel AI Gateway", constant.GetChannelTypeName(constant.ChannelTypeVercel))

	apiType, ok := ChannelType2APIType(constant.ChannelTypeVercel)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
}

func TestVercelEndpointTypes(t *testing.T) {
	require.Equal(
		t,
		[]constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse},
		GetEndpointTypesByChannelType(constant.ChannelTypeVercel, "minimax/minimax-m2.7-free"),
	)
}
