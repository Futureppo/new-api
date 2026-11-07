package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seedBillingCheckin(t *testing.T) *model.Checkin {
	t.Helper()
	truncate(t)
	seedUser(t, 1, 200)
	seedToken(t, 1, 1, "checkin-billing", 1000)
	now := time.Now()
	row := &model.Checkin{UserId: 1, CheckinDate: now.Format("2006-01-02"), CreatedAt: now.Unix(), QuotaAwarded: 100, RemainingQuota: 100, WalletTracked: true, ExpiresAt: now.Add(time.Hour).Unix()}
	require.NoError(t, model.DB.Create(row).Error)
	return row
}
func expireBillingCheckin(t *testing.T, row *model.Checkin) {
	t.Helper()
	require.NoError(t, model.DB.Model(row).Updates(map[string]any{"checkin_date": time.Now().AddDate(0, 0, -1).Format("2006-01-02"), "expires_at": time.Now().Unix() - 1}).Error)
}

func TestCheckinBillingSessionRefundAcrossMidnight(t *testing.T) {
	row := seedBillingCheckin(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "checkin-billing", UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
	session, apiErr := NewBillingSession(c, info, 150)
	require.Nil(t, apiErr)
	require.Positive(t, info.WalletChargeId)
	expireBillingCheckin(t, row)
	session.RefundSync(c)
	session.RefundSync(c)
	require.Equal(t, 100, getUserQuota(t, 1))
	require.Equal(t, 1000, getTokenRemainQuota(t, 1))
}

func TestCheckinTaskRefundUsesPersistedCharge(t *testing.T) {
	row := seedBillingCheckin(t)
	charge, err := model.ReserveWalletCharge(1, 150)
	require.NoError(t, err)
	task := makeTask(1, 0, 150, 1, BillingSourceWallet, 0)
	task.PrivateData.WalletChargeId = charge.Id
	require.NoError(t, task.Insert())
	var loaded model.Task
	require.NoError(t, model.DB.First(&loaded, task.ID).Error)
	expireBillingCheckin(t, row)
	RefundTaskQuota(context.Background(), &loaded, "upstream failure")
	require.Equal(t, 100, getUserQuota(t, 1))
	var saved model.WalletCharge
	require.NoError(t, model.DB.First(&saved, charge.Id).Error)
	require.True(t, saved.Refunded)
}
