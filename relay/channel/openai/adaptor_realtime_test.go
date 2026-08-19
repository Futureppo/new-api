package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRealtimeRequestPathWithModel(t *testing.T) {
	require.Equal(
		t,
		"/v1/realtime?intent=transcription&model=upstream-model",
		realtimeRequestPathWithModel("/v1/realtime?model=request-alias&intent=transcription", "upstream-model"),
	)
	require.Equal(t, "/v1/realtime?model=upstream-model", realtimeRequestPathWithModel("/v1/realtime", "upstream-model"))
	require.Equal(t, "/v1/realtime?model=request-alias", realtimeRequestPathWithModel("/v1/realtime?model=request-alias", ""))
}
