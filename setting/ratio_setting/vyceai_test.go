package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVyceAIModelsAreFreeByDefault(t *testing.T) {
	prices := GetDefaultModelPriceMap()
	for _, model := range []string{
		"你妈-1x1",
		"你妈-16x9",
		"你妈-9x16",
		"你妈-2x3",
		"你妈-3x2",
		"你妈-4x3",
	} {
		price, ok := prices[model]
		require.True(t, ok, model)
		require.Zero(t, price, model)
	}
}
