package enhancement

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Optional DSNs must point to empty, disposable databases. This suite checks the
// real migration and aggregate queries; SQLite always runs without setup.
func TestSessionRiskDatabaseMigrationAndAggregation(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "logs.db"))
			case "mysql":
				dsn := os.Getenv("TEST_SESSION_RISK_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_SESSION_RISK_MYSQL_DSN not set")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_SESSION_RISK_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_SESSION_RISK_POSTGRES_DSN not set")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			require.False(t, db.Migrator().HasTable("logs"), "use an empty disposable database")
			previous := model.LOG_DB
			model.LOG_DB = db // Keep the main DB independent of the log DB.
			defer func() { model.LOG_DB = previous }()
			// Reconstruct the pre-feature schema from Log, excluding only the new fields/indexes.
			var fields []reflect.StructField
			logType := reflect.TypeOf(model.Log{})
			for i := 0; i < logType.NumField(); i++ {
				field := logType.Field(i)
				if strings.HasPrefix(field.Name, "Session") || strings.HasPrefix(field.Name, "Cache") {
					continue
				}
				field.Tag = reflect.StructTag(strings.ReplaceAll(strings.ReplaceAll(string(field.Tag), ";index:idx_logs_session_time,priority:2", ""), ";index:idx_logs_cache_time,priority:2", ""))
				fields = append(fields, field)
			}
			legacy := reflect.New(reflect.StructOf(fields)).Interface()
			require.NoError(t, db.Table("logs").AutoMigrate(legacy))
			defer db.Migrator().DropTable(&model.Log{})
			require.NoError(t, db.Table("logs").Create(map[string]interface{}{"user_id": 1, "created_at": 100, "type": model.LogTypeConsume, "username": "old"}).Error)
			for i := 0; i < 2; i++ {
				require.NoError(t, db.AutoMigrate(&model.Log{}))
			}
			require.True(t, db.Migrator().HasIndex(&model.Log{}, "idx_logs_session_time"))
			require.True(t, db.Migrator().HasIndex(&model.Log{}, "idx_logs_cache_time"))
			var historical model.Log
			require.NoError(t, db.First(&historical).Error)
			require.False(t, historical.SessionRecorded)
			require.Empty(t, historical.SessionHash)
			require.Empty(t, historical.CacheHash)
			now := common.GetTimestamp()
			logs := []model.Log{
				sessionRiskLog(1, 1, "Case-敏感", "cache", now-60),
				sessionRiskLog(2, 2, "Case-敏感", "cache", now),
				sessionRiskLog(3, 3, "case-敏感", "", now),
			}
			require.NoError(t, db.Create(&logs).Error)
			for _, kind := range []string{"session", "cache"} {
				page, err := SharedSessions(kind, IPRiskQuery{Start: now - 60, End: now, Keyword: "user-2"})
				require.NoError(t, err)
				require.EqualValues(t, 1, page.Total)
				require.EqualValues(t, 2, page.Items[0].UserCount)
				require.EqualValues(t, 2, page.Items[0].RequestCount)
				require.EqualValues(t, 20, page.Items[0].Quota)
			}
			items, total, err := model.GetUserLogs(2, 0, now-60, now, "", "", 0, 20, "", "", "", "", model.LogSessionFilter{SessionID: "Case-敏感"})
			require.NoError(t, err)
			require.EqualValues(t, 1, total)
			require.Equal(t, 2, items[0].UserId)
		})
	}
}
