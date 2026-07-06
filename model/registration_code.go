package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type RegistrationCode struct {
	Id           int            `json:"id"`
	UserId       int            `json:"user_id" gorm:"index"`
	Code         string         `json:"code" gorm:"type:varchar(64);uniqueIndex"`
	Status       int            `json:"status" gorm:"default:1"`
	Name         string         `json:"name" gorm:"index"`
	MaxUses      int            `json:"max_uses" gorm:"default:1"`
	UsedCount    int            `json:"used_count" gorm:"default:0"`
	OpenTime     int64          `json:"open_time" gorm:"bigint"`
	EndTime      int64          `json:"end_time" gorm:"bigint"`
	CreatedTime  int64          `json:"created_time" gorm:"bigint"`
	LastUsedTime int64          `json:"last_used_time" gorm:"bigint"`
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

type RegistrationCodeUsage struct {
	Id                 int    `json:"id"`
	RegistrationCodeId int    `json:"registration_code_id" gorm:"index"`
	UserId             int    `json:"user_id" gorm:"index"`
	Username           string `json:"username" gorm:"index"`
	Source             string `json:"source" gorm:"index"`
	UsedTime           int64  `json:"used_time" gorm:"bigint"`
}

func ConsumeRegistrationCodeTx(tx *gorm.DB, code string, userId int, username string, source string, required bool) error {
	code = strings.TrimSpace(code)
	if code == "" {
		if required {
			return errors.New("请输入注册码")
		}
		return nil
	}
	if len([]rune(code)) > 64 {
		return errors.New("注册码无效")
	}
	if userId <= 0 {
		return errors.New("无效的用户")
	}
	now := common.GetTimestamp()
	var registrationCode RegistrationCode
	if err := tx.Where("code = ?", code).First(&registrationCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("注册码无效")
		}
		return err
	}
	if registrationCode.Status != common.RegistrationCodeStatusEnabled {
		return errors.New("注册码已禁用")
	}
	if registrationCode.OpenTime > now {
		return errors.New("注册码尚未到可用时间")
	}
	if registrationCode.EndTime != 0 && registrationCode.EndTime < now {
		return errors.New("注册码已过期")
	}
	if registrationCode.MaxUses <= 0 || registrationCode.UsedCount >= registrationCode.MaxUses {
		return errors.New("注册码使用次数已达上限")
	}

	result := tx.Model(&RegistrationCode{}).
		Where("id = ? AND status = ? AND open_time <= ? AND (end_time = 0 OR end_time >= ?) AND used_count < max_uses", registrationCode.Id, common.RegistrationCodeStatusEnabled, now, now).
		Updates(map[string]interface{}{
			"used_count":     gorm.Expr("used_count + ?", 1),
			"last_used_time": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("注册码使用次数已达上限")
	}

	usage := RegistrationCodeUsage{
		RegistrationCodeId: registrationCode.Id,
		UserId:             userId,
		Username:           username,
		Source:             source,
		UsedTime:           now,
	}
	return tx.Create(&usage).Error
}
