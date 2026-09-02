package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	inviteCodeGenerationMaxAttempts = 128
	inviteCodeV2MigrationKey        = "migration.invite_code_v2_16"
)

type inviteCodeGenerator func() (string, error)

func GenerateUniqueInviteCode(db *gorm.DB) (string, error) {
	return generateUniqueInviteCodeWithGenerator(db, common.GenerateInviteCode)
}

func generateUniqueInviteCodeWithGenerator(db *gorm.DB, generate inviteCodeGenerator) (string, error) {
	if db == nil {
		return "", errors.New("database is not initialized")
	}
	for attempt := 0; attempt < inviteCodeGenerationMaxAttempts; attempt++ {
		code, err := generate()
		if err != nil {
			return "", fmt.Errorf("failed to generate invite code: %w", err)
		}
		if !common.IsValidInviteCode(code) {
			return "", errors.New("invite code generator returned an invalid code")
		}
		var count int64
		if err := db.Unscoped().Model(&User{}).Where("aff_code = ?", code).Count(&count).Error; err != nil {
			return "", fmt.Errorf("failed to check invite code uniqueness: %w", err)
		}
		if count == 0 {
			return code, nil
		}
	}
	return "", errors.New("failed to generate a unique invite code")
}

// EnsureUserInviteCode replaces an empty or legacy invite code with a v2 code.
func EnsureUserInviteCode(user *User) (string, error) {
	if user == nil || user.Id <= 0 {
		return "", errors.New("user is invalid")
	}
	if common.IsValidInviteCode(user.AffCode) {
		return user.AffCode, nil
	}
	code, err := GenerateUniqueInviteCode(DB)
	if err != nil {
		return "", err
	}
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Update("aff_code", code).Error; err != nil {
		return "", err
	}
	user.AffCode = code
	return code, nil
}

func migrateInviteCodesV2(db *gorm.DB) error {
	return migrateInviteCodesV2WithGenerator(db, common.GenerateInviteCode)
}

func migrateInviteCodesV2WithGenerator(db *gorm.DB, generate inviteCodeGenerator) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var marker Option
		err := tx.Take(&marker, &Option{Key: inviteCodeV2MigrationKey}).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("failed to check invite code migration state: %w", err)
		}

		var users []User
		if err := tx.Unscoped().Select("id", "aff_code").Order("id ASC").Find(&users).Error; err != nil {
			return fmt.Errorf("failed to load users for invite code migration: %w", err)
		}

		occupiedCodes := make(map[string]struct{}, len(users)*2)
		for _, user := range users {
			occupiedCodes[strings.ToUpper(user.AffCode)] = struct{}{}
		}

		for _, user := range users {
			var code string
			for attempt := 0; attempt < inviteCodeGenerationMaxAttempts; attempt++ {
				candidate, err := generate()
				if err != nil {
					return fmt.Errorf("failed to generate invite code for user %d: %w", user.Id, err)
				}
				if !common.IsValidInviteCode(candidate) {
					return fmt.Errorf("invite code generator returned an invalid code for user %d", user.Id)
				}
				if _, exists := occupiedCodes[candidate]; exists {
					continue
				}
				code = candidate
				break
			}
			if code == "" {
				return fmt.Errorf("failed to generate a unique invite code for user %d", user.Id)
			}
			if err := tx.Unscoped().Model(&User{}).Where("id = ?", user.Id).Update("aff_code", code).Error; err != nil {
				return fmt.Errorf("failed to update invite code for user %d: %w", user.Id, err)
			}
			occupiedCodes[code] = struct{}{}
		}

		marker = Option{Key: inviteCodeV2MigrationKey, Value: "completed"}
		if err := tx.Create(&marker).Error; err != nil {
			return fmt.Errorf("failed to save invite code migration state: %w", err)
		}
		return nil
	})
}
