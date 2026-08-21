package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	checkinExpiryTickInterval = 5 * time.Minute
	checkinExpiryBatchSize    = 300
	// 只回收最近若干天的签到额度。功能刚开启时库里可能积压大量历史记录，
	// 若直接回收会让用户余额毫无预警地缩水，因此超出窗口的记录只标记不扣减。
	checkinExpiryMaxLookbackDays = 3
)

var (
	checkinExpiryOnce    sync.Once
	checkinExpiryRunning atomic.Bool
)

// StartCheckinExpiryTask 启动签到额度当日有效的清算任务。
// 与订阅重置任务一致：仅主节点执行，进程内只启动一次。
func StartCheckinExpiryTask() {
	checkinExpiryOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf(
				"checkin quota expiry task started: tick=%s", checkinExpiryTickInterval))
			ticker := time.NewTicker(checkinExpiryTickInterval)
			defer ticker.Stop()

			runCheckinExpiryOnce()
			for range ticker.C {
				runCheckinExpiryOnce()
			}
		})
	})
}

func runCheckinExpiryOnce() {
	setting := operation_setting.GetCheckinSetting()
	if setting == nil || !setting.IsExpireEnabled() {
		return
	}
	if !checkinExpiryRunning.CompareAndSwap(false, true) {
		return
	}
	defer checkinExpiryRunning.Store(false)

	ctx := context.Background()
	mode := setting.NormalizedExpireMode()
	now := time.Now()
	today := now.Format("2006-01-02")
	cutoff := now.AddDate(0, 0, -checkinExpiryMaxLookbackDays).Format("2006-01-02")

	totalSettled, totalWrittenOff := 0, 0
	var totalReclaimed int64

	// 逐日结算：先处理最早的未清算日期，直到没有早于今天的记录为止。
	for {
		date, err := model.OldestUnsettledCheckinDate(today)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("checkin expiry: query pending date failed: %v", err))
			return
		}
		if date == "" {
			break
		}

		if date < cutoff {
			n, err := model.WriteOffCheckinDate(date, checkinExpiryBatchSize)
			if err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("checkin expiry: write-off %s failed: %v", date, err))
				return
			}
			if n == 0 {
				break
			}
			totalWrittenOff += n
			continue
		}

		settled, reclaimed, err := model.SettleCheckinDate(date, mode, checkinExpiryBatchSize)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("checkin expiry: settle %s failed: %v", date, err))
			return
		}
		if settled == 0 {
			break
		}
		totalSettled += settled
		totalReclaimed += reclaimed
	}

	if totalSettled > 0 || totalWrittenOff > 0 {
		logger.LogInfo(ctx, fmt.Sprintf(
			"checkin expiry done: mode=%s settled=%d reclaimed=%s skipped_stale=%d",
			mode, totalSettled, logger.LogQuota(int(totalReclaimed)), totalWrittenOff))
	}
}
