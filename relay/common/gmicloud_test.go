package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestGMICloudDoesNotAdvertiseUndocumentedStreamOptions(t *testing.T) {
	require.False(t, streamSupportedChannels[constant.ChannelTypeGMICloud])
}
