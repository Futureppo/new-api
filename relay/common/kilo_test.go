package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestKiloStreamOptions(t *testing.T) {
	require.True(t, streamSupportedChannels[constant.ChannelTypeKilo])
}
