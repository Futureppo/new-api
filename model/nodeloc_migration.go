package model

import "gorm.io/gorm"

// SQLite cannot ADD COLUMN with a UNIQUE constraint. Add the nullable column
// first; the regular User AutoMigrate then creates its unique index.
func migrateNodeLocUserID(db *gorm.DB) error {
	if db.Dialector.Name() != "sqlite" || !db.Migrator().HasTable(&User{}) || db.Migrator().HasColumn(&User{}, "NodeLocId") {
		return nil
	}
	type nodeLocColumn struct {
		NodeLocId string `gorm:"column:nodeloc_id;type:varchar(64);default:null"`
	}
	return db.Table("users").Migrator().AddColumn(&nodeLocColumn{}, "NodeLocId")
}
