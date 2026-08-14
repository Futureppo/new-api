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

func TestCerebrasChannelMapsToCerebrasAPI(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeCerebras)
	require.True(t, ok)
	require.Equal(t, constant.APITypeCerebras, apiType)
}

func TestMistralConsoleChannelMapsToMistralConsoleAPI(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeMistralConsole)
	require.True(t, ok)
	require.Equal(t, constant.APITypeMistralConsole, apiType)
	require.Equal(t, "https://console.mistral.ai", constant.ChannelBaseURLs[constant.ChannelTypeMistralConsole])
	require.Equal(t, "Mistral Console", constant.GetChannelTypeName(constant.ChannelTypeMistralConsole))
}

func TestOpenAILocalImageModelsAreRecognized(t *testing.T) {
	require.True(t, IsImageGenerationModel("gpt-image-2"))
	require.True(t, IsImageGenerationModel("codex-gpt-image-2"))
}
