package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Opt-in integration run. All tables live in a unique disposable schema; no
// production tables or database defaults are changed.
func TestCheckinWalletPostgresIntegration(t *testing.T) {
	dsn := os.Getenv("NEWAPI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("NEWAPI_TEST_POSTGRES_DSN is not set")
	}
	root, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	schema := fmt.Sprintf("checkin_test_%d", time.Now().UnixNano())
	require.NoError(t, root.Exec("CREATE SCHEMA "+schema).Error)
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	oldDB, oldLogDB := DB, LOG_DB
	oldSQLite, oldPG, oldMySQL := common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL
	DB, LOG_DB = db, db
	common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = false, true, false
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingPostgreSQL, common.UsingMySQL = oldSQLite, oldPG, oldMySQL
		initCol()
		require.NoError(t, root.Exec("DROP SCHEMA "+schema+" CASCADE").Error)
		conn, _ := db.DB()
		_ = conn.Close()
		conn, _ = root.DB()
		_ = conn.Close()
	})
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(8)
	require.NoError(t, migrateWalletTestDB(db, &Task{}, &Token{}, &Log{}, &ConversationLog{},
		&Channel{}, &Ability{}, &TopUp{}, &SubscriptionPlan{}, &SubscriptionOrder{},
		&UserSubscription{}, &IPBan{}, &ProbeIPAbuseState{}))
	for _, test := range []struct {
		name string
		fn   func(*testing.T)
	}{
		{"redemption", TestCheckinWalletPreservesRedemptionWithoutConsumeLogs},
		{"cross_midnight", TestCheckinWalletCrossMidnightReservationPreservesPermanentQuota},
		{"expired_refund", TestCheckinWalletExpiredRefundReturnsOnlyPermanentPart},
		{"partial_refund", TestCheckinWalletSameDayPartialRefundKeepsSigninFirst},
		{"rollback_and_retry", TestCheckinWalletSettlementFailureRollsBackBothWrites},
		{"legacy", TestCheckinWalletLegacyRowsNeverReclaimUnknownSources},
		{"concurrent_debits", TestCheckinWalletConcurrentDebitAndReclaimIsSafe},
		{"concurrent_reservations", TestCheckinWalletConcurrentReservationsCannotOverspend},
		{"deleted_users", TestCheckinWalletDeletedUserDoesNotBlockExpiry},
		{"group_reduction", TestCheckinWalletGroupReductionKeepsSourceBalances},
		{"idempotency", TestCheckinWalletChargeTargetRetriesAreIdempotent},
		{"new_day_grant", TestCheckinWalletReservedFundsDoNotSpendTomorrowsGrant},
		{"admin_override", TestCheckinWalletAdminOverrideReducesGrantFirst},
		{"signed_pricing", TestCheckinWalletSignedCreditsRemainPermanentAndIdempotent},
	} {
		t.Run(test.name, test.fn)
	}
}
