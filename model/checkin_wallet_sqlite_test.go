package model

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func setupFileWalletTestDB(t *testing.T, journal string) {
	t.Helper()
	dsn := filepath.ToSlash(filepath.Join(t.TempDir(), "wallet.db")) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(" + journal + ")"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{PrepareStmt: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)
	oldDB, oldLogDB := DB, LOG_DB
	DB, LOG_DB = db, db
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, migrateWalletTestDB(db, &Log{}))
}

func parallelWalletRequests(count int, fn func(int) error) []error {
	start := make(chan struct{})
	results := make([]error, count)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return results
}

func TestCheckinWalletSQLiteConcurrent(t *testing.T) {
	for _, journal := range []string{"DELETE", "WAL"} {
		t.Run(journal, func(t *testing.T) {
			t.Run("ordinary_quota_without_checkins", func(t *testing.T) {
				setupFileWalletTestDB(t, journal)
				user := seedCheckinUser(t, 1, 1000)
				for _, err := range parallelWalletRequests(20, func(int) error {
					return DecreaseUserQuota(user.Id, 10, false)
				}) {
					require.NoError(t, err)
				}
				require.Equal(t, 800, userQuota(t, user.Id))
				for _, err := range parallelWalletRequests(20, func(int) error {
					if _, err := GetUserQuota(user.Id, true); err != nil {
						return err
					}
					return IncreaseUserQuota(user.Id, 10, false)
				}) {
					require.NoError(t, err)
				}
				require.Equal(t, 1000, userQuota(t, user.Id))
			})
			t.Run("reservations_settlement_and_expired_refunds", func(t *testing.T) {
				setupFileWalletTestDB(t, journal)
				grant := seedTrackedWallet(t, 1, 1000, 200)
				// Expiry must commit exactly once alongside concurrent reservations.
				expired := &Checkin{UserId: 1, CheckinDate: yesterday(), QuotaAwarded: 40,
					RemainingQuota: 40, WalletTracked: true, ExpiresAt: time.Now().Unix() - 1}
				require.NoError(t, DB.Create(expired).Error)
				require.NoError(t, DB.Model(&User{}).Where("id = ?", 1).Update("quota", 1240).Error)
				charges := make([]*WalletCharge, 20)
				for _, err := range parallelWalletRequests(len(charges), func(i int) error {
					var err error
					charges[i], err = ReserveWalletCharge(1, 15)
					return err
				}) {
					require.NoError(t, err)
				}
				require.Equal(t, 900, userQuota(t, 1))
				require.NoError(t, DB.First(expired, expired.Id).Error)
				require.Equal(t, 40, expired.ExpiredQuota)
				require.Zero(t, expired.RemainingQuota)
				var logCount int64
				require.NoError(t, DB.Model(&Log{}).Count(&logCount).Error)
				require.EqualValues(t, 1, logCount)
				// Duplicate delivery of an absolute settlement target cannot debit twice.
				for _, err := range parallelWalletRequests(40, func(i int) error {
					return SetWalletChargeQuota(1, charges[i%20].Id, 20, true)
				}) {
					require.NoError(t, err)
				}
				require.Equal(t, 800, userQuota(t, 1))
				var stored []WalletCharge
				require.NoError(t, DB.Find(&stored).Error)
				require.Len(t, stored, 20)
				paid, allocated := 0, 0
				for _, charge := range stored {
					require.Equal(t, 20, charge.Quota)
					paid += charge.PermanentQuota
					var parts []walletPart
					require.NoError(t, common.UnmarshalJsonStr(charge.Allocations, &parts))
					for _, part := range parts {
						require.Equal(t, grant.Id, part.CheckinId)
						allocated += part.Quota
					}
				}
				require.Equal(t, 200, paid)
				require.Equal(t, 200, allocated)
				require.NoError(t, DB.Model(grant).Update("expires_at", time.Now().Unix()-1).Error)
				for _, err := range parallelWalletRequests(40, func(i int) error {
					return RefundWalletCharge(1, charges[i%20].Id)
				}) {
					require.NoError(t, err)
				}
				require.Equal(t, 1000, userQuota(t, 1), "only permanent funds return after expiry")
				require.NoError(t, DB.First(grant, grant.Id).Error)
				require.Equal(t, 200, grant.ExpiredQuota)
				require.Zero(t, grant.RemainingQuota)
			})
			t.Run("reservations_cannot_overspend", func(t *testing.T) {
				setupFileWalletTestDB(t, journal)
				seedTrackedWallet(t, 1, 50, 50)
				successes := 0
				for _, err := range parallelWalletRequests(20, func(int) error {
					_, err := ReserveWalletCharge(1, 10)
					return err
				}) {
					if err == nil {
						successes++
					} else {
						require.ErrorIs(t, err, ErrInsufficientWalletQuota)
					}
				}
				require.Equal(t, 10, successes)
				require.Zero(t, userQuota(t, 1))
				var count int64
				require.NoError(t, DB.Model(&WalletCharge{}).Count(&count).Error)
				require.EqualValues(t, 10, count, "failed reservations must roll back their ledger rows")
			})
			t.Run("rollback", func(t *testing.T) {
				setupFileWalletTestDB(t, journal)
				grant := seedTrackedWallet(t, 1, 100, 100)
				expireTestGrant(t, grant)
				injected := errors.New("settlement failed")
				_, err := walletUserTransaction(1, func(*gorm.DB, *User, int64) error { return injected })
				require.ErrorIs(t, err, injected)
				require.Equal(t, 200, userQuota(t, 1))
				require.NoError(t, DB.First(grant, grant.Id).Error)
				require.Zero(t, grant.SettledAt)
				require.Equal(t, 100, grant.RemainingQuota)
				var count int64
				require.NoError(t, DB.Model(&Log{}).Count(&count).Error)
				require.Zero(t, count, "rolled back expiry must not emit an audit log")
			})
		})
	}
}

func TestWalletTestSchemaHonorsTablePrefix(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "isolated_"}})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, migrateWalletTestDB(db))
	require.True(t, db.Migrator().HasTable("isolated_checkins"))
	require.False(t, db.Migrator().HasTable("checkins"))
	var count int64
	require.NoError(t, db.Model(&Checkin{}).Count(&count).Error)
}
