package model

import "gorm.io/gorm/clause"

type UserGroupCount struct {
	Group string `json:"group" gorm:"column:group_name"`
	Count int64  `json:"count"`
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
