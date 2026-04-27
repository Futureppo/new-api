package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestRetryParamEffectiveRetryTimes(t *testing.T) {
	originalRetryTimes := common.RetryTimes
	t.Cleanup(func() {
		common.RetryTimes = originalRetryTimes
	})
	common.RetryTimes = 3

	param := &RetryParam{}
	require.Equal(t, 3, param.GetEffectiveRetryTimes())

	zero := 0
	param.SetEffectiveRetryTimesFromChannel(&model.Channel{RetryTimes: &zero})
	require.Equal(t, 0, param.GetEffectiveRetryTimes())
	require.Equal(t, 0, param.GetRemainingRetryTimes())

	two := 2
	param.SetEffectiveRetryTimesFromChannel(&model.Channel{RetryTimes: &two})
	require.Equal(t, 2, param.GetEffectiveRetryTimes())

	param.SetRetry(1)
	require.Equal(t, 1, param.GetRemainingRetryTimes())

	param.SetEffectiveRetryTimesFromChannel(&model.Channel{})
	require.Equal(t, 3, param.GetEffectiveRetryTimes())
	require.Equal(t, 2, param.GetRemainingRetryTimes())
}
