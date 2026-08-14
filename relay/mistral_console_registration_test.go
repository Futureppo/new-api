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
	require.Equal(t, []string{"glm-5-2"}, adaptor.GetModelList())
}
