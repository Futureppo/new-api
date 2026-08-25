package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestVercelDoesNotAdvertiseStreamOptionsBeforeLiveVerification(t *testing.T) {
	require.False(t, streamSupportedChannels[constant.ChannelTypeVercel])
}
