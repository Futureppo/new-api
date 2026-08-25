package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func createCreemEmailTestUser(t *testing.T, username string, email string) User {
	t.Helper()
	user := User{
		Username:    username,
		DisplayName: username,
		Email:       email,
		Status:      common.UserStatusEnabled,
		AffCode:     username + "-aff",
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func createCreemEmailTestTopUp(t *testing.T, tradeNo string, userID int) {
	t.Helper()
	topUp := TopUp{
		UserId:          userID,
		Amount:          25,
		Money:           1,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentProviderCreem,
		PaymentProvider: PaymentProviderCreem,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())
}

func TestRechargeCreemSkipsConflictingEmailWithoutFailingPayment(t *testing.T) {
	truncateTables(t)
	originalSetting := common.EmailCaseInsensitiveEnabled
	common.EmailCaseInsensitiveEnabled = true
	t.Cleanup(func() { common.EmailCaseInsensitiveEnabled = originalSetting })

	createCreemEmailTestUser(t, "existing-email-owner", "Buyer@Example.com")
	topupUser := createCreemEmailTestUser(t, "creem-topup-user", "")
	createCreemEmailTestTopUp(t, "creem-email-conflict", topupUser.Id)

	require.NoError(t, RechargeCreem("creem-email-conflict", "buyer@example.COM", "Buyer", "127.0.0.1"))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, topupUser.Id).Error)
	require.Empty(t, reloaded.Email)
	require.Equal(t, 25, reloaded.Quota)
	require.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo("creem-email-conflict").Status)
}

func TestRechargeCreemPreservesUniqueEmailCase(t *testing.T) {
	truncateTables(t)
	originalSetting := common.EmailCaseInsensitiveEnabled
	common.EmailCaseInsensitiveEnabled = true
	t.Cleanup(func() { common.EmailCaseInsensitiveEnabled = originalSetting })

	topupUser := createCreemEmailTestUser(t, "creem-unique-email", "")
	createCreemEmailTestTopUp(t, "creem-email-unique", topupUser.Id)

	require.NoError(t, RechargeCreem("creem-email-unique", " Unique.Buyer@Example.com ", "Buyer", "127.0.0.1"))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, topupUser.Id).Error)
	require.Equal(t, "Unique.Buyer@Example.com", reloaded.Email)
}
