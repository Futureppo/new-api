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

func TestGetUserInviteRelationsDepthOneReturnsOnlyDirectRelations(t *testing.T) {
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

	relations, err := GetUserInviteRelations(target.Id, 1, 9999, common.RoleAdminUser)
	require.NoError(t, err)
	require.Equal(t, 1, relations.QueryDepth)
	require.Equal(t, target.Id, relations.Target.Id)
	require.Equal(t, 0, relations.Target.Depth)
	require.Equal(t, InviteRelationTypeTarget, relations.Target.RelationType)
	require.True(t, relations.Target.Selectable)
	require.NotNil(t, relations.Inviter)
	require.Equal(t, inviter.Id, relations.Inviter.Id)
	require.Equal(t, 1, relations.Inviter.Depth)
	require.Equal(t, InviteRelationTypeInviter, relations.Inviter.RelationType)
	require.True(t, relations.Inviter.Selectable)
	require.Len(t, relations.RelatedUsers, 5)

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

func TestGetUserInviteRelationsDepthTwoExpandsBidirectionally(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	greatGrandparent := seedUserInviteDisableTestUser(t, db, "depth-great-grandparent", common.RoleCommonUser, common.UserStatusEnabled, 0)
	grandparent := seedUserInviteDisableTestUser(t, db, "depth-grandparent", common.RoleCommonUser, common.UserStatusEnabled, greatGrandparent.Id)
	inviter := seedUserInviteDisableTestUser(t, db, "depth-inviter", common.RoleCommonUser, common.UserStatusEnabled, grandparent.Id)
	target := seedUserInviteDisableTestUser(t, db, "depth-target", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	sibling := seedUserInviteDisableTestUser(t, db, "depth-sibling", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	invitee := seedUserInviteDisableTestUser(t, db, "depth-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	grandchild := seedUserInviteDisableTestUser(t, db, "depth-grandchild", common.RoleCommonUser, common.UserStatusEnabled, invitee.Id)
	siblingChild := seedUserInviteDisableTestUser(t, db, "depth-sibling-child", common.RoleCommonUser, common.UserStatusEnabled, sibling.Id)

	relations, err := GetUserInviteRelations(target.Id, 2, 9999, common.RoleRootUser)
	require.NoError(t, err)
	require.Equal(t, 2, relations.QueryDepth)
	require.NotNil(t, relations.Inviter)
	require.Equal(t, inviter.Id, relations.Inviter.Id)
	require.Len(t, relations.Invitees, 1)
	require.Equal(t, invitee.Id, relations.Invitees[0].Id)

	relatedById := make(map[int]InviteRelationUser, len(relations.RelatedUsers))
	for _, item := range relations.RelatedUsers {
		relatedById[item.Id] = item
	}
	require.Len(t, relatedById, 5)
	require.Equal(t, 1, relatedById[inviter.Id].Depth)
	require.Equal(t, InviteRelationTypeInviter, relatedById[inviter.Id].RelationType)
	require.Equal(t, 1, relatedById[invitee.Id].Depth)
	require.Equal(t, InviteRelationTypeInvitee, relatedById[invitee.Id].RelationType)
	for _, userId := range []int{grandparent.Id, sibling.Id, grandchild.Id} {
		require.Equal(t, 2, relatedById[userId].Depth)
		require.Equal(t, InviteRelationTypeRelated, relatedById[userId].RelationType)
	}
	require.NotContains(t, relatedById, greatGrandparent.Id)
	require.NotContains(t, relatedById, siblingChild.Id)
}

func TestGetUserInviteRelationsUnlimitedTerminatesOnCycle(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	first := seedUserInviteDisableTestUser(t, db, "cycle-first", common.RoleCommonUser, common.UserStatusEnabled, 0)
	target := seedUserInviteDisableTestUser(t, db, "cycle-target", common.RoleCommonUser, common.UserStatusEnabled, first.Id)
	third := seedUserInviteDisableTestUser(t, db, "cycle-third", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	fourth := seedUserInviteDisableTestUser(t, db, "cycle-fourth", common.RoleCommonUser, common.UserStatusEnabled, third.Id)
	branch := seedUserInviteDisableTestUser(t, db, "cycle-branch", common.RoleCommonUser, common.UserStatusEnabled, fourth.Id)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", first.Id).Update("inviter_id", fourth.Id).Error)

	relations, err := GetUserInviteRelations(target.Id, 0, 9999, common.RoleRootUser)
	require.NoError(t, err)
	require.Equal(t, 0, relations.QueryDepth)

	relatedById := make(map[int]InviteRelationUser, len(relations.RelatedUsers))
	for _, item := range relations.RelatedUsers {
		require.NotEqual(t, target.Id, item.Id)
		_, duplicate := relatedById[item.Id]
		require.False(t, duplicate)
		relatedById[item.Id] = item
	}
	require.Len(t, relatedById, 4)
	require.Equal(t, 1, relatedById[first.Id].Depth)
	require.Equal(t, 1, relatedById[third.Id].Depth)
	require.Equal(t, 2, relatedById[fourth.Id].Depth)
	require.Equal(t, 3, relatedById[branch.Id].Depth)
}

func TestGetUserInviteRelationsTraversesThroughUnavailableUser(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	target := seedUserInviteDisableTestUser(t, db, "unavailable-target", common.RoleCommonUser, common.UserStatusEnabled, 0)
	disabledInvitee := seedUserInviteDisableTestUser(t, db, "unavailable-disabled", common.RoleCommonUser, common.UserStatusDisabled, target.Id)
	grandchild := seedUserInviteDisableTestUser(t, db, "unavailable-grandchild", common.RoleCommonUser, common.UserStatusEnabled, disabledInvitee.Id)

	relations, err := GetUserInviteRelations(target.Id, 2, 9999, common.RoleRootUser)
	require.NoError(t, err)
	relatedById := make(map[int]InviteRelationUser, len(relations.RelatedUsers))
	for _, item := range relations.RelatedUsers {
		relatedById[item.Id] = item
	}
	require.False(t, relatedById[disabledInvitee.Id].Selectable)
	require.Equal(t, UserDisableUnavailableAlreadyDisabled, relatedById[disabledInvitee.Id].UnavailableReason)
	require.True(t, relatedById[grandchild.Id].Selectable)
	require.Equal(t, 2, relatedById[grandchild.Id].Depth)
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
		1,
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

func TestBatchDisableRelatedUsersAllowsMultiLevelSelection(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	inviter := seedUserInviteDisableTestUser(t, db, "multi-batch-inviter", common.RoleCommonUser, common.UserStatusEnabled, 0)
	target := seedUserInviteDisableTestUser(t, db, "multi-batch-target", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	sibling := seedUserInviteDisableTestUser(t, db, "multi-batch-sibling", common.RoleCommonUser, common.UserStatusEnabled, inviter.Id)
	invitee := seedUserInviteDisableTestUser(t, db, "multi-batch-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	grandchild := seedUserInviteDisableTestUser(t, db, "multi-batch-grandchild", common.RoleCommonUser, common.UserStatusEnabled, invitee.Id)

	result, err := BatchDisableRelatedUsers(
		target.Id,
		[]int{sibling.Id, grandchild.Id},
		"multi-level relation",
		2,
		9999,
		common.RoleRootUser,
	)
	require.NoError(t, err)
	require.Equal(t, []int{target.Id, sibling.Id, grandchild.Id}, result.DisabledIds)

	for _, userId := range result.DisabledIds {
		var user model.User
		require.NoError(t, db.First(&user, userId).Error)
		require.Equal(t, common.UserStatusDisabled, user.Status)
	}
	for _, userId := range []int{inviter.Id, invitee.Id} {
		var user model.User
		require.NoError(t, db.First(&user, userId).Error)
		require.Equal(t, common.UserStatusEnabled, user.Status)
	}
}

func TestBatchDisableRelatedUsersRejectsSelectionBeyondDepthWithoutPartialUpdate(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	target := seedUserInviteDisableTestUser(t, db, "depth-rollback-target", common.RoleCommonUser, common.UserStatusEnabled, 0)
	invitee := seedUserInviteDisableTestUser(t, db, "depth-rollback-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	grandchild := seedUserInviteDisableTestUser(t, db, "depth-rollback-grandchild", common.RoleCommonUser, common.UserStatusEnabled, invitee.Id)
	greatGrandchild := seedUserInviteDisableTestUser(t, db, "depth-rollback-great-grandchild", common.RoleCommonUser, common.UserStatusEnabled, grandchild.Id)

	_, err := BatchDisableRelatedUsers(
		target.Id,
		[]int{grandchild.Id, greatGrandchild.Id},
		"beyond depth",
		2,
		9999,
		common.RoleRootUser,
	)
	require.Error(t, err)

	for _, userId := range []int{target.Id, invitee.Id, grandchild.Id, greatGrandchild.Id} {
		var user model.User
		require.NoError(t, db.First(&user, userId).Error)
		require.Equal(t, common.UserStatusEnabled, user.Status)
	}
}

func TestBatchDisableRelatedUsersUnlimitedDepth(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)

	target := seedUserInviteDisableTestUser(t, db, "unlimited-batch-target", common.RoleCommonUser, common.UserStatusEnabled, 0)
	invitee := seedUserInviteDisableTestUser(t, db, "unlimited-batch-invitee", common.RoleCommonUser, common.UserStatusEnabled, target.Id)
	grandchild := seedUserInviteDisableTestUser(t, db, "unlimited-batch-grandchild", common.RoleCommonUser, common.UserStatusEnabled, invitee.Id)
	greatGrandchild := seedUserInviteDisableTestUser(t, db, "unlimited-batch-great-grandchild", common.RoleCommonUser, common.UserStatusEnabled, grandchild.Id)

	result, err := BatchDisableRelatedUsers(
		target.Id,
		[]int{greatGrandchild.Id},
		"unlimited relation",
		0,
		9999,
		common.RoleRootUser,
	)
	require.NoError(t, err)
	require.Equal(t, []int{target.Id, greatGrandchild.Id}, result.DisabledIds)
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
		1,
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
			1,
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
			1,
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

func TestNormalizeUserInviteRelationDepth(t *testing.T) {
	depth, err := NormalizeUserInviteRelationDepth(nil)
	require.NoError(t, err)
	require.Equal(t, DefaultUserInviteRelationDepth, depth)

	unlimited := 0
	depth, err = NormalizeUserInviteRelationDepth(&unlimited)
	require.NoError(t, err)
	require.Zero(t, depth)

	positive := 5
	depth, err = NormalizeUserInviteRelationDepth(&positive)
	require.NoError(t, err)
	require.Equal(t, positive, depth)

	negative := -1
	_, err = NormalizeUserInviteRelationDepth(&negative)
	require.Error(t, err)
}
