package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	DefaultUserInviteRelationDepth = 2
	userDisableUpdateChunkSize     = 100
	userInviteQueryChunkSize       = 500
)

const (
	InviteRelationTypeTarget  = "target"
	InviteRelationTypeInviter = "inviter"
	InviteRelationTypeInvitee = "invitee"
	InviteRelationTypeRelated = "related"
)

const (
	UserDisableUnavailableDeleted                = "deleted"
	UserDisableUnavailableAlreadyDisabled        = "already_disabled"
	UserDisableUnavailableRootProtected          = "root_protected"
	UserDisableUnavailableOperatorSelf           = "operator_self"
	UserDisableUnavailableInsufficientPermission = "insufficient_permission"
	UserDisableUnavailableInvalidStatus          = "invalid_status"
)

type InviteRelationUser struct {
	Id                int    `json:"id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Role              int    `json:"role"`
	Status            int    `json:"status"`
	Deleted           bool   `json:"deleted"`
	DisableReason     string `json:"disable_reason,omitempty"`
	Selectable        bool   `json:"selectable"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
	Depth             int    `json:"depth"`
	RelationType      string `json:"relation_type"`
}

type UserInviteRelations struct {
	Target       InviteRelationUser   `json:"target"`
	Inviter      *InviteRelationUser  `json:"inviter"`
	Invitees     []InviteRelationUser `json:"invitees"`
	RelatedUsers []InviteRelationUser `json:"related_users"`
	QueryDepth   int                  `json:"query_depth"`
}

type BatchDisableRelatedUsersResult struct {
	TargetId           int   `json:"target_id"`
	DisabledIds        []int `json:"disabled_ids"`
	AlreadyDisabledIds []int `json:"already_disabled_ids"`
}

type userInviteRelationSnapshot struct {
	Target       model.User
	RelatedUsers []userInviteRelationMember
}

type userInviteRelationMember struct {
	User         model.User
	Depth        int
	RelationType string
}

func relationUserQuery(db *gorm.DB) *gorm.DB {
	return db.Unscoped().Select(
		"id",
		"username",
		"display_name",
		"role",
		"status",
		"disable_reason",
		"inviter_id",
		"deleted_at",
	)
}

func NormalizeUserInviteRelationDepth(depth *int) (int, error) {
	if depth == nil {
		return DefaultUserInviteRelationDepth, nil
	}
	if *depth < 0 {
		return 0, errors.New("invite relation depth cannot be negative")
	}
	return *depth, nil
}

func appendUsersByIds(
	db *gorm.DB,
	userIds []int,
	appendUser func(model.User),
) error {
	for start := 0; start < len(userIds); start += userInviteQueryChunkSize {
		end := start + userInviteQueryChunkSize
		if end > len(userIds) {
			end = len(userIds)
		}
		var users []model.User
		if err := relationUserQuery(db).
			Where("id IN ?", userIds[start:end]).
			Find(&users).Error; err != nil {
			return err
		}
		usersById := make(map[int]model.User, len(users))
		for _, user := range users {
			usersById[user.Id] = user
		}
		for _, userId := range userIds[start:end] {
			if user, exists := usersById[userId]; exists {
				appendUser(user)
			}
		}
	}
	return nil
}

func appendInviteesByInviterIds(
	db *gorm.DB,
	inviterIds []int,
	appendUser func(model.User),
) error {
	for start := 0; start < len(inviterIds); start += userInviteQueryChunkSize {
		end := start + userInviteQueryChunkSize
		if end > len(inviterIds) {
			end = len(inviterIds)
		}
		var users []model.User
		if err := relationUserQuery(db).
			Where("inviter_id IN ?", inviterIds[start:end]).
			Order("id DESC").
			Find(&users).Error; err != nil {
			return err
		}
		for _, user := range users {
			appendUser(user)
		}
	}
	return nil
}

func loadUserInviteRelationSnapshot(db *gorm.DB, userId int, depth int) (userInviteRelationSnapshot, error) {
	if userId <= 0 {
		return userInviteRelationSnapshot{}, errors.New("target user id is invalid")
	}
	if depth < 0 {
		return userInviteRelationSnapshot{}, errors.New("invite relation depth cannot be negative")
	}

	var target model.User
	if err := relationUserQuery(db).Where("id = ?", userId).First(&target).Error; err != nil {
		return userInviteRelationSnapshot{}, err
	}

	snapshot := userInviteRelationSnapshot{
		Target:       target,
		RelatedUsers: make([]userInviteRelationMember, 0),
	}

	visited := map[int]struct{}{target.Id: {}}
	frontier := []model.User{target}

	for currentDepth := 1; depth == 0 || currentDepth <= depth; currentDepth++ {
		candidateUsers := make(map[int]model.User)
		candidateIds := make([]int, 0)
		appendCandidate := func(user model.User) {
			if _, exists := visited[user.Id]; exists {
				return
			}
			if _, exists := candidateUsers[user.Id]; exists {
				return
			}
			candidateUsers[user.Id] = user
			candidateIds = append(candidateIds, user.Id)
		}

		parentIds := make([]int, 0, len(frontier))
		seenParentIds := make(map[int]struct{}, len(frontier))
		frontierIds := make([]int, 0, len(frontier))
		for _, user := range frontier {
			frontierIds = append(frontierIds, user.Id)
			if user.InviterId <= 0 || user.InviterId == user.Id {
				continue
			}
			if _, exists := visited[user.InviterId]; exists {
				continue
			}
			if _, exists := seenParentIds[user.InviterId]; exists {
				continue
			}
			seenParentIds[user.InviterId] = struct{}{}
			parentIds = append(parentIds, user.InviterId)
		}

		if err := appendUsersByIds(db, parentIds, appendCandidate); err != nil {
			return userInviteRelationSnapshot{}, err
		}
		if err := appendInviteesByInviterIds(db, frontierIds, appendCandidate); err != nil {
			return userInviteRelationSnapshot{}, err
		}

		if len(candidateIds) == 0 {
			break
		}

		nextFrontier := make([]model.User, 0, len(candidateIds))
		for _, candidateId := range candidateIds {
			user := candidateUsers[candidateId]
			relationType := InviteRelationTypeRelated
			if currentDepth == 1 {
				if user.Id == target.InviterId {
					relationType = InviteRelationTypeInviter
				} else {
					relationType = InviteRelationTypeInvitee
				}
			}
			snapshot.RelatedUsers = append(snapshot.RelatedUsers, userInviteRelationMember{
				User:         user,
				Depth:        currentDepth,
				RelationType: relationType,
			})
			visited[user.Id] = struct{}{}
			nextFrontier = append(nextFrontier, user)
		}
		frontier = nextFrontier
	}

	return snapshot, nil
}

func userDisableEligibility(user model.User, operatorId int, operatorRole int) (bool, string) {
	if user.DeletedAt.Valid {
		return false, UserDisableUnavailableDeleted
	}
	if user.Status == common.UserStatusDisabled {
		return false, UserDisableUnavailableAlreadyDisabled
	}
	if user.Status != common.UserStatusEnabled {
		return false, UserDisableUnavailableInvalidStatus
	}
	if user.Role == common.RoleRootUser {
		return false, UserDisableUnavailableRootProtected
	}
	if user.Id == operatorId {
		return false, UserDisableUnavailableOperatorSelf
	}
	if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
		return false, UserDisableUnavailableInsufficientPermission
	}
	return true, ""
}

func toInviteRelationUser(
	user model.User,
	depth int,
	relationType string,
	operatorId int,
	operatorRole int,
) InviteRelationUser {
	selectable, unavailableReason := userDisableEligibility(user, operatorId, operatorRole)
	return InviteRelationUser{
		Id:                user.Id,
		Username:          user.Username,
		DisplayName:       user.DisplayName,
		Role:              user.Role,
		Status:            user.Status,
		Deleted:           user.DeletedAt.Valid,
		DisableReason:     user.DisableReason,
		Selectable:        selectable,
		UnavailableReason: unavailableReason,
		Depth:             depth,
		RelationType:      relationType,
	}
}

func GetUserInviteRelations(userId int, depth int, operatorId int, operatorRole int) (UserInviteRelations, error) {
	snapshot, err := loadUserInviteRelationSnapshot(model.DB, userId, depth)
	if err != nil {
		return UserInviteRelations{}, err
	}

	result := UserInviteRelations{
		Target:       toInviteRelationUser(snapshot.Target, 0, InviteRelationTypeTarget, operatorId, operatorRole),
		Invitees:     make([]InviteRelationUser, 0),
		RelatedUsers: make([]InviteRelationUser, 0, len(snapshot.RelatedUsers)),
		QueryDepth:   depth,
	}
	for _, member := range snapshot.RelatedUsers {
		relationUser := toInviteRelationUser(
			member.User,
			member.Depth,
			member.RelationType,
			operatorId,
			operatorRole,
		)
		result.RelatedUsers = append(result.RelatedUsers, relationUser)
		if member.Depth != 1 {
			continue
		}
		switch member.RelationType {
		case InviteRelationTypeInviter:
			inviter := relationUser
			result.Inviter = &inviter
		case InviteRelationTypeInvitee:
			result.Invitees = append(result.Invitees, relationUser)
		}
	}
	return result, nil
}

func normalizeBatchDisableReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", errors.New("disable reason cannot be empty")
	}
	if len([]rune(reason)) > 255 {
		return "", errors.New("disable reason cannot exceed 255 characters")
	}
	return reason, nil
}

func BatchDisableRelatedUsers(
	targetId int,
	relatedUserIds []int,
	reason string,
	depth int,
	operatorId int,
	operatorRole int,
) (BatchDisableRelatedUsersResult, error) {
	normalizedReason, err := normalizeBatchDisableReason(reason)
	if err != nil {
		return BatchDisableRelatedUsersResult{}, err
	}

	result := BatchDisableRelatedUsersResult{
		TargetId:           targetId,
		DisabledIds:        make([]int, 0),
		AlreadyDisabledIds: make([]int, 0),
	}
	disabledUsers := make(map[int]model.User)

	err = model.DB.Transaction(func(tx *gorm.DB) error {
		snapshot, err := loadUserInviteRelationSnapshot(tx, targetId, depth)
		if err != nil {
			return err
		}

		allowedRelatedUsers := make(map[int]model.User, len(snapshot.RelatedUsers))
		for _, member := range snapshot.RelatedUsers {
			allowedRelatedUsers[member.User.Id] = member.User
		}

		orderedIds := make([]int, 0, len(relatedUserIds)+1)
		selectedUsers := make(map[int]model.User, len(relatedUserIds)+1)
		seenIds := make(map[int]struct{}, len(relatedUserIds)+1)
		appendSelectedUser := func(user model.User) {
			if _, exists := seenIds[user.Id]; exists {
				return
			}
			seenIds[user.Id] = struct{}{}
			orderedIds = append(orderedIds, user.Id)
			selectedUsers[user.Id] = user
		}

		appendSelectedUser(snapshot.Target)
		for _, relatedUserId := range relatedUserIds {
			if relatedUserId <= 0 {
				return fmt.Errorf("related user id %d is invalid", relatedUserId)
			}
			if relatedUserId == snapshot.Target.Id {
				continue
			}
			relatedUser, exists := allowedRelatedUsers[relatedUserId]
			if !exists {
				return fmt.Errorf(
					"user %d is not within invite relation depth %d of user %d",
					relatedUserId,
					depth,
					targetId,
				)
			}
			appendSelectedUser(relatedUser)
		}

		for _, userId := range orderedIds {
			user := selectedUsers[userId]
			selectable, unavailableReason := userDisableEligibility(user, operatorId, operatorRole)
			if !selectable {
				if unavailableReason == UserDisableUnavailableAlreadyDisabled {
					result.AlreadyDisabledIds = append(result.AlreadyDisabledIds, userId)
					continue
				}
				return fmt.Errorf("user %d cannot be disabled: %s", userId, unavailableReason)
			}
			result.DisabledIds = append(result.DisabledIds, userId)
			disabledUsers[userId] = user
		}

		for start := 0; start < len(result.DisabledIds); start += userDisableUpdateChunkSize {
			end := start + userDisableUpdateChunkSize
			if end > len(result.DisabledIds) {
				end = len(result.DisabledIds)
			}
			chunk := result.DisabledIds[start:end]
			updateResult := tx.Model(&model.User{}).
				Where("id IN ?", chunk).
				Updates(map[string]interface{}{
					"status":         common.UserStatusDisabled,
					"disable_reason": normalizedReason,
				})
			if updateResult.Error != nil {
				return updateResult.Error
			}
			if updateResult.RowsAffected != int64(len(chunk)) {
				return errors.New("selected users changed while applying batch disable")
			}
		}
		return nil
	})
	if err != nil {
		return BatchDisableRelatedUsersResult{}, err
	}

	adminInfo := map[string]interface{}{
		"admin_id":              operatorId,
		"batch_target_user_id":  targetId,
		"invite_relation_depth": depth,
	}
	if adminUsername, err := model.GetUsernameById(operatorId, false); err == nil {
		adminInfo["admin_username"] = adminUsername
	}
	for _, userId := range result.DisabledIds {
		user := disabledUsers[userId]
		model.RecordLogWithAdminInfo(
			userId,
			model.LogTypeManage,
			fmt.Sprintf("管理员联动禁用用户 id=%d username=%s，原因：%s", userId, user.Username, normalizedReason),
			adminInfo,
		)
		if err := model.InvalidateUserCache(userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", userId, err.Error()))
		}
		if err := model.InvalidateUserTokensCache(userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", userId, err.Error()))
		}
	}

	return result, nil
}
