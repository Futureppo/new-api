package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeSafeDefaultPricingAndSavedOverrides(t *testing.T) {
	oldRatios, oldCompletion, oldPrices := ModelRatio2JSONString(), CompletionRatio2JSONString(), ModelPrice2JSONString()
	t.Cleanup(func() {
		_ = UpdateModelRatioByJSONString(oldRatios)
		_ = UpdateCompletionRatioByJSONString(oldCompletion)
		_ = UpdateModelPriceByJSONString(oldPrices)
	})
	require.NoError(t, UpdateModelRatioByJSONString(`{"jev-latest":0,"jev-preview":0.1,"custom":3}`))
	require.NoError(t, UpdateCompletionRatioByJSONString(`{"jev-preview":2}`))
	require.NoError(t, UpdateModelPriceByJSONString(`{"jev-latest":0}`))
	for _, name := range []string{"jev-latest", "jev-preview", "jev-1.13.0"} {
		require.Equal(t, 0.021, defaultModelRatio[name])
		ratio, ok, _ := GetModelRatio(name)
		require.True(t, ok)
		require.Equal(t, map[string]float64{"jev-latest": 0, "jev-preview": 0.1, "jev-1.13.0": 0.021}[name], ratio)
		require.Equal(t, map[string]float64{"jev-preview": 2}[name], GetCompletionRatio(name))
	}
	price, ok := GetModelPrice("jev-latest", false)
	require.True(t, ok)
	require.Zero(t, price)
}
