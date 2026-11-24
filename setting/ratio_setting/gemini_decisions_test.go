package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeminiDecisionsDefaultPrice(t *testing.T) {
	require.Equal(t, 0.05, defaultModelRatio["gemini-decision-1"])
	ratio, ok := defaultCompletionRatio["gemini-decision-1"]
	require.True(t, ok)
	require.Zero(t, ratio)
}

func TestGeminiDecisionsSavedPricing(t *testing.T) {
	oldInput, oldOutput := ModelRatio2JSONString(), CompletionRatio2JSONString()
	t.Cleanup(func() {
		_ = UpdateModelRatioByJSONString(oldInput)
		_ = UpdateCompletionRatioByJSONString(oldOutput)
	})
	require.NoError(t, UpdateModelRatioByJSONString(`{"existing":1}`))
	require.NoError(t, UpdateCompletionRatioByJSONString(`{}`))
	input, ok, _ := GetModelRatio("gemini-decision-1")
	require.True(t, ok)
	require.Equal(t, 0.05, input)
	require.Zero(t, GetCompletionRatio("gemini-decision-1"))
	require.NoError(t, UpdateModelRatioByJSONString(`{"gemini-decision-1":0}`))
	require.NoError(t, UpdateCompletionRatioByJSONString(`{"gemini-decision-1":2}`))
	input, ok, _ = GetModelRatio("gemini-decision-1")
	require.True(t, ok)
	require.Zero(t, input)
	require.Equal(t, float64(2), GetCompletionRatio("gemini-decision-1"))
}
