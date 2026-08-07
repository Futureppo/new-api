package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestOpenCodeResponsesModelsAlwaysUseResponses(t *testing.T) {
	require.True(t, ShouldChatCompletionsUseResponsesGlobal(0, constant.ChannelTypeOpenCode, "gpt-5.6-sol"))
	require.True(t, ShouldChatCompletionsUseResponsesGlobal(0, constant.ChannelTypeOpenCode, "grok-4.5"))
	require.False(t, ShouldChatCompletionsUseResponsesGlobal(0, constant.ChannelTypeOpenCode, "big-pickle"))
}
