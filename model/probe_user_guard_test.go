package model

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestProbeUserGuardDatabaseCompatibility(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "probe.db") + "?_pragma=busy_timeout(5000)")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("set TEST_MYSQL_DSN for isolated migration/concurrency checks")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("set TEST_POSTGRES_DSN for isolated migration/concurrency checks")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("probeguard_%d_", time.Now().UnixNano())}})
			require.NoError(t, err)
			previous := DB
			DB = db
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(8)
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&ProbeUserAbuseState{}, &User{}))
				DB = previous
				_ = sqlDB.Close()
			})
			// Upgrade a pre-existing user schema without inheriting any IP offenses.
			require.NoError(t, db.AutoMigrate(&User{}))
			user := User{Username: "probe", AffCode: "probe", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.AutoMigrate(&ProbeUserAbuseState{}))
			reason := func(count int, period UserDisablePeriod) string {
				return fmt.Sprintf("offense %d minutes %d", count, period.Minutes)
			}
			for stage, minutes := range []int64{2, 9, 0} {
				start := time.Now()
				var wait sync.WaitGroup
				results := make(chan *ProbeUserPenaltyResult, 8)
				errs := make(chan error, 8)
				for i := 0; i < 8; i++ {
					wait.Add(1)
					go func() {
						defer wait.Done()
						result, err := ApplyProbeUserPenalty(user.Id, start, "tool:probe_ping", "request", 2, 9, reason)
						results <- result
						errs <- err
					}()
				}
				wait.Wait()
				close(results)
				close(errs)
				for err := range errs {
					require.NoError(t, err)
				}
				applied := 0
				for result := range results {
					if result.Applied {
						applied++
					}
				}
				require.Equal(t, 1, applied)
				state, err := GetProbeUserAbuseState(user.Id)
				require.NoError(t, err)
				require.Equal(t, stage+1, state.OffenseCount)
				require.NoError(t, db.First(&user, user.Id).Error)
				require.Equal(t, minutes, user.DisableDurationMinutes)
				require.Equal(t, common.UserStatusDisabled, user.Status)
				if minutes > 0 {
					require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Updates(UserEnableUpdates()).Error)
				} else {
					require.Zero(t, user.DisableUntil)
				}
			}
			require.NoError(t, ResetProbeUserAbuseState(user.Id))
			state, err := GetProbeUserAbuseState(user.Id)
			require.NoError(t, err)
			require.Zero(t, state.OffenseCount)
			require.NoError(t, db.First(&user, user.Id).Error)
			require.Equal(t, common.UserStatusDisabled, user.Status)
			require.Zero(t, user.DisableUntil)
		})
	}
}
