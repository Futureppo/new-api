package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func setupProbeBatch(t *testing.T) (User, ProbeUserBatchPolicy) {
	t.Helper()
	setupUserDisableTestDB(t)
	require.NoError(t, DB.AutoMigrate(&ProbeUserAbuseState{}))
	user := User{Username: "batch", AffCode: "batch", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	return user, ProbeUserBatchPolicy{WindowSeconds: 30, RequestCount: 3}
}

func recordTestProbe(t *testing.T, user User, policy ProbeUserBatchPolicy, started, now time.Time) *ProbeUserPenaltyResult {
	t.Helper()
	result, err := recordProbeUserMatchAt(user.Id, started, "text:test", "same-request-id", policy, 10, 60, func(int, UserDisablePeriod) string { return "batch test" }, now)
	require.NoError(t, err)
	return result
}

func TestProbeBatchSlidingWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds []int
		counts  []int
		banned  bool
	}{
		{"three_identical_timestamps", []int{0, 0, 0}, []int{1, 2, 3}, true},
		{"rolling_not_fixed_window", []int{0, 20, 31, 45}, []int{1, 2, 2, 3}, true},
		{"boundary_expires", []int{0, 1, 30}, []int{1, 2, 2}, false},
		{"separate_batches_do_not_accumulate", []int{0, 1, 32, 33, 64}, []int{1, 2, 1, 2, 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user, policy := setupProbeBatch(t)
			base := time.Now()
			for i, seconds := range tc.seconds {
				now := base.Add(time.Duration(seconds) * time.Second)
				result := recordTestProbe(t, user, policy, now, now)
				require.Equal(t, tc.counts[i], result.MatchedRequests)
				require.Equal(t, tc.banned && i == len(tc.seconds)-1, result.Applied)
				if !result.Applied {
					require.Zero(t, result.State.OffenseCount)
					require.Equal(t, common.UserStatusEnabled, result.User.Status)
				}
			}
		})
	}
}

func TestProbeBatchUsesArrivalTimeAndHandlesOutOfOrderRequests(t *testing.T) {
	user, policy := setupProbeBatch(t)
	base := time.Now()
	// A request older than the whole window does not create a new hit on arrival.
	require.Zero(t, recordTestProbe(t, user, policy, base.Add(-31*time.Second), base).MatchedRequests)
	require.Equal(t, 1, recordTestProbe(t, user, policy, base, base).MatchedRequests)
	require.Equal(t, 2, recordTestProbe(t, user, policy, base.Add(20*time.Second), base.Add(20*time.Second)).MatchedRequests)
	// A delayed request can still count while its original arrival is in the window.
	result := recordTestProbe(t, user, policy, base.Add(10*time.Second), base.Add(31*time.Second))
	require.Equal(t, 2, result.MatchedRequests) // the request at t=0 expired
	require.False(t, result.Applied)
}

func TestProbeBatchPolicyChangesAndDryRunStartFresh(t *testing.T) {
	for _, change := range []string{"window", "threshold", "enforce", "dry_run"} {
		t.Run(change, func(t *testing.T) {
			user, policy := setupProbeBatch(t)
			policy.DryRun = change == "enforce"
			for i := 0; i < 2; i++ {
				require.False(t, recordTestProbe(t, user, policy, time.Now(), time.Now()).ThresholdReached)
			}
			switch change {
			case "window":
				policy.WindowSeconds = 60
			case "threshold":
				policy.RequestCount = 2
			default:
				policy.DryRun = !policy.DryRun
			}
			result := recordTestProbe(t, user, policy, time.Now(), time.Now())
			require.Equal(t, 1, result.MatchedRequests)
			require.False(t, result.Applied)
			require.False(t, result.ThresholdReached)
		})
	}
}

func TestProbeBatchDryRunAndReset(t *testing.T) {
	user, policy := setupProbeBatch(t)
	policy.DryRun = true
	for i := 1; i <= 3; i++ {
		result := recordTestProbe(t, user, policy, time.Now(), time.Now())
		require.Equal(t, i == 3, result.ThresholdReached)
		require.False(t, result.Applied)
		require.Zero(t, result.State.OffenseCount)
		require.Zero(t, result.State.LastOffenseAt)
		require.Equal(t, common.UserStatusEnabled, result.User.Status)
	}
	// Reset discards the pending window as well as offenses and rejects old work.
	policy.DryRun = false
	oldStart := time.Now()
	for i := 0; i < 2; i++ {
		recordTestProbe(t, user, policy, time.Now(), time.Now())
	}
	require.NoError(t, ResetProbeUserAbuseState(user.Id))
	state, err := GetProbeUserAbuseState(user.Id)
	require.NoError(t, err)
	require.Empty(t, state.PendingBatch)
	require.True(t, recordTestProbe(t, user, policy, oldStart, time.Now()).Ignored)
	fresh := time.Unix(0, state.LastEventAt+1)
	require.Equal(t, 1, recordTestProbe(t, user, policy, fresh, fresh).MatchedRequests)
}

func TestProbeBatchThresholdOneAndMaximumStorage(t *testing.T) {
	user, policy := setupProbeBatch(t)
	policy.RequestCount = 1000
	now := time.Now()
	// Preload 998 valid timestamps to exercise the largest persisted state.
	pending := probeUserBatch{WindowSeconds: 30, RequestCount: 1000, Requests: make([]int64, 998)}
	for i := range pending.Requests {
		pending.Requests[i] = now.UnixNano()
	}
	data, err := common.Marshal(pending)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&ProbeUserAbuseState{UserId: user.Id, PendingBatch: string(data)}).Error)
	result := recordTestProbe(t, user, policy, now, now)
	require.Equal(t, 999, result.MatchedRequests)
	require.Less(t, len(result.State.PendingBatch), 65535)
	require.True(t, recordTestProbe(t, user, policy, now, now).Applied)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(UserEnableUpdates()).Error)
	policy.RequestCount = 1
	fresh := now.Add(time.Second)
	require.True(t, recordTestProbe(t, user, policy, fresh, fresh).Applied)
}
