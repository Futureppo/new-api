package model

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupInviteCodeMigrationTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := DB
	oldLogDB := LOG_DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Option{}))
	DB = db
	LOG_DB = db

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		DB = oldDB
		LOG_DB = oldLogDB
	})
	return db
}

func seedLegacyInviteCodeUser(t *testing.T, db *gorm.DB, username string, status int, affCode string, inviterId int) User {
	t.Helper()
	user := User{
		Username:    username,
		Password:    "password123",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      status,
		AffCode:     affCode,
		InviterId:   inviterId,
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func TestMigrateInviteCodesV2RotatesAllUsersOnce(t *testing.T) {
	db := setupInviteCodeMigrationTestDB(t)
	inviter := seedLegacyInviteCodeUser(t, db, "legacy-inviter", common.UserStatusEnabled, "A1B2", 0)
	disabled := seedLegacyInviteCodeUser(t, db, "legacy-disabled", common.UserStatusDisabled, "", inviter.Id)
	deleted := seedLegacyInviteCodeUser(t, db, "legacy-deleted", common.UserStatusEnabled, "C3D4", inviter.Id)
	formattedLegacy := seedLegacyInviteCodeUser(t, db, "legacy-formatted", common.UserStatusEnabled, "ZZZZZZZZZZZZZZZZ", inviter.Id)
	require.NoError(t, db.Delete(&deleted).Error)

	codes := []string{
		"ABCDEFGHJKLMNPQR",
		"BCDEFGHJKLMNPQRS",
		"CDEFGHJKLMNPQRST",
		"DEFGHJKLMNPQRSTU",
	}
	nextCode := 0
	require.NoError(t, migrateInviteCodesV2WithGenerator(db, func() (string, error) {
		code := codes[nextCode]
		nextCode++
		return code, nil
	}))

	var migrated []User
	require.NoError(t, db.Unscoped().Order("id ASC").Find(&migrated).Error)
	require.Len(t, migrated, 4)
	seen := make(map[string]struct{}, len(migrated))
	for _, user := range migrated {
		require.True(t, common.IsValidInviteCode(user.AffCode))
		_, exists := seen[user.AffCode]
		require.False(t, exists)
		seen[user.AffCode] = struct{}{}
	}
	require.Equal(t, 0, migrated[0].InviterId)
	require.Equal(t, inviter.Id, migrated[1].InviterId)
	require.Equal(t, inviter.Id, migrated[2].InviterId)
	require.Equal(t, inviter.Id, migrated[3].InviterId)
	require.Equal(t, common.UserStatusDisabled, migrated[1].Status)
	require.True(t, migrated[2].DeletedAt.Valid)
	require.NotEqual(t, formattedLegacy.AffCode, migrated[3].AffCode)

	for _, oldCode := range []string{"A1B2", "C3D4", "ZZZZZZZZZZZZZZZZ"} {
		var count int64
		require.NoError(t, db.Unscoped().Model(&User{}).Where("aff_code = ?", oldCode).Count(&count).Error)
		require.Zero(t, count)
	}
	var marker Option
	require.NoError(t, db.Where("key = ?", inviteCodeV2MigrationKey).First(&marker).Error)
	require.Equal(t, "completed", marker.Value)

	firstMigrationCodes := []string{migrated[0].AffCode, migrated[1].AffCode, migrated[2].AffCode, migrated[3].AffCode}
	require.NoError(t, migrateInviteCodesV2WithGenerator(db, func() (string, error) {
		return "", errors.New("generator must not run after migration")
	}))
	migrated = nil
	require.NoError(t, db.Unscoped().Order("id ASC").Find(&migrated).Error)
	require.Equal(t, firstMigrationCodes, []string{migrated[0].AffCode, migrated[1].AffCode, migrated[2].AffCode, migrated[3].AffCode})
	require.Equal(t, disabled.Id, migrated[1].Id)

	var inviteeCount int64
	require.NoError(t, db.Unscoped().Model(&User{}).Where("inviter_id = ?", inviter.Id).Count(&inviteeCount).Error)
	require.Equal(t, int64(3), inviteeCount)
}

func TestMigrateInviteCodesV2RollsBackOnFailure(t *testing.T) {
	db := setupInviteCodeMigrationTestDB(t)
	first := seedLegacyInviteCodeUser(t, db, "rollback-first", common.UserStatusEnabled, "R1A2", 0)
	second := seedLegacyInviteCodeUser(t, db, "rollback-second", common.UserStatusEnabled, "R3B4", first.Id)

	calls := 0
	err := migrateInviteCodesV2WithGenerator(db, func() (string, error) {
		calls++
		if calls == 1 {
			return "DEFGHJKLMNPQRSTU", nil
		}
		return "", errors.New("forced failure")
	})
	require.ErrorContains(t, err, "forced failure")

	var users []User
	require.NoError(t, db.Order("id ASC").Find(&users).Error)
	require.Equal(t, []string{first.AffCode, second.AffCode}, []string{users[0].AffCode, users[1].AffCode})
	var markerCount int64
	require.NoError(t, db.Model(&Option{}).Where("key = ?", inviteCodeV2MigrationKey).Count(&markerCount).Error)
	require.Zero(t, markerCount)
}

func TestEnsureUserInviteCodeRepairsLegacyCodeAndKeepsV2Code(t *testing.T) {
	db := setupInviteCodeMigrationTestDB(t)
	user := seedLegacyInviteCodeUser(t, db, "repair-legacy", common.UserStatusEnabled, "OLD1", 0)

	code, err := EnsureUserInviteCode(&user)
	require.NoError(t, err)
	require.True(t, common.IsValidInviteCode(code))
	require.NotEqual(t, "OLD1", code)

	sameCode, err := EnsureUserInviteCode(&user)
	require.NoError(t, err)
	require.Equal(t, code, sameCode)
	var stored User
	require.NoError(t, db.First(&stored, user.Id).Error)
	require.Equal(t, code, stored.AffCode)
}

func TestInsertWithTxGeneratesV2InviteCode(t *testing.T) {
	db := setupInviteCodeMigrationTestDB(t)
	user := User{
		Username:    "new-invite-code-user",
		Password:    "password123",
		DisplayName: "new-invite-code-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return user.InsertWithTx(tx, 0)
	}))
	require.True(t, common.IsValidInviteCode(user.AffCode))
}

func TestGenerateUniqueInviteCodeRetriesCollision(t *testing.T) {
	db := setupInviteCodeMigrationTestDB(t)
	existingCode := "ABCDEFGHJKLMNPQR"
	seedLegacyInviteCodeUser(t, db, "existing-code-user", common.UserStatusEnabled, existingCode, 0)

	candidates := []string{existingCode, "BCDEFGHJKLMNPQRS"}
	calls := 0
	code, err := generateUniqueInviteCodeWithGenerator(db, func() (string, error) {
		candidate := candidates[calls]
		calls++
		return candidate, nil
	})
	require.NoError(t, err)
	require.Equal(t, "BCDEFGHJKLMNPQRS", code)
	require.Equal(t, 2, calls)
}
