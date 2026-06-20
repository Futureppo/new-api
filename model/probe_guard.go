package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ProbeIPAbuseState struct {
	Id            int    `json:"id"`
	TargetIP      string `json:"target_ip" gorm:"type:varchar(128);uniqueIndex;not null"`
	LastUserId    int    `json:"last_user_id" gorm:"index;default:0"`
	OffenseCount  int    `json:"offense_count" gorm:"not null;default:0"`
	LastOffenseAt int64  `json:"last_offense_at" gorm:"bigint;index;default:0"`
	LastModels    string `json:"last_models" gorm:"type:text"`
	CreatedAt     int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt     int64  `json:"updated_at" gorm:"bigint"`
}

func GetProbeIPAbuseState(targetIP string) (*ProbeIPAbuseState, error) {
	targetIP = strings.TrimSpace(targetIP)
	if targetIP == "" {
		return nil, errors.New("target ip is invalid")
	}
	state := &ProbeIPAbuseState{}
	err := DB.Where("target_ip = ?", targetIP).First(state).Error
	return state, err
}

func IncrementProbeIPAbuseOffense(targetIP string, lastUserId int, models []string) (*ProbeIPAbuseState, error) {
	targetIP = strings.TrimSpace(targetIP)
	if targetIP == "" {
		return nil, errors.New("target ip is invalid")
	}
	now := common.GetTimestamp()
	modelsJSON, err := marshalProbeGuardStrings(models)
	if err != nil {
		return nil, err
	}

	var out ProbeIPAbuseState
	err = DB.Transaction(func(tx *gorm.DB) error {
		state := ProbeIPAbuseState{}
		err := tx.Where("target_ip = ?", targetIP).First(&state).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			state = ProbeIPAbuseState{
				TargetIP:      targetIP,
				LastUserId:    lastUserId,
				OffenseCount:  1,
				LastOffenseAt: now,
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
		state.LastUserId = lastUserId
		state.LastOffenseAt = now
		state.LastModels = modelsJSON
		state.UpdatedAt = now
		if err := tx.Model(&state).Select("last_user_id", "offense_count", "last_offense_at", "last_models", "updated_at").Updates(&state).Error; err != nil {
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
