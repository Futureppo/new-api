package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMiMoPricingAndSavedOverrides(t *testing.T) {
	for _, tc := range []struct {
		get    func() string
		update func(string) error
	}{
		{ModelRatio2JSONString, UpdateModelRatioByJSONString},
		{CompletionRatio2JSONString, UpdateCompletionRatioByJSONString},
		{CacheRatio2JSONString, UpdateCacheRatioByJSONString},
		{CreateCacheRatio2JSONString, UpdateCreateCacheRatioByJSONString},
	} {
		old := tc.get()
		t.Cleanup(func() { require.NoError(t, tc.update(old)) })
		require.NoError(t, tc.update(`{}`))
	}
	for _, tc := range []struct {
		model        string
		input, cache float64
	}{
		{"mimo-v2.5", 1, 0.02},
		{"mimo-v2.5-pro", 3, 0.025},
		{"mimo-v2.6-pro-ultraspeed", 3, 0.025},
	} {
		ratio, ok, _ := GetModelRatio(tc.model)
		require.True(t, ok)
		require.InDelta(t, tc.input, ratio/RMB*1000, 1e-10)
		require.Equal(t, 2.0, GetCompletionRatio(tc.model))
		cache, ok := GetCacheRatio(tc.model)
		require.True(t, ok)
		require.InDelta(t, tc.cache, ratio*cache/RMB*1000, 1e-10)
		write, ok := GetCreateCacheRatio(tc.model)
		require.True(t, ok)
		require.Zero(t, write)
	}
	for _, tc := range []struct {
		get    func() string
		update func(string) error
	}{
		{ModelRatio2JSONString, UpdateModelRatioByJSONString},
		{CompletionRatio2JSONString, UpdateCompletionRatioByJSONString},
		{CacheRatio2JSONString, UpdateCacheRatioByJSONString},
		{CreateCacheRatio2JSONString, UpdateCreateCacheRatioByJSONString},
	} {
		require.NoError(t, tc.update(`{"mimo-v2.5":0,"mimo-v2.5-pro":0.123,"unrelated":7}`))
		require.Contains(t, tc.get(), `"mimo-v2.5":0`)
		require.Contains(t, tc.get(), `"mimo-v2.5-pro":0.123`)
		require.Contains(t, tc.get(), `"unrelated":7`)
		require.Contains(t, tc.get(), `"mimo-v2.6-pro-ultraspeed":`)
	}
}
