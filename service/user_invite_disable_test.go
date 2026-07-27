package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserInviteDisableTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})

	return db
}

func seedUserInviteDisableTestUser(
	t *testing.T,
	db *gorm.DB,
	username string,
	role int,
	status int,
	inviterId int,
) model.User {
	t.Helper()
	user := model.User{
		Username:    username,
		Password:    "password",
		DisplayName: username + "-display",
		Role:        role,
		Status:      status,
		Group:       "default",
		AffCode:     username + "-aff",
		InviterId:   inviterId,
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func TestGetUserInviteRelationsReturnsOnlyDirectRelations(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	inviter := seedUserInviteDisableTestUser(t, db, "relation-inviter", common.RoleCommonUser, common.UserStatusEnabled, 0)
	target := seedUserInviteDisableTestUser(t, db, "relation-target", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	invitee := seedUserInviteDisableTestUser(t, db, "relation-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	disabledInvitee := seedUserInviteDisableTestUser(t, db, "relation-disabled", common.RoleCommonUser, common.UserStatusDisabled, target.Id)
	deletedInvitee := seedUserInviteDisableTestUser(t, db, "relation-deleted", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	adminInvitee := seedUserInviteDisableTestUser(t, db, "relation-admin", common.RoleAdminUser, common.UserStatusEnabled, target.Id)
	sibling := seedUserInviteDisableTestUser(t, db, "relation-sibling", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	grandchild := seedUserInviteDisableTestUser(t, db, "relation-grandchild", common.RoleCommonUser, common.UserStatusEnabled, invitee.Id)
	require.NoError(t, db.Delete(&deletedInvitee).Error)

	relations, err := GetUserInviteRelations(target.Id, 9999, common.RoleAdminUser)
	require.NoError(t, err)
	require.Equal(t, target.Id, relations.Target.Id)
	require.True(t, relations.Target.Selectable)
	require.NotNil(t, relations.Inviter)
	require.Equal(t, inviter.Id, relations.Inviter.Id)
	require.True(t, relations.Inviter.Selectable)

	inviteesById := make(map[int]InviteRelationUser)
	for _, item := range relations.Invitees {
		inviteesById[item.Id] = item
	}
	require.Len(t, inviteesById, 4)
	require.Contains(t, inviteesById, invitee.Id)
	require.Contains(t, inviteesById, disabledInvitee.Id)
	require.Contains(t, inviteesById, deletedInvitee.Id)
	require.Contains(t, inviteesById, adminInvitee.Id)
	require.NotContains(t, inviteesById, sibling.Id)
	require.NotContains(t, inviteesById, grandchild.Id)

	require.False(t, inviteesById[disabledInvitee.Id].Selectable)
	require.Equal(t, UserDisableUnavailableAlreadyDisabled, inviteesById[disabledInvitee.Id].UnavailableReason)
	require.False(t, inviteesById[deletedInvitee.Id].Selectable)
	require.True(t, inviteesById[deletedInvitee.Id].Deleted)
	require.Equal(t, UserDisableUnavailableDeleted, inviteesById[deletedInvitee.Id].UnavailableReason)
	require.False(t, inviteesById[adminInvitee.Id].Selectable)
	require.Equal(t, UserDisableUnavailableInsufficientPermission, inviteesById[adminInvitee.Id].UnavailableReason)
}

func TestBatchDisableRelatedUsersDisablesSelectionAndSkipsDisabledUsers(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	inviter := seedUserInviteDisableTestUser(t, db, "batch-inviter", common.RoleCommonUser, common.UserStatusEnabled, 0)
	target := seedUserInviteDisableTestUser(t, db, "batch-target", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	invitee := seedUserInviteDisableTestUser(t, db, "batch-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	disabledInvitee := seedUserInviteDisableTestUser(t, db, "batch-disabled", common.RoleCommonUser, common.UserStatusDisabled, target.Id)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", disabledInvitee.Id).Update("disable_reason", "existing reason").Error)

	result, err := BatchDisableRelatedUsers(
		target.Id,
		[]int{inviter.Id, invitee.Id, invitee.Id, disabledInvitee.Id},
		"  linked abuse  ",
		9999,
		common.RoleRootUser,
	)
	require.NoError(t, err)
	require.Equal(t, []int{target.Id, inviter.Id, invitee.Id}, result.DisabledIds)
	require.Equal(t, []int{disabledInvitee.Id}, result.AlreadyDisabledIds)

	for _, userId := range result.DisabledIds {
		var user model.User
		require.NoError(t, db.First(&user, userId).Error)
		require.Equal(t, common.UserStatusDisabled, user.Status)
		require.Equal(t, "linked abuse", user.DisableReason)
	}
	var alreadyDisabled model.User
	require.NoError(t, db.First(&alreadyDisabled, disabledInvitee.Id).Error)
	require.Equal(t, "existing reason", alreadyDisabled.DisableReason)

	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).
		Where("type = ? AND user_id IN ?", model.LogTypeManage, result.DisabledIds).
		Count(&logCount).Error)
	require.Equal(t, int64(3), logCount)
}

func TestBatchDisableRelatedUsersRejectsUnrelatedUserWithoutPartialUpdate(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	target := seedUserInviteDisableTestUser(t, db, "rollback-target", common.RoleCommonUser, common.UserStatusEnabled, 0)
	invitee := seedUserInviteDisableTestUser(t, db, "rollback-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	unrelated := seedUserInviteDisableTestUser(t, db, "rollback-unrelated", common.RoleCommonUser, common.UserStatusEnabled, 0)

	_, err := BatchDisableRelatedUsers(
		target.Id,
		[]int{invitee.Id, unrelated.Id},
		"invalid relation",
		9999,
		common.RoleRootUser,
	)
	require.Error(t, err)

	for _, userId := range []int{target.Id, invitee.Id, unrelated.Id} {
		var user model.User
		require.NoError(t, db.First(&user, userId).Error)
		require.Equal(t, common.UserStatusEnabled, user.Status)
		require.Empty(t, user.DisableReason)
	}
}

func TestBatchDisableRelatedUsersRejectsProtectedOrDeletedSelection(t *testing.T) {
	t.Run("protected role", func(t *testing.T) {
		db := setupUserInviteDisableTestDB(t)
		target := seedUserInviteDisableTestUser(t, db, "protected-target", common.RoleCommonUser, common.UserStatusEnabled, 0)
		adminInvitee := seedUserInviteDisableTestUser(t, db, "protected-admin", common.RoleAdminUser, common.UserStatusEnabled, target.Id)

		_, err := BatchDisableRelatedUsers(
			target.Id,
			[]int{adminInvitee.Id},
			"protected relation",
			9999,
			common.RoleAdminUser,
		)
		require.Error(t, err)

		var currentTarget model.User
		require.NoError(t, db.First(&currentTarget, target.Id).Error)
		require.Equal(t, common.UserStatusEnabled, currentTarget.Status)
	})

	t.Run("deleted relation", func(t *testing.T) {
		db := setupUserInviteDisableTestDB(t)
		target := seedUserInviteDisableTestUser(t, db, "deleted-target", common.RoleCommonUser, common.UserStatusEnabled, 0)
		deletedInvitee := seedUserInviteDisableTestUser(t, db, "deleted-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
		require.NoError(t, db.Delete(&deletedInvitee).Error)

		_, err := BatchDisableRelatedUsers(
			target.Id,
			[]int{deletedInvitee.Id},
			"deleted relation",
			9999,
			common.RoleRootUser,
		)
		require.Error(t, err)

		var currentTarget model.User
		require.NoError(t, db.First(&currentTarget, target.Id).Error)
		require.Equal(t, common.UserStatusEnabled, currentTarget.Status)
	})
}

func TestBatchDisableRelatedUsersValidatesReason(t *testing.T) {
	_, err := normalizeBatchDisableReason(" ")
	require.Error(t, err)

	_, err = normalizeBatchDisableReason(strings.Repeat("a", 256))
	require.Error(t, err)

	reason, err := normalizeBatchDisableReason("  valid reason  ")
	require.NoError(t, err)
	require.Equal(t, "valid reason", reason)
}
