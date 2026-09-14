package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedDeferredTask(t *testing.T, source string) (*model.Task, *model.Channel) {
	task, channel := seedAgnesPollingTask(t, source)
	task.Quota = 0
	if source == BillingSourceSubscription {
		task.Quota = 1
		require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", 1).Update("amount_used", 101).Error)
	}
	task.PrivateData.BillingContext.DeferredSettlement = true
	task.PrivateData.BillingContext.EstimatedQuota = -500
	task.PrivateData.BillingContext.ModelPrice = -0.001
	require.NoError(t, task.Update())
	return task, channel
}

func TestDeferredTaskSuccessfulCreditOnce(t *testing.T) {
	for _, source := range []string{BillingSourceWallet, BillingSourceSubscription} {
		t.Run(source, func(t *testing.T) {
			task, channel := seedDeferredTask(t, source)
			// Submission has persisted only the reservation, never a credit.
			require.Equal(t, 100000, getUserQuota(t, 1))
			first, second := reloadAgnesTask(t, task.ID), reloadAgnesTask(t, task.ID)
			adaptor := &agnesPollingFixture{status: model.TaskStatusSuccess, started: make(chan struct{}, 2), release: make(chan struct{})}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for _, copy := range []*model.Task{first, second} {
				wg.Add(1)
				go func(task *model.Task) {
					defer wg.Done()
					errs <- RefreshVideoTask(context.Background(), adaptor, channel, task)
				}(copy)
			}
			for i := 0; i < 2; i++ {
				select {
				case <-adaptor.started:
				case <-time.After(5 * time.Second):
					t.Fatal("poller did not start")
				}
			}
			close(adaptor.release)
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			saved := reloadAgnesTask(t, task.ID)
			require.Equal(t, -500, saved.Quota)
			require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), saved.Status)
			require.NoError(t, RefreshVideoTask(context.Background(), adaptor, channel, saved))
			require.Equal(t, int64(1), countLogs(t))
			require.Zero(t, adaptor.settlements.Load(), "upstream cost must not replace configured negative pricing")
			if source == BillingSourceWallet {
				require.Equal(t, 100500, getUserQuota(t, 1))
				require.Equal(t, 100500, getTokenRemainQuota(t, 1))
			} else {
				require.Equal(t, 100000, getUserQuota(t, 1))
				require.Zero(t, getSubscriptionUsed(t, 1))
				require.Equal(t, 100501, getTokenRemainQuota(t, 1))
			}
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.Equal(t, -500, user.UsedQuota)
			require.Equal(t, 1, user.RequestCount)
		})
	}
}

func TestDeferredTaskFailureAndTimeoutOnlyRefundReservations(t *testing.T) {
	for _, source := range []string{BillingSourceWallet, BillingSourceSubscription} {
		for _, timeout := range []bool{false, true} {
			t.Run(source+map[bool]string{true: " timeout", false: " failure"}[timeout], func(t *testing.T) {
				task, channel := seedDeferredTask(t, source)
				reserved := task.Quota
				if timeout {
					previous := constant.TaskTimeoutMinutes
					constant.TaskTimeoutMinutes = 1
					t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })
					task.SubmitTime = time.Now().Add(-2 * time.Minute).Unix()
					require.NoError(t, task.Update())
					sweepTimedOutTasks(context.Background())
					sweepTimedOutTasks(context.Background())
				} else {
					require.NoError(t, RefreshVideoTask(context.Background(), &agnesPollingFixture{status: model.TaskStatusFailure}, channel, task))
				}
				require.Equal(t, 100000, getUserQuota(t, 1))
				require.Equal(t, 100000+reserved, getTokenRemainQuota(t, 1))
				require.Zero(t, reloadAgnesTask(t, task.ID).Quota)
				if source == BillingSourceSubscription {
					require.Equal(t, int64(100), getSubscriptionUsed(t, 1))
				}
				require.Equal(t, int64(1), countLogs(t))
			})
		}
	}
}

func TestDeferredTaskTransactionFailureCanRetry(t *testing.T) {
	task, channel := seedDeferredTask(t, BillingSourceWallet)
	adaptor := &agnesPollingFixture{status: model.TaskStatusSuccess}
	callback := "test:fail_deferred_token"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(errors.New("simulated token write failure"))
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove(callback) })
	require.Error(t, RefreshVideoTask(context.Background(), adaptor, channel, task))
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.Status)
	require.Equal(t, task.Status, reloadAgnesTask(t, task.ID).Status)
	require.Equal(t, 100000, getUserQuota(t, 1))
	require.Equal(t, 100000, getTokenRemainQuota(t, 1))
	require.Zero(t, countLogs(t))
	require.NoError(t, model.DB.Callback().Update().Remove(callback))
	require.NoError(t, RefreshVideoTask(context.Background(), adaptor, channel, task))
	require.Equal(t, 100500, getUserQuota(t, 1))
	require.Equal(t, int64(1), countLogs(t))
}

func TestDeferredTaskUsesFrozenPricesAndUsage(t *testing.T) {
	task := &model.Task{Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{
		DeferredSettlement: true, EstimatedQuota: -100, ModelRatio: -2, GroupRatio: 0.5, OtherRatios: map[string]float64{"seconds": 3},
	}}}
	require.Equal(t, -30, deferredTaskQuota(task, &relaycommon.TaskInfo{TotalTokens: 10}))
	require.Equal(t, -100, deferredTaskQuota(task, &relaycommon.TaskInfo{}))
	task.PrivateData.BillingContext.PerCallBilling = true
	require.Equal(t, -100, deferredTaskQuota(task, &relaycommon.TaskInfo{TotalTokens: 10}))
	task.Status = model.TaskStatusFailure
	require.Zero(t, deferredTaskQuota(task, nil))
}

func TestDeferredSunoTerminalUpdates(t *testing.T) {
	for _, status := range []string{model.TaskStatusSuccess, model.TaskStatusFailure} {
		t.Run(status, func(t *testing.T) {
			task, _ := seedDeferredTask(t, BillingSourceSubscription)
			stale := *task
			result := dto.SunoDataResponse{TaskID: task.TaskID, Status: status}
			require.NoError(t, updateDeferredSunoTask(context.Background(), task, result))
			require.NoError(t, updateDeferredSunoTask(context.Background(), &stale, result))
			require.Equal(t, model.TaskStatus(status), reloadAgnesTask(t, task.ID).Status)
			require.Equal(t, int64(1), countLogs(t))
			require.Equal(t, 100000, getUserQuota(t, 1))
			if status == model.TaskStatusSuccess {
				require.Zero(t, getSubscriptionUsed(t, 1))
			} else {
				require.Equal(t, int64(100), getSubscriptionUsed(t, 1))
			}
		})
	}
}

func TestDeferredTaskMissingChannelRefundsReservation(t *testing.T) {
	for _, platform := range []constant.TaskPlatform{constant.TaskPlatformSuno, "video"} {
		t.Run(string(platform), func(t *testing.T) {
			task, _ := seedDeferredTask(t, BillingSourceSubscription)
			const missingChannel = 999999
			task.ChannelId = missingChannel
			require.NoError(t, task.Update())
			stale := *task
			for _, copy := range []*model.Task{task, &stale} {
				ids := []string{copy.GetUpstreamTaskID()}
				tasks := map[string]*model.Task{ids[0]: copy}
				if platform == constant.TaskPlatformSuno {
					_ = updateSunoTasks(context.Background(), missingChannel, ids, tasks)
				} else {
					_ = updateVideoTasks(context.Background(), platform, missingChannel, ids, tasks)
				}
			}
			require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloadAgnesTask(t, task.ID).Status)
			require.Equal(t, int64(100), getSubscriptionUsed(t, 1))
			require.Equal(t, 100000, getUserQuota(t, 1))
			require.Equal(t, int64(1), countLogs(t))
		})
	}
}
