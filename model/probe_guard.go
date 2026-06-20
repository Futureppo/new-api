package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ProbeAbuseState struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id" gorm:"uniqueIndex;not null"`
	OffenseCount  int    `json:"offense_count" gorm:"not null;default:0"`
	LastOffenseAt int64  `json:"last_offense_at" gorm:"bigint;index;default:0"`
	LastIPs       string `json:"last_ips" gorm:"type:text"`
	LastModels    string `json:"last_models" gorm:"type:text"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

func GetProbeAbuseState(userId int) (*ProbeAbuseState, error) {
	if userId <= 0 {
		return nil, errors.New("user id is invalid")
	}
	state := &ProbeAbuseState{}
	err := DB.Where("user_id = ?", userId).First(state).Error
	return state, err
}

func IncrementProbeAbuseOffense(userId int, ips []string, models []string) (*ProbeAbuseState, error) {
	if userId <= 0 {
		return nil, errors.New("user id is invalid")
	}
	now := common.GetTimestamp()
	ipsJSON, err := marshalProbeGuardStrings(ips)
	if err != nil {
		return nil, err
	}
	modelsJSON, err := marshalProbeGuardStrings(models)
	if err != nil {
		return nil, err
	}

	var out ProbeAbuseState
	err = DB.Transaction(func(tx *gorm.DB) error {
		state := ProbeAbuseState{}
		err := tx.Where("user_id = ?", userId).First(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state = ProbeAbuseState{
				UserId:        userId,
				OffenseCount:  1,
				LastOffenseAt: now,
				LastIPs:       ipsJSON,
				LastModels:    modelsJSON,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := tx.Create(&state).Error; err != nil {
				return err
			}
			out = state
			return nil
		}
		if err != nil {
			return err
		}
		state.OffenseCount++
		state.LastOffenseAt = now
		state.LastIPs = ipsJSON
		state.LastModels = modelsJSON
		state.UpdatedAt = now
		if err := tx.Model(&state).Select("offense_count", "last_offense_at", "last_ips", "last_models", "updated_at").Updates(&state).Error; err != nil {
			return err
		}
		out = state
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func UpsertProbeGuardIPBan(target string, reason string, expiresAt int64) error {
	normalized, err := NormalizeIPBanTarget(target)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "bulk probe guard"
	}

	var ban IPBan
	err = DB.Where("target = ?", normalized).First(&ban).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CreateIPBan(&IPBan{
			Target:      normalized,
			Reason:      reason,
			ExpiresAt:   expiresAt,
			AutoBanUser: false,
			CreatedBy:   0,
		})
	}
	if err != nil {
		return err
	}

	if ban.ExpiresAt == 0 {
		return nil
	}
	if expiresAt != 0 && expiresAt <= ban.ExpiresAt {
		return nil
	}
	ban.Reason = reason
	ban.ExpiresAt = expiresAt
	ban.AutoBanUser = false
	ban.UpdatedAt = now
	return UpdateIPBan(&ban)
}

func marshalProbeGuardStrings(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	bytes, err := common.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
