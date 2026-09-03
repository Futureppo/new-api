package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestModalDoesNotAdvertiseStreamOptions(t *testing.T) {
	// Modal hosts arbitrary user applications, so stream_options support cannot
	// be guaranteed even though ordinary OpenAI-compatible streaming still works.
	require.False(t, streamSupportedChannels[constant.ChannelTypeModal])
}
