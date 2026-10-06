package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestMistralConsoleAdaptorRegistration(t *testing.T) {
	adaptor := GetAdaptor(constant.APITypeMistralConsole)
	require.NotNil(t, adaptor)
	require.Equal(t, "mistral-console", adaptor.GetChannelName())
	require.Equal(t, []string{
		"codestral-latest", "ministral-14b-latest", "ministral-3b-latest",
		"ministral-8b-latest", "mistral-medium-latest", "mistral-small-latest",
		"mistral-large-4", "labs-leanstral-1-5-1",
	}, adaptor.GetModelList())
}
