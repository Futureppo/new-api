package model

import "gorm.io/gorm"

// Quota operations also read the check-in ledger, even for users who never
// checked in. Keep that schema dependency shared by isolated test databases.
func migrateWalletTestDB(db *gorm.DB, extra ...interface{}) error {
	models := []interface{}{&User{}, &Checkin{}, &WalletCharge{}}
	return db.AutoMigrate(append(models, extra...)...)
}
