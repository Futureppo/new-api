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
	PendingBatch  string `json:"-" gorm:"type:text"`
}

type ProbeUserPenaltyResult struct {
	User             User
	State            ProbeUserAbuseState
	Applied          bool
	Ignored          bool
	ThresholdReached bool
	MatchedRequests  int
}

type ProbeUserBatchPolicy struct {
	WindowSeconds int
	RequestCount  int
	DryRun        bool
}

// Every matched request contributes one timestamp, including repeated models.
// At most 999 timestamps survive a write, keeping this compatible with MySQL TEXT.
type probeUserBatch struct {
	WindowSeconds int     `json:"window_seconds"`
	RequestCount  int     `json:"request_count"`
	DryRun        bool    `json:"dry_run"`
	Requests      []int64 `json:"requests"`
}

// The first statement takes the account's write lock in all three supported DBs.
// Matching, window updates and any resulting penalty commit together, across nodes.
func RecordProbeUserMatch(userId int, requestStarted time.Time, ruleID, requestID string, batch ProbeUserBatchPolicy, firstMinutes, secondMinutes int64, reason func(int, UserDisablePeriod) string) (*ProbeUserPenaltyResult, error) {
	return recordProbeUserMatchAt(userId, requestStarted, ruleID, requestID, batch, firstMinutes, secondMinutes, reason, time.Time{})
}

func recordProbeUserMatchAt(userId int, requestStarted time.Time, ruleID, requestID string, batch ProbeUserBatchPolicy, firstMinutes, secondMinutes int64, reason func(int, UserDisablePeriod) string, now time.Time) (*ProbeUserPenaltyResult, error) {
	if batch.WindowSeconds < 1 || batch.WindowSeconds > 3600 || batch.RequestCount < 1 || batch.RequestCount > 1000 {
		return nil, errors.New("invalid probe batch policy")
	}
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
			result.Ignored = true
			return nil
		}
		if now.IsZero() {
			now = time.Now()
		}
		recent := probeUserBatch{}
		if result.State.PendingBatch != "" {
			if err := common.UnmarshalJsonStr(result.State.PendingBatch, &recent); err != nil {
				return err
			}
		}
		// Changing thresholds or leaving dry-run starts a fresh batch.
		if recent.WindowSeconds != batch.WindowSeconds || recent.RequestCount != batch.RequestCount || recent.DryRun != batch.DryRun {
			recent = probeUserBatch{WindowSeconds: batch.WindowSeconds, RequestCount: batch.RequestCount, DryRun: batch.DryRun}
		}
		cutoff := now.Add(-time.Duration(batch.WindowSeconds) * time.Second).UnixNano()
		active := recent.Requests[:0]
		for _, seen := range recent.Requests {
			if seen > cutoff {
				active = append(active, seen)
			}
		}
		recent.Requests = active
		seen := now.UnixNano()
		if !requestStarted.IsZero() && requestStarted.Before(now) {
			seen = requestStarted.UnixNano()
		}
		if seen > cutoff {
			recent.Requests = append(recent.Requests, seen)
		}
		result.MatchedRequests = len(recent.Requests)
		result.State.UserId = userId
		if result.MatchedRequests < batch.RequestCount {
			data, err := common.Marshal(recent)
			if err != nil {
				return err
			}
			result.State.PendingBatch = string(data)
			return tx.Save(&result.State).Error
		}
		result.ThresholdReached = true
		result.State.PendingBatch = ""
		result.State.LastEventAt = now.UnixNano()
		if batch.DryRun {
			return tx.Save(&result.State).Error
		}
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
		state.PendingBatch = ""
		state.LastRuleID, state.LastRequestID, state.LastBanReason = "", "", ""
		return tx.Save(&state).Error
	})
}
