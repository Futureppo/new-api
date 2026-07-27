package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const userDisableUpdateChunkSize = 100

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
}

type UserInviteRelations struct {
	Target   InviteRelationUser   `json:"target"`
	Inviter  *InviteRelationUser  `json:"inviter"`
	Invitees []InviteRelationUser `json:"invitees"`
}

type BatchDisableRelatedUsersResult struct {
	TargetId           int   `json:"target_id"`
	DisabledIds        []int `json:"disabled_ids"`
	AlreadyDisabledIds []int `json:"already_disabled_ids"`
}

type userInviteRelationSnapshot struct {
	Target   model.User
	Inviter  *model.User
	Invitees []model.User
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

func loadUserInviteRelationSnapshot(db *gorm.DB, userId int) (userInviteRelationSnapshot, error) {
	if userId <= 0 {
		return userInviteRelationSnapshot{}, errors.New("target user id is invalid")
	}

	var target model.User
	if err := relationUserQuery(db).Where("id = ?", userId).First(&target).Error; err != nil {
		return userInviteRelationSnapshot{}, err
	}

	snapshot := userInviteRelationSnapshot{
		Target:   target,
		Invitees: make([]model.User, 0),
	}

	if target.InviterId > 0 && target.InviterId != target.Id {
		var inviter model.User
		err := relationUserQuery(db).Where("id = ?", target.InviterId).First(&inviter).Error
		if err == nil {
			snapshot.Inviter = &inviter
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return userInviteRelationSnapshot{}, err
		}
	}

	if err := relationUserQuery(db).
		Where("inviter_id = ? AND id <> ?", target.Id, target.Id).
		Order("id DESC").
		Find(&snapshot.Invitees).Error; err != nil {
		return userInviteRelationSnapshot{}, err
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

func toInviteRelationUser(user model.User, operatorId int, operatorRole int) InviteRelationUser {
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
	}
}

func GetUserInviteRelations(userId int, operatorId int, operatorRole int) (UserInviteRelations, error) {
	snapshot, err := loadUserInviteRelationSnapshot(model.DB, userId)
	if err != nil {
		return UserInviteRelations{}, err
	}

	result := UserInviteRelations{
		Target:   toInviteRelationUser(snapshot.Target, operatorId, operatorRole),
		Invitees: make([]InviteRelationUser, 0, len(snapshot.Invitees)),
	}
	if snapshot.Inviter != nil {
		inviter := toInviteRelationUser(*snapshot.Inviter, operatorId, operatorRole)
		result.Inviter = &inviter
	}
	for _, invitee := range snapshot.Invitees {
		result.Invitees = append(result.Invitees, toInviteRelationUser(invitee, operatorId, operatorRole))
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
		snapshot, err := loadUserInviteRelationSnapshot(tx, targetId)
		if err != nil {
			return err
		}

		allowedRelatedUsers := make(map[int]model.User, len(snapshot.Invitees)+1)
		if snapshot.Inviter != nil {
			allowedRelatedUsers[snapshot.Inviter.Id] = *snapshot.Inviter
		}
		for _, invitee := range snapshot.Invitees {
			allowedRelatedUsers[invitee.Id] = invitee
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
				return fmt.Errorf("user %d is not a direct invite relation of user %d", relatedUserId, targetId)
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
		"admin_id":             operatorId,
		"batch_target_user_id": targetId,
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
