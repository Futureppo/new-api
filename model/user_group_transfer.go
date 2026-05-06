package model

import (
	"errors"
	"math"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	UserGroupBalanceModeAdd      = "add"
	UserGroupBalanceModeSubtract = "subtract"
	UserGroupBalanceModeOverride = "override"
	UserGroupBalanceModeMultiply = "multiply"

	userGroupBalanceUpdateBatchSize = 500
)

type UserGroupCount struct {
	Group string `json:"group" gorm:"column:group_name"`
	Count int64  `json:"count"`
}

type UserGroupBalancePreview struct {
	Group      string   `json:"group"`
	Mode       string   `json:"mode"`
	Quota      int      `json:"quota"`
	Factor     *float64 `json:"factor,omitempty"`
	Affected   int64    `json:"affected"`
	TotalDelta int64    `json:"total_delta"`
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

func PreviewActiveUsersGroupBalance(group string, mode string, quota int, factor *float64) (UserGroupBalancePreview, error) {
	users, err := getActiveUserBalanceSnapshotsByGroup(DB, group)
	if err != nil {
		return UserGroupBalancePreview{}, err
	}
	return buildUserGroupBalancePreview(group, mode, quota, factor, users)
}

func UpdateActiveUsersGroupBalance(group string, mode string, quota int, factor *float64) (UserGroupBalancePreview, []int, error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return UserGroupBalancePreview{}, nil, tx.Error
	}
	defer tx.Rollback()

	users, err := getActiveUserBalanceSnapshotsByGroup(tx, group)
	if err != nil {
		return UserGroupBalancePreview{}, nil, err
	}

	preview, err := buildUserGroupBalancePreview(group, mode, quota, factor, users)
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

	if mode == UserGroupBalanceModeMultiply {
		if factor == nil {
			return UserGroupBalancePreview{}, nil, errors.New("invalid balance factor")
		}
		if err := updateUserGroupBalanceByFactor(tx, users, *factor); err != nil {
			return UserGroupBalancePreview{}, nil, err
		}
	} else {
		updates, err := buildUserGroupBalanceUpdates(mode, quota)
		if err != nil {
			return UserGroupBalancePreview{}, nil, err
		}

		if err := tx.Model(&User{}).
			Where("id IN ?", userIDs).
			Updates(updates).Error; err != nil {
			return UserGroupBalancePreview{}, nil, err
		}
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

func buildUserGroupBalancePreview(group string, mode string, quota int, factor *float64, users []userGroupBalanceSnapshot) (UserGroupBalancePreview, error) {
	preview := UserGroupBalancePreview{
		Group:    group,
		Mode:     mode,
		Quota:    quota,
		Affected: int64(len(users)),
	}
	if factor != nil {
		factorValue := *factor
		preview.Factor = &factorValue
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
		case UserGroupBalanceModeMultiply:
			if preview.Factor == nil {
				return UserGroupBalancePreview{}, errors.New("invalid balance factor")
			}
			newQuota, err := multiplyUserQuota(user.Quota, *preview.Factor)
			if err != nil {
				return UserGroupBalancePreview{}, err
			}
			preview.TotalDelta += int64(newQuota) - int64(user.Quota)
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

func updateUserGroupBalanceByFactor(tx *gorm.DB, users []userGroupBalanceSnapshot, factor float64) error {
	for start := 0; start < len(users); start += userGroupBalanceUpdateBatchSize {
		end := start + userGroupBalanceUpdateBatchSize
		if end > len(users) {
			end = len(users)
		}
		batch := users[start:end]
		ids := make([]int, 0, len(batch))
		args := make([]interface{}, 0, len(batch)*2)
		var builder strings.Builder
		builder.WriteString("CASE id")
		for _, user := range batch {
			newQuota, err := multiplyUserQuota(user.Quota, factor)
			if err != nil {
				return err
			}
			builder.WriteString(" WHEN ? THEN ?")
			args = append(args, user.Id, newQuota)
			ids = append(ids, user.Id)
		}
		builder.WriteString(" ELSE quota END")

		if err := tx.Model(&User{}).
			Where("id IN ?", ids).
			Update("quota", gorm.Expr(builder.String(), args...)).Error; err != nil {
			return err
		}
	}
	return nil
}

func multiplyUserQuota(quota int, factor float64) (int, error) {
	if factor <= 0 || math.IsNaN(factor) || math.IsInf(factor, 0) {
		return 0, errors.New("invalid balance factor")
	}
	value := float64(quota) * factor
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, errors.New("invalid balance factor")
	}
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	if value >= float64(maxInt) || value <= float64(minInt) {
		return 0, errors.New("balance quota overflow")
	}
	return int(value), nil
}
