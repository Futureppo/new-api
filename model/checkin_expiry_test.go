package model

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func seedCheckinUser(t *testing.T, id, quota int) *User {
	t.Helper()
	u := &User{Id: id, Username: fmt.Sprintf("expiry_user_%d", id), Password: "password123", Status: common.UserStatusEnabled, Group: "default", Quota: quota, AffCode: fmt.Sprintf("expiry_%d", id)}
	require.NoError(t, DB.Create(u).Error)
	return u
}
func userQuota(t *testing.T, id int) int {
	t.Helper()
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", id).First(&u).Error)
	return u.Quota
}
func yesterday() string { return time.Now().AddDate(0, 0, -1).Format("2006-01-02") }
func seedTrackedWallet(t *testing.T, id, paid, grant int) *Checkin {
	t.Helper()
	seedCheckinUser(t, id, paid+grant)
	now := time.Now()
	row := &Checkin{UserId: id, CheckinDate: now.Format("2006-01-02"), QuotaAwarded: grant, RemainingQuota: grant, WalletTracked: true, ExpiresAt: checkinExpiresAt(now), CreatedAt: now.Unix()}
	require.NoError(t, DB.Create(row).Error)
	return row
}
func expireTestGrant(t *testing.T, row *Checkin) {
	t.Helper()
	require.NoError(t, DB.Model(row).Updates(map[string]interface{}{"checkin_date": yesterday(), "expires_at": time.Now().Unix() - 1}).Error)
}

func TestCheckinWalletPreservesRedemptionWithoutConsumeLogs(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 601, 100, 100)
	old := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = old })
	require.NoError(t, DecreaseUserQuota(601, 60, false))
	expireTestGrant(t, row)
	n, reclaimed, err := SettleCheckinDate(yesterday(), "unused", 100)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.EqualValues(t, 40, reclaimed)
	require.Equal(t, 100, userQuota(t, 601))
}
func TestCheckinWalletCrossMidnightReservationPreservesPermanentQuota(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 602, 100, 100)
	charge, err := ReserveWalletCharge(602, 60)
	require.NoError(t, err)
	expireTestGrant(t, row)
	require.NoError(t, SetWalletChargeQuota(602, charge.Id, 60, true))
	require.Equal(t, 100, userQuota(t, 602))
}
func TestCheckinWalletExpiredRefundReturnsOnlyPermanentPart(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 603, 100, 100)
	charge, err := ReserveWalletCharge(603, 150)
	require.NoError(t, err)
	expireTestGrant(t, row)
	require.NoError(t, RefundWalletCharge(603, charge.Id))
	require.Equal(t, 100, userQuota(t, 603))
	require.NoError(t, RefundWalletCharge(603, charge.Id))
	require.Equal(t, 100, userQuota(t, 603))
	var grant Checkin
	require.NoError(t, DB.First(&grant, row.Id).Error)
	require.Equal(t, 100, grant.ExpiredQuota)
}
func TestCheckinWalletSameDayPartialRefundKeepsSigninFirst(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 604, 100, 100)
	charge, err := ReserveWalletCharge(604, 150)
	require.NoError(t, err)
	require.NoError(t, SetWalletChargeQuota(604, charge.Id, 60, true))
	require.Equal(t, 140, userQuota(t, 604))
	expireTestGrant(t, row)
	quota, err := GetWalletQuota(604)
	require.NoError(t, err)
	require.Equal(t, 100, quota)
}
func TestCheckinWalletSettlementFailureRollsBackBothWrites(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 605, 100, 100)
	expireTestGrant(t, row)
	name := "test:fail_settlement_marker"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "checkins" {
			tx.AddError(fmt.Errorf("injected failure"))
		}
	}))
	_, _, err := SettleCheckinDate(yesterday(), "unused", 100)
	require.Error(t, err)
	require.Equal(t, 200, userQuota(t, 605))
	require.NoError(t, DB.Callback().Update().Remove(name))
	_, total, err := SettleCheckinDate(yesterday(), "unused", 100)
	require.NoError(t, err)
	require.EqualValues(t, 100, total)
	require.Equal(t, 100, userQuota(t, 605))
	_, total, err = SettleCheckinDate(yesterday(), "unused", 100)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Equal(t, 100, userQuota(t, 605))
}
func TestCheckinWalletLegacyRowsNeverReclaimUnknownSources(t *testing.T) {
	truncateTables(t)
	seedCheckinUser(t, 606, 500)
	require.NoError(t, DB.Create(&Checkin{UserId: 606, CheckinDate: yesterday(), QuotaAwarded: 100}).Error)
	n, total, err := SettleCheckinDate(yesterday(), operation_setting.CheckinExpireModeAll, 100)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Zero(t, total)
	require.Equal(t, 500, userQuota(t, 606))
}
func TestCheckinWalletConcurrentDebitAndReclaimIsSafe(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 607, 1000, 100)
	expireTestGrant(t, row)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- DecreaseUserQuota(607, 10, false) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 800, userQuota(t, 607))
}
func TestCheckinWalletChargeTargetRetriesAreIdempotent(t *testing.T) {
	truncateTables(t)
	seedTrackedWallet(t, 608, 100, 100)
	charge, err := ReserveWalletCharge(608, 30)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		require.NoError(t, SetWalletChargeQuota(608, charge.Id, 70, true))
	}
	require.Equal(t, 130, userQuota(t, 608))
	require.Error(t, SetWalletChargeQuota(999, charge.Id, 1, true))
	require.Equal(t, 130, userQuota(t, 608))
}
func TestCheckinWalletReservedFundsDoNotSpendTomorrowsGrant(t *testing.T) {
	truncateTables(t)
	old := seedTrackedWallet(t, 609, 100, 100)
	charge, err := ReserveWalletCharge(609, 80)
	require.NoError(t, err)
	expireTestGrant(t, old)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", 609).Update("quota", gorm.Expr("quota + ?", 50)).Error)
	today := &Checkin{UserId: 609, CheckinDate: time.Now().Format("2006-01-02"), QuotaAwarded: 50, RemainingQuota: 50, WalletTracked: true, ExpiresAt: checkinExpiresAt(time.Now())}
	require.NoError(t, DB.Create(today).Error)
	require.NoError(t, SetWalletChargeQuota(609, charge.Id, 60, true))
	require.Equal(t, 150, userQuota(t, 609))
	var actual Checkin
	require.NoError(t, DB.First(&actual, today.Id).Error)
	require.Equal(t, 50, actual.RemainingQuota)
}
func TestCheckinWalletAdminOverrideReducesGrantFirst(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 610, 100, 100)
	require.NoError(t, SetWalletQuota(610, 150))
	expireTestGrant(t, row)
	q, err := GetWalletQuota(610)
	require.NoError(t, err)
	require.Equal(t, 100, q)
}
func TestCheckinWalletSignedCreditsRemainPermanentAndIdempotent(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 611, 100, 100)
	charge, err := ReserveWalletCharge(611, 60)
	require.NoError(t, err)
	require.NoError(t, SetWalletChargeQuota(611, charge.Id, -20, true))
	require.NoError(t, SetWalletChargeQuota(611, charge.Id, -20, true))
	expireTestGrant(t, row)
	q, err := GetWalletQuota(611)
	require.NoError(t, err)
	require.Equal(t, 120, q)
}
func TestOldestUnsettledCheckinDateIgnoresToday(t *testing.T) {
	truncateTables(t)
	seedTrackedWallet(t, 612, 100, 100)
	date, err := OldestUnsettledCheckinDate(time.Now().Format("2006-01-02"))
	require.NoError(t, err)
	require.Empty(t, date)
}

func TestCheckinWalletDeletedUserDoesNotBlockExpiry(t *testing.T) {
	truncateTables(t)
	first := seedTrackedWallet(t, 613, 100, 100)
	second := seedTrackedWallet(t, 614, 100, 100)
	expireTestGrant(t, first)
	expireTestGrant(t, second)
	require.NoError(t, DB.Delete(&User{}, 613).Error)
	n, _, err := SettleCheckinDate(yesterday(), "unused", 100)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, 100, userQuota(t, 614))
}
func TestCheckinWalletGroupReductionKeepsSourceBalances(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 615, 100, 100)
	_, _, err := UpdateActiveUsersGroupBalance("default", UserGroupBalanceModeSubtract, 50, nil)
	require.NoError(t, err)
	expireTestGrant(t, row)
	quota, err := GetWalletQuota(615)
	require.NoError(t, err)
	require.Equal(t, 100, quota)
}

func TestCheckinWalletConcurrentReservationsCannotOverspend(t *testing.T) {
	truncateTables(t)
	seedTrackedWallet(t, 616, 50, 50)
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := ReserveWalletCharge(616, 30); results <- err }()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, ErrInsufficientWalletQuota)
		}
	}
	require.Equal(t, 3, accepted)
	require.Equal(t, 10, userQuota(t, 616))
}

func TestCheckinWalletReferralUpdatesDoNotOverwriteFunds(t *testing.T) {
	truncateTables(t)
	row := seedTrackedWallet(t, 617, 100, 100)
	old := common.QuotaForInviter
	common.QuotaForInviter = 7
	t.Cleanup(func() { common.QuotaForInviter = old })
	require.NoError(t, inviteUser(617))
	var user User
	require.NoError(t, DB.First(&user, 617).Error)
	require.Equal(t, 7, user.AffQuota)
	require.Equal(t, 7, user.AffHistoryQuota)
	require.Equal(t, 200, user.Quota)
	// A profile read before billing cannot restore that older balance.
	require.NoError(t, DecreaseUserQuota(617, 60, false))
	user.DisplayName = "updated-profile"
	require.NoError(t, user.Update(false))
	require.Equal(t, 140, userQuota(t, 617))
	expireTestGrant(t, row)
	q, err := GetWalletQuota(617)
	require.NoError(t, err)
	require.Equal(t, 100, q)
}
