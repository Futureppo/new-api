package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Kept separate from ProbeIPAbuseState: legacy IP offenses cannot be attributed to accounts.
type ProbeUserAbuseState struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id" gorm:"uniqueIndex;not null"`
	OffenseCount  int    `json:"offense_count" gorm:"not null;default:0"`
	LastOffenseAt int64  `json:"last_offense_at" gorm:"bigint;not null;default:0"`
	LastEventAt   int64  `json:"-" gorm:"bigint;not null;default:0"`
	LastRuleID    string `json:"last_rule_id" gorm:"type:varchar(96)"`
	LastRequestID string `json:"last_request_id" gorm:"type:varchar(128)"`
	LastBanReason string `json:"last_ban_reason" gorm:"type:varchar(255)"`
}

type ProbeUserPenaltyResult struct {
	User    User
	State   ProbeUserAbuseState
	Applied bool
}

// The first statement takes the account's write lock in all three supported DBs.
// This no-op update never changes an administrative ban. Count and ban commit together.
func ApplyProbeUserPenalty(userId int, requestStarted time.Time, ruleID, requestID string, firstMinutes, secondMinutes int64, reason func(int, UserDisablePeriod) string) (*ProbeUserPenaltyResult, error) {
	result := &ProbeUserPenaltyResult{}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", userId).UpdateColumn("status", gorm.Expr("status")).Error; err != nil {
			return err
		}
		if err := tx.Omit("password").First(&result.User, "id = ?", userId).Error; err != nil {
			return err
		}
		if result.User.Role != common.RoleCommonUser || result.User.Status != common.UserStatusEnabled {
			return nil
		}
		err := tx.Where("user_id = ?", userId).First(&result.State).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		// Requests already in flight before a previous penalty/reset cannot escalate it.
		if !requestStarted.IsZero() && requestStarted.UnixNano() <= result.State.LastEventAt {
			return nil
		}
		now := time.Now()
		count := min(result.State.OffenseCount+1, 3)
		minutes := int64(0)
		if count == 1 {
			minutes = firstMinutes
		} else if count == 2 {
			minutes = secondMinutes
		}
		period, err := NewUserDisablePeriod(minutes, now.Unix())
		if err != nil {
			return err
		}
		message := reason(count, period)
		if err := tx.Model(&User{}).Where("id = ?", userId).Updates(period.Updates(message)).Error; err != nil {
			return err
		}
		result.User.Status = common.UserStatusDisabled
		result.User.DisableReason = message
		result.User.DisableDurationMinutes = period.Minutes
		result.User.DisableUntil = period.Until
		result.State.UserId = userId
		result.State.OffenseCount = count
		result.State.LastOffenseAt = now.Unix()
		result.State.LastEventAt = now.UnixNano()
		result.State.LastRuleID = ruleID
		result.State.LastRequestID = requestID
		result.State.LastBanReason = message
		if err := tx.Save(&result.State).Error; err != nil {
			return err
		}
		result.Applied = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func GetProbeUserAbuseState(userId int) (*ProbeUserAbuseState, error) {
	state := &ProbeUserAbuseState{UserId: userId}
	err := DB.Where("user_id = ?", userId).First(state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return state, err
}

// Resetting offenses never enables an account or changes its existing ban.
func ResetProbeUserAbuseState(userId int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", userId).UpdateColumn("status", gorm.Expr("status")).Error; err != nil {
			return err
		}
		var user User
		if err := tx.Select("id").First(&user, "id = ?", userId).Error; err != nil {
			return err
		}
		state := ProbeUserAbuseState{UserId: userId}
		if err := tx.Where("user_id = ?", userId).First(&state).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		state.OffenseCount = 0
		state.LastOffenseAt = 0
		state.LastEventAt = time.Now().UnixNano()
		state.LastRuleID, state.LastRequestID, state.LastBanReason = "", "", ""
		return tx.Save(&state).Error
	})
}
