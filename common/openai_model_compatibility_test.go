package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestOpenAIResponseOnlyModelBoundaries(t *testing.T) {
	for _, model := range []string{"o3-pro", "o3-pro-2025-06-10", "gpt-5.1-codex-max", "gpt-5.5-pro", "gpt-5.5-pro-high"} {
		require.True(t, IsOpenAIResponseOnlyModel(model), model)
		require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponse}, GetEndpointTypesByChannelType(constant.ChannelTypeOpenAI, model))
	}
	for _, model := range []string{"custom-o3-pro", "o3-pro-2099-01-01", "provider/gpt-5.5-pro", "gpt-6-astra"} {
		require.False(t, IsOpenAIResponseOnlyModel(model), model)
	}
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, GetEndpointTypesByChannelType(constant.ChannelTypeAzure, "gpt-5.5-pro"))
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIResponse}, GetEndpointTypesByChannelType(constant.ChannelTypeAzure, "o3-pro-2025-06-10"))
}
