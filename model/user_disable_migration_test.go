package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestUserDisableMigrationCompatibility(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite": driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" { t.Skip("set TEST_MYSQL_DSN to run MySQL migration and expiry checks") }
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" { t.Skip("set TEST_POSTGRES_DSN to run PostgreSQL migration and expiry checks") }
				driver = postgres.Open(dsn)
			}
			// External databases get uniquely named tables; existing tables are never touched.
			prefix := fmt.Sprintf("disabletest_%d_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}})
			require.NoError(t, err)
			oldDB, oldLogDB, oldRedis := DB, LOG_DB, common.RedisEnabled
			DB, LOG_DB, common.RedisEnabled = db, db, false
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&Log{}, &Token{}, &User{}))
				DB, LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis
				sqlDB, _ := db.DB(); _ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &Log{}))
			// Reconstruct the prior schema on these disposable tables.
			require.NoError(t, db.Migrator().DropColumn(&User{}, "DisableDurationMinutes"))
			require.NoError(t, db.Migrator().DropColumn(&User{}, "DisableUntil"))
			legacy := User{Username: "legacy", AffCode: "legacy", Password: "hash", Status: common.UserStatusDisabled, DisableReason: "legacy reason"}
			require.NoError(t, db.Omit("disable_duration_minutes", "disable_until").Create(&legacy).Error)
			require.NoError(t, db.AutoMigrate(&User{}))
			require.NoError(t, db.First(&legacy, legacy.Id).Error)
			require.Equal(t, common.UserStatusDisabled, legacy.Status)
			require.Equal(t, "legacy reason", legacy.DisableReason)
			require.Zero(t, legacy.DisableDurationMinutes)
			require.Zero(t, legacy.DisableUntil)
			period, err := NewUserDisablePeriod(2, 1700000000)
			require.NoError(t, err)
			require.NoError(t, db.Model(&User{}).Where("id = ?", legacy.Id).Updates(period.Updates("timed")).Error)
			require.NoError(t, db.First(&legacy, legacy.Id).Error)
			require.Equal(t, int64(1700000120), legacy.DisableUntil)
			require.NoError(t, ResolveUserDisableExpiry(&legacy, 1700000120))
			require.Equal(t, common.UserStatusEnabled, legacy.Status)
			require.Empty(t, legacy.DisableReason)
			require.Zero(t, legacy.DisableUntil)
		})
	}
}
