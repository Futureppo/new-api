package model

import (
	"errors"

	"github.com/samber/lo"
	"gorm.io/gorm"
)

// IsValidChannelIDRange keeps API IDs within JavaScript's safe integer range.
func IsValidChannelIDRange(startID, endID int) bool {
	return startID > 0 && endID >= startID && int64(endID) <= 9007199254740991
}

// DeleteChannelsByIDRange deletes existing channels and their abilities atomically.
// Only existing IDs are collected, so sparse ranges do not allocate by range size.
func DeleteChannelsByIDRange(startID, endID int) (int64, []int, error) {
	if !IsValidChannelIDRange(startID, endID) {
		return 0, nil, errors.New("渠道 ID 范围无效")
	}
	var ids []int
	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Channel{}).Where("id >= ? AND id <= ?", startID, endID).
			Order("id").Pluck("id", &ids).Error; err != nil {
			return err
		}
		var err error
		deleted, err = deleteChannelIDsInTransaction(tx, ids)
		return err
	})
	if err != nil {
		return 0, nil, err
	}
	return deleted, ids, nil
}

func deleteChannelIDsInTransaction(tx *gorm.DB, ids []int) (int64, error) {
	var deleted int64
	for _, chunk := range lo.Chunk(ids, 200) {
		result := tx.Where("id IN ?", chunk).Delete(&Channel{})
		if result.Error != nil {
			return 0, result.Error
		}
		deleted += result.RowsAffected
		if err := tx.Where("channel_id IN ?", chunk).Delete(&Ability{}).Error; err != nil {
			return 0, err
		}
	}
	return deleted, nil
}
