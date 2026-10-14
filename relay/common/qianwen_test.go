package common

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestQianwenSupportsStreamUsage(t *testing.T) {
	require.True(t, streamSupportedChannels[constant.ChannelTypeQianwenAI])
}
