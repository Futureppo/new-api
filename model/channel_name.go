package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeleteChannelsByExactName preserves case, accents and all whitespace in name.
func DeleteChannelsByExactName(name string) (int64, []int, error) {
	if strings.TrimSpace(name) == "" {
		return 0, nil, errors.New("渠道名称不能为空")
	}
	var ids []int
	var deleted int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var candidates []Channel
		// Use the name index to narrow candidates, then compare in Go because
		// database collations may ignore case, accents or trailing spaces.
		// Lock candidates against renames until deletion commits (SQLite uses
		// its transaction locking and omits the unsupported FOR UPDATE clause).
		if err := tx.Select("id", "name").Where("name = ?", name).Order("id").
			Clauses(clause.Locking{Strength: "UPDATE"}).Find(&candidates).Error; err != nil {
			return err
		}
		for _, channel := range candidates {
			if channel.Name == name {
				ids = append(ids, channel.Id)
			}
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
