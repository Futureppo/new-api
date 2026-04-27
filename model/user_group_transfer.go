package model

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UserGroupBalanceModeAdd      = "add"
	UserGroupBalanceModeSubtract = "subtract"
	UserGroupBalanceModeOverride = "override"
)

type UserGroupCount struct {
	Group string `json:"group" gorm:"column:group_name"`
	Count int64  `json:"count"`
}

type UserGroupBalancePreview struct {
	Group      string `json:"group"`
	Mode       string `json:"mode"`
	Quota      int    `json:"quota"`
	Affected   int64  `json:"affected"`
	TotalDelta int64  `json:"total_delta"`
}

type userGroupBalanceSnapshot struct {
	Id    int
	Quota int
}

func GetActiveUserGroupCounts() ([]UserGroupCount, error) {
	var counts []UserGroupCount
	err := DB.Model(&User{}).
		Select(commonGroupCol+" AS group_name, COUNT(*) AS count").
		Where(commonGroupCol+" <> ?", "").
		Group("group").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "group"}}).
		Scan(&counts).Error
	return counts, err
}

func CountActiveUsersByGroup(group string) (int64, error) {
	var count int64
	err := DB.Model(&User{}).
		Where(commonGroupCol+" = ?", group).
		Count(&count).Error
	return count, err
}

func TransferActiveUsersGroup(sourceGroup string, targetGroup string) ([]int, int64, error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer tx.Rollback()

	var userIDs []int
	if err := tx.Model(&User{}).
		Where(commonGroupCol+" = ?", sourceGroup).
		Pluck("id", &userIDs).Error; err != nil {
		return nil, 0, err
	}

	if len(userIDs) == 0 {
		if err := tx.Commit().Error; err != nil {
			return nil, 0, err
		}
		return userIDs, 0, nil
	}

	result := tx.Model(&User{}).
		Where("id IN ?", userIDs).
		Updates(map[string]interface{}{
			"group": targetGroup,
		})
	if result.Error != nil {
		return nil, 0, result.Error
	}

	if err := tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	for _, userID := range userIDs {
		_ = InvalidateUserCache(userID)
	}

	return userIDs, result.RowsAffected, nil
}

func PreviewActiveUsersGroupBalance(group string, mode string, quota int) (UserGroupBalancePreview, error) {
	users, err := getActiveUserBalanceSnapshotsByGroup(DB, group)
	if err != nil {
		return UserGroupBalancePreview{}, err
	}
	return buildUserGroupBalancePreview(group, mode, quota, users)
}

func UpdateActiveUsersGroupBalance(group string, mode string, quota int) (UserGroupBalancePreview, []int, error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return UserGroupBalancePreview{}, nil, tx.Error
	}
	defer tx.Rollback()

	users, err := getActiveUserBalanceSnapshotsByGroup(tx, group)
	if err != nil {
		return UserGroupBalancePreview{}, nil, err
	}

	preview, err := buildUserGroupBalancePreview(group, mode, quota, users)
	if err != nil {
		return UserGroupBalancePreview{}, nil, err
	}

	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.Id)
	}

	if len(userIDs) == 0 {
		if err := tx.Commit().Error; err != nil {
			return UserGroupBalancePreview{}, nil, err
		}
		return preview, userIDs, nil
	}

	updates, err := buildUserGroupBalanceUpdates(mode, quota)
	if err != nil {
		return UserGroupBalancePreview{}, nil, err
	}

	if err := tx.Model(&User{}).
		Where("id IN ?", userIDs).
		Updates(updates).Error; err != nil {
		return UserGroupBalancePreview{}, nil, err
	}

	if err := tx.Commit().Error; err != nil {
		return UserGroupBalancePreview{}, nil, err
	}

	for _, userID := range userIDs {
		_ = InvalidateUserCache(userID)
	}

	return preview, userIDs, nil
}

func getActiveUserBalanceSnapshotsByGroup(db *gorm.DB, group string) ([]userGroupBalanceSnapshot, error) {
	var users []userGroupBalanceSnapshot
	err := db.Model(&User{}).
		Select("id", "quota").
		Where(commonGroupCol+" = ?", group).
		Find(&users).Error
	return users, err
}

func buildUserGroupBalancePreview(group string, mode string, quota int, users []userGroupBalanceSnapshot) (UserGroupBalancePreview, error) {
	preview := UserGroupBalancePreview{
		Group:    group,
		Mode:     mode,
		Quota:    quota,
		Affected: int64(len(users)),
	}

	for _, user := range users {
		switch mode {
		case UserGroupBalanceModeAdd:
			preview.TotalDelta += int64(quota)
		case UserGroupBalanceModeSubtract:
			if user.Quota < quota {
				preview.TotalDelta -= int64(user.Quota)
			} else {
				preview.TotalDelta -= int64(quota)
			}
		case UserGroupBalanceModeOverride:
			preview.TotalDelta += int64(quota - user.Quota)
		default:
			return UserGroupBalancePreview{}, errors.New("invalid balance mode")
		}
	}

	return preview, nil
}

func buildUserGroupBalanceUpdates(mode string, quota int) (map[string]interface{}, error) {
	switch mode {
	case UserGroupBalanceModeAdd:
		return map[string]interface{}{
			"quota": gorm.Expr("quota + ?", quota),
		}, nil
	case UserGroupBalanceModeSubtract:
		return map[string]interface{}{
			"quota": gorm.Expr("CASE WHEN quota > ? THEN quota - ? ELSE 0 END", quota, quota),
		}, nil
	case UserGroupBalanceModeOverride:
		return map[string]interface{}{
			"quota": quota,
		}, nil
	default:
		return nil, errors.New("invalid balance mode")
	}
}
