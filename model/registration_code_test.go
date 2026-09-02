package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRegistrationCodeTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := DB
	oldLogDB := LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(16)

	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()

	require.NoError(t, db.AutoMigrate(&User{}, &RegistrationCode{}, &RegistrationCodeUsage{}))

	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB = oldDB
		LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
		initCol()
	})

	return db
}

func createRegistrationCodeTestUser(t *testing.T, db *gorm.DB, username string) User {
	t.Helper()
	affCode, err := common.GenerateInviteCode()
	require.NoError(t, err)
	user := User{
		Username:    username,
		Password:    "password",
		DisplayName: username,
		Status:      common.UserStatusEnabled,
		Role:        common.RoleCommonUser,
		AffCode:     affCode,
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func createRegistrationCodeTestCode(t *testing.T, db *gorm.DB, code string, maxUses int, openTime int64, endTime ...int64) RegistrationCode {
	t.Helper()
	codeEndTime := int64(0)
	if len(endTime) > 0 {
		codeEndTime = endTime[0]
	}
	registrationCode := RegistrationCode{
		Code:        code,
		Status:      common.RegistrationCodeStatusEnabled,
		Name:        "test",
		MaxUses:     maxUses,
		OpenTime:    openTime,
		EndTime:     codeEndTime,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, db.Create(&registrationCode).Error)
	return registrationCode
}

func countRegistrationCodeUsages(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&RegistrationCodeUsage{}).Count(&count).Error)
	return count
}

func TestConsumeRegistrationCodeRequiredAndOptional(t *testing.T) {
	db := setupRegistrationCodeTestDB(t)
	user := createRegistrationCodeTestUser(t, db, "required-user")

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeRegistrationCodeTx(tx, "", user.Id, user.Username, "password", false)
	}))
	require.Equal(t, int64(0), countRegistrationCodeUsages(t, db))

	err := DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeRegistrationCodeTx(tx, "", user.Id, user.Username, "password", true)
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "请输入注册码")
}

func TestResolveInviterIdByAffCodeRequiredAndOptional(t *testing.T) {
	db := setupRegistrationCodeTestDB(t)
	inviter := createRegistrationCodeTestUser(t, db, "inviter")

	inviterId, err := ResolveInviterIdByAffCode("  "+inviter.AffCode+"  ", true)
	require.NoError(t, err)
	require.Equal(t, inviter.Id, inviterId)
	inviterId, err = ResolveInviterIdByAffCode(strings.ToLower(inviter.AffCode), true)
	require.NoError(t, err)
	require.Equal(t, inviter.Id, inviterId)

	inviterId, err = ResolveInviterIdByAffCode("", false)
	require.NoError(t, err)
	require.Zero(t, inviterId)

	inviterId, err = ResolveInviterIdByAffCode("UNKNOWN", false)
	require.NoError(t, err)
	require.Zero(t, inviterId)

	_, err = ResolveInviterIdByAffCode("", true)
	require.EqualError(t, err, "请输入邀请码")

	_, err = ResolveInviterIdByAffCode("UNKNOWN", true)
	require.EqualError(t, err, "邀请码无效")

	require.NoError(t, db.Model(&inviter).Update("status", common.UserStatusDisabled).Error)
	_, err = ResolveInviterIdByAffCode(inviter.AffCode, true)
	require.EqualError(t, err, "邀请码无效")

	inviterId, err = ResolveInviterIdByAffCode(inviter.AffCode, false)
	require.NoError(t, err)
	require.Zero(t, inviterId)

	require.NoError(t, db.Delete(&inviter).Error)
	_, err = ResolveInviterIdByAffCode(inviter.AffCode, true)
	require.EqualError(t, err, "邀请码无效")
}

func TestConsumeRegistrationCodeOpenTimeAndMaxUses(t *testing.T) {
	db := setupRegistrationCodeTestDB(t)
	now := common.GetTimestamp()
	user := createRegistrationCodeTestUser(t, db, "open-user")
	futureCode := createRegistrationCodeTestCode(t, db, "FUTURE", 1, now+3600)

	err := DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeRegistrationCodeTx(tx, futureCode.Code, user.Id, user.Username, "password", true)
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "尚未")

	expiredCode := createRegistrationCodeTestCode(t, db, "EXPIRED", 1, 0, now-1)
	err = DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeRegistrationCodeTx(tx, expiredCode.Code, user.Id, user.Username, "password", true)
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "过期")

	require.NoError(t, db.Model(&RegistrationCode{}).Where("id = ?", futureCode.Id).Update("open_time", now-1).Error)
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeRegistrationCodeTx(tx, futureCode.Code, user.Id, user.Username, "password", true)
	}))

	secondUser := createRegistrationCodeTestUser(t, db, "second-user")
	err = DB.Transaction(func(tx *gorm.DB) error {
		return ConsumeRegistrationCodeTx(tx, futureCode.Code, secondUser.Id, secondUser.Username, "password", true)
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "上限")

	var reloaded RegistrationCode
	require.NoError(t, db.First(&reloaded, futureCode.Id).Error)
	require.Equal(t, 1, reloaded.UsedCount)
	require.Equal(t, int64(1), countRegistrationCodeUsages(t, db))
}

func TestConsumeRegistrationCodeRollsBackWithTransaction(t *testing.T) {
	db := setupRegistrationCodeTestDB(t)
	user := createRegistrationCodeTestUser(t, db, "rollback-user")
	code := createRegistrationCodeTestCode(t, db, "ROLLBACK", 1, 0)
	expected := errors.New("simulate user creation failure")

	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := ConsumeRegistrationCodeTx(tx, code.Code, user.Id, user.Username, "password", true); err != nil {
			return err
		}
		return expected
	})
	require.ErrorIs(t, err, expected)

	var reloaded RegistrationCode
	require.NoError(t, db.First(&reloaded, code.Id).Error)
	require.Equal(t, 0, reloaded.UsedCount)
	require.Equal(t, int64(0), countRegistrationCodeUsages(t, db))
}

func TestConcurrentConsumeRegistrationCodeDoesNotExceedMaxUses(t *testing.T) {
	db := setupRegistrationCodeTestDB(t)
	code := createRegistrationCodeTestCode(t, db, "CONCURRENT", 3, 0)
	var successes int64
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			err := DB.Transaction(func(tx *gorm.DB) error {
				user := User{
					Username:    fmt.Sprintf("concurrent-%d", index),
					Password:    "password",
					DisplayName: fmt.Sprintf("concurrent-%d", index),
					Status:      common.UserStatusEnabled,
					Role:        common.RoleCommonUser,
					AffCode:     fmt.Sprintf("concurrent-%d-aff", index),
				}
				if err := tx.Create(&user).Error; err != nil {
					return err
				}
				return ConsumeRegistrationCodeTx(tx, code.Code, user.Id, user.Username, "password", true)
			})
			if err == nil {
				atomic.AddInt64(&successes, 1)
			}
		}(i)
	}
	wg.Wait()

	var reloaded RegistrationCode
	require.NoError(t, db.First(&reloaded, code.Id).Error)
	require.LessOrEqual(t, successes, int64(code.MaxUses))
	require.Equal(t, int(successes), reloaded.UsedCount)
	require.Equal(t, successes, countRegistrationCodeUsages(t, db))
}
