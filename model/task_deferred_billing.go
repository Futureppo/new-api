package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CompleteDeferredTask commits the terminal transition and its funding/token
// adjustment together. A failed transaction leaves the task eligible for polling;
// a competing poller cannot commit the same credit twice.
func CompleteDeferredTask(task *Task, fromStatus TaskStatus, actualQuota int) (bool, error) {
	if !task.HasDeferredBilling() || fromStatus == TaskStatusSuccess || fromStatus == TaskStatusFailure {
		return false, errors.New("invalid deferred task transition")
	}
	if task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure {
		return false, errors.New("deferred billing requires a terminal task")
	}
	if task.Status == TaskStatusFailure {
		actualQuota = 0
	}
	completed := *task
	completed.Quota = actualQuota
	delta := actualQuota - task.Quota
	won := false
	var tokenKey string
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Task{}).Where("id = ? AND status = ?", task.ID, fromStatus).
			Select("*").Updates(&completed)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		won = true
		var err error
		tokenKey, err = applyDeferredTaskFunding(tx, task, actualQuota, delta)
		return err
	})
	if err != nil {
		return false, err
	}
	if !won {
		return false, nil
	}
	task.Quota = actualQuota
	syncDeferredTaskFundingCache(task, delta, tokenKey)
	return true, nil
}

func applyDeferredTaskFunding(tx *gorm.DB, task *Task, actualQuota, delta int) (string, error) {
	userChanges := map[string]any{}
	if task.PrivateData.BillingSource == "subscription" {
		var sub UserSubscription
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ?", task.PrivateData.SubscriptionId, task.UserId).First(&sub).Error; err != nil {
			return "", err
		}
		used := max(int64(0), sub.AmountUsed+int64(delta))
		if sub.AmountTotal > 0 && used > sub.AmountTotal {
			return "", errors.New("subscription used exceeds total")
		}
		if err := tx.Model(&sub).Update("amount_used", used).Error; err != nil {
			return "", err
		}
	} else {
		userChanges["quota"] = gorm.Expr("quota - ?", delta)
	}
	if task.Status == TaskStatusSuccess {
		userChanges["used_quota"] = gorm.Expr("used_quota + ?", actualQuota)
		userChanges["request_count"] = gorm.Expr("request_count + ?", 1)
		if err := tx.Model(&Channel{}).Where("id = ?", task.ChannelId).
			Update("used_quota", gorm.Expr("used_quota + ?", actualQuota)).Error; err != nil {
			return "", err
		}
	}
	if len(userChanges) > 0 {
		result := tx.Model(&User{}).Where("id = ?", task.UserId).Updates(userChanges)
		if result.Error != nil {
			return "", result.Error
		}
		// The user must still exist, even when a zero adjustment changes no rows.
		var count int64
		if err := tx.Model(&User{}).Where("id = ?", task.UserId).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return "", gorm.ErrRecordNotFound
		}
	}
	if task.PrivateData.TokenId > 0 && delta != 0 {
		var token Token
		if err := tx.First(&token, task.PrivateData.TokenId).Error; err != nil {
			// A deleted token does not prevent returning the user's funds.
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return "", nil
			}
			return "", err
		}
		tokenKey := token.Key
		err := tx.Model(&token).Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota - ?", delta),
			"used_quota":    gorm.Expr("used_quota + ?", delta),
			"accessed_time": common.GetTimestamp(),
		}).Error
		return tokenKey, err
	}
	return "", nil
}

func syncDeferredTaskFundingCache(task *Task, delta int, tokenKey string) {
	// Apply the committed delta to caches as well, preserving any outstanding
	// reservations in the batch updater instead of reloading an older DB balance.
	if task.PrivateData.BillingSource != "subscription" && delta != 0 {
		if err := cacheIncrUserQuota(task.UserId, -int64(delta)); err != nil {
			common.SysError(fmt.Sprintf("deferred task user cache update: %v", err))
			_ = InvalidateUserCache(task.UserId)
		}
	}
	if common.RedisEnabled && tokenKey != "" {
		if err := cacheIncrTokenQuota(tokenKey, -int64(delta)); err != nil {
			common.SysError(fmt.Sprintf("deferred task token cache update: %v", err))
			_ = cacheDeleteToken(tokenKey)
		}
	}
}
