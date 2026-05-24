package setting

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func restoreModelRequestRateLimitGroup(t *testing.T) {
	t.Helper()

	ModelRequestRateLimitMutex.RLock()
	oldGroup := ModelRequestRateLimitGroup
	ModelRequestRateLimitMutex.RUnlock()

	t.Cleanup(func() {
		ModelRequestRateLimitMutex.Lock()
		defer ModelRequestRateLimitMutex.Unlock()
		ModelRequestRateLimitGroup = oldGroup
	})
}

func TestModelRequestRateLimitGroupSupportsConcurrencyOverride(t *testing.T) {
	restoreModelRequestRateLimitGroup(t)

	err := UpdateModelRequestRateLimitGroupByJSONString(`{"default":[200,100,2],"legacy":[0,1000]}`)
	require.NoError(t, err)

	totalCount, successCount, concurrencyLimit, hasConcurrencyLimit, found := GetGroupRateLimit("default")
	require.True(t, found)
	require.Equal(t, 200, totalCount)
	require.Equal(t, 100, successCount)
	require.True(t, hasConcurrencyLimit)
	require.Equal(t, 2, concurrencyLimit)

	totalCount, successCount, concurrencyLimit, hasConcurrencyLimit, found = GetGroupRateLimit("legacy")
	require.True(t, found)
	require.Equal(t, 0, totalCount)
	require.Equal(t, 1000, successCount)
	require.False(t, hasConcurrencyLimit)
	require.Equal(t, 0, concurrencyLimit)
}

func TestCheckModelRequestRateLimitGroupRejectsInvalidConcurrency(t *testing.T) {
	require.Error(t, CheckModelRequestRateLimitGroup(`{"default":[200,100,-1]}`))
	require.Error(t, CheckModelRequestRateLimitGroup(`{"default":[200]}`))
	require.Error(t, CheckModelRequestRateLimitGroup(`{"default":[200,100,2,1]}`))
	require.Error(t, CheckModelRequestRateLimitGroup(fmt.Sprintf(`{"default":[200,100,%d]}`, int64(math.MaxInt32)+1)))
}

func TestCheckModelRequestConcurrencyLimit(t *testing.T) {
	require.NoError(t, CheckModelRequestConcurrencyLimit("0"))
	require.NoError(t, CheckModelRequestConcurrencyLimit("2"))
	require.Error(t, CheckModelRequestConcurrencyLimit("-1"))
	require.Error(t, CheckModelRequestConcurrencyLimit(fmt.Sprintf("%d", int64(math.MaxInt32)+1)))
}
