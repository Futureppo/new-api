package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// UserDisablePeriod is computed once per operation so every selected user has
// the same deadline. Unix seconds avoid database-specific datetime arithmetic.
type UserDisablePeriod struct {
	Minutes int64
	Until   int64
}

func NewUserDisablePeriod(minutes int64, now int64) (UserDisablePeriod, error) {
	// Keep deadlines representable as a four-digit calendar year in every UI.
	const maxDisableUntil int64 = 253402300799 // 9999-12-31T23:59:59Z
	if minutes < 0 || now < 0 || now > maxDisableUntil || minutes > (maxDisableUntil-now)/60 {
		return UserDisablePeriod{}, errors.New("duration_minutes must be a non-negative integer within the supported date range")
	}
	period := UserDisablePeriod{Minutes: minutes}
	if minutes > 0 {
		period.Until = now + minutes*60
	}
	return period, nil
}

func (period UserDisablePeriod) Updates(reason string) map[string]interface{} {
	return map[string]interface{}{
		"status":                   common.UserStatusDisabled,
		"disable_reason":           reason,
		"disable_duration_minutes": period.Minutes,
		"disable_until":            period.Until,
	}
}

func UserEnableUpdates() map[string]interface{} {
	return map[string]interface{}{
		"status":                   common.UserStatusEnabled,
		"disable_reason":           "",
		"disable_duration_minutes": int64(0),
		"disable_until":            int64(0),
	}
}

func InvalidateUserAuthCaches(userId int) {
	if err := InvalidateUserCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate user %d cache: %v", userId, err))
	}
	if err := InvalidateUserTokensCache(userId); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate user %d token caches: %v", userId, err))
	}
}

// ResolveUserDisableExpiry never trusts an expired cache entry to authorize a
// request. The conditional update cannot clear a newer ban, and the subsequent
// read returns the current database state even when another request won the race.
func ResolveUserDisableExpiry(user *User, now int64) error {
	if user == nil || user.DeletedAt.Valid || user.Status != common.UserStatusDisabled || user.DisableUntil <= 0 || user.DisableUntil > now {
		return nil
	}
	previous := *user
	result := DB.Model(&User{}).
		Where("id = ? AND status = ? AND disable_until = ? AND disable_until <= ?", user.Id, common.UserStatusDisabled, user.DisableUntil, now).
		Updates(UserEnableUpdates())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		InvalidateUserAuthCaches(user.Id)
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("用户定时封禁到期自动解禁，原原因：%s，封禁 %d 分钟，解禁时间：%s", previous.DisableReason, previous.DisableDurationMinutes, time.Unix(previous.DisableUntil, 0).Format(time.RFC3339)))
	}
	return DB.Omit("password").First(user, "id = ?", previous.Id).Error
}

// ExpireDueUserDisables also runs before status-filtered administration actions.
// Keyset pagination keeps concurrent changes from causing an endless first page.
func ExpireDueUserDisables(now int64) error {
	lastID := 0
	for {
		var users []User
		if err := DB.Where("id > ? AND status = ? AND disable_until > 0 AND disable_until <= ?", lastID, common.UserStatusDisabled, now).
			Order("id ASC").Limit(100).Find(&users).Error; err != nil {
			return err
		}
		for i := range users {
			lastID = users[i].Id
			if err := ResolveUserDisableExpiry(&users[i], now); err != nil {
				return err
			}
		}
		if len(users) < 100 {
			return nil
		}
	}
}
