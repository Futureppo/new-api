package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserAffQuotaTestDB(t *testing.T) {
	t.Helper()
	db := setupUserListTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	previousReward := common.QuotaForInviter
	common.QuotaForInviter = 37
	t.Cleanup(func() { common.QuotaForInviter = previousReward })
}

func seedAffQuotaUser(t *testing.T, name string, nodeLocID string) User {
	t.Helper()
	user := User{
		Username: name, AffCode: name, Password: "password-hash", DisplayName: name,
		Email: name + "@example.com", GitHubId: "github-" + name, NodeLocId: nodeLocID,
		Quota: 123, AffQuota: 2 * int(common.QuotaPerUnit), AffCount: 3,
		AffHistoryQuota: 10 * int(common.QuotaPerUnit), UsedQuota: 45, RequestCount: 6,
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func TestAffQuotaOperationsPreserveUserFields(t *testing.T) {
	for _, operation := range []string{"transfer", "invite"} {
		t.Run(operation, func(t *testing.T) {
			setupUserAffQuotaTestDB(t)
			// A previous full-row Save may already have stored an empty NodeLoc ID.
			legacy := seedAffQuotaUser(t, "legacy", "")
			require.NoError(t, DB.Model(&User{}).Where("id = ?", legacy.Id).Update("nodeloc_id", "").Error)
			for i, nodeLocID := range []string{"", "", "bound-id"} {
				user := seedAffQuotaUser(t, fmt.Sprintf("user%d", i), nodeLocID)
				expected := user
				if operation == "transfer" {
					// Transferring the entire balance must persist aff_quota = 0.
					require.NoError(t, user.TransferAffQuotaToQuota(user.AffQuota))
					expected.Quota += expected.AffQuota
					expected.AffQuota = 0
					require.Equal(t, expected.Quota, user.Quota)
					require.Zero(t, user.AffQuota)
				} else {
					require.NoError(t, inviteUser(user.Id))
					expected.AffCount++
					expected.AffQuota += common.QuotaForInviter
					expected.AffHistoryQuota += common.QuotaForInviter
				}
				var stored User
				require.NoError(t, DB.First(&stored, user.Id).Error)
				require.Equal(t, expected, stored)
				if nodeLocID == "" {
					var count int64
					require.NoError(t, DB.Model(&User{}).Where("id = ? AND nodeloc_id IS NULL", user.Id).Count(&count).Error)
					require.EqualValues(t, 1, count, "unbound NodeLoc IDs must stay NULL")
				}
			}
		})
	}
}

func TestTransferAffQuotaRejectsInvalidRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		quota int
	}{
		{"negative", -1},
		{"zero", 0},
		{"below_minimum", int(common.QuotaPerUnit) - 1},
		{"insufficient", 3 * int(common.QuotaPerUnit)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupUserAffQuotaTestDB(t)
			user := seedAffQuotaUser(t, "invalid", "")
			before := user
			require.Error(t, user.TransferAffQuotaToQuota(tc.quota))
			require.Equal(t, before, user)
			var stored User
			require.NoError(t, DB.First(&stored, user.Id).Error)
			require.Equal(t, before, stored)
		})
	}
}

func TestAffQuotaOperationsRejectMissingUsers(t *testing.T) {
	setupUserAffQuotaTestDB(t)
	deleted := seedAffQuotaUser(t, "deleted", "")
	require.NoError(t, DB.Delete(&deleted).Error)
	for _, id := range []int{0, deleted.Id, deleted.Id + 1} {
		user := User{Id: id}
		require.Error(t, user.TransferAffQuotaToQuota(int(common.QuotaPerUnit)))
		require.Error(t, inviteUser(id))
	}
	var count int64
	require.NoError(t, DB.Unscoped().Model(&User{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "quota updates must never create users")
}

func TestTransferAffQuotaUsesCurrentBalance(t *testing.T) {
	setupUserAffQuotaTestDB(t)
	user := seedAffQuotaUser(t, "stale", "bound-id")
	unit := int(common.QuotaPerUnit)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"quota": gorm.Expr("quota + ?", 77), "aff_quota": unit, "display_name": "updated",
	}).Error)
	require.NoError(t, user.TransferAffQuotaToQuota(unit))
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, 123+77+unit, stored.Quota)
	require.Zero(t, stored.AffQuota)
	require.Equal(t, "updated", stored.DisplayName)
	require.ErrorContains(t, user.TransferAffQuotaToQuota(unit), "邀请额度不足")
}

func TestTransferAffQuotaRollsBackOnDatabaseError(t *testing.T) {
	setupUserAffQuotaTestDB(t)
	user := seedAffQuotaUser(t, "rollback", "")
	before := user
	injected := errors.New("balance read failed")
	const callback = "test:aff_quota_read_failure"
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		tx.AddError(injected)
	}))
	t.Cleanup(func() { _ = DB.Callback().Query().Remove(callback) })
	require.ErrorIs(t, user.TransferAffQuotaToQuota(int(common.QuotaPerUnit)), injected)
	require.Equal(t, before, user)
	require.NoError(t, DB.Callback().Query().Remove(callback))
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, before, stored)
}

func TestTransferAffQuotaConcurrentRequests(t *testing.T) {
	setupUserAffQuotaTestDB(t)
	testTransferAffQuotaConcurrentRequests(t)
}

func testTransferAffQuotaConcurrentRequests(t *testing.T) {
	t.Helper()
	user := seedAffQuotaUser(t, "concurrent", "bound-id")
	unit := int(common.QuotaPerUnit)
	const available = 6
	const requests = 12
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("aff_quota", available*unit).Error)
	start := make(chan struct{})
	results := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			requestUser := User{Id: user.Id}
			results <- requestUser.TransferAffQuotaToQuota(unit)
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorContains(t, err, "邀请额度不足")
		}
	}
	require.Equal(t, available, successes)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, user.Quota+available*unit, stored.Quota)
	require.Zero(t, stored.AffQuota)
}
