package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestOpenAILocalChannelMapsToOpenAIAPI(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeOpenAILocal)
	require.True(t, ok)
	require.Equal(t, constant.APITypeOpenAI, apiType)
}

func TestOpenAILocalImageModelsAreRecognized(t *testing.T) {
	require.True(t, IsImageGenerationModel("gpt-image-2"))
	require.True(t, IsImageGenerationModel("codex-gpt-image-2"))
}
