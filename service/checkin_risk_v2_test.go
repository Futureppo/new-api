package service

import (
	"fmt"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCheckinRiskActiveUserFixedMidnightIsNotPenalised(t *testing.T) {
	setupCheckinTestDB(t)
	for d := 1; d <= 10; d++ {
		seedCheckinAt(t, 9501, d, 5)
	}
	for d := 1; d <= 3; d++ {
		require.NoError(t, model.LOG_DB.Create(&model.Log{UserId: 9501, Type: model.LogTypeConsume, Quota: 100, CreatedAt: time.Now().AddDate(0, 0, -d).Unix()}).Error)
	}
	now := time.Now()
	now = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 5, 0, time.Local)
	require.Equal(t, checkinBehaviorMaxScore, checkinBehaviorScore(9501, now))
}
func TestCheckinRiskFakeOriginDoesNotEarnTrust(t *testing.T) {
	h := browserHeaders()
	h["Origin"] = "https://unrelated.invalid"
	h["Referer"] = "https://unrelated.invalid/personal"
	require.Less(t, clientEnvironmentScore(contextWithHeaders(h)), checkinHeaderMaxScore)
}
func TestCheckinRiskSafariPrivacyHeadersAreNotRequired(t *testing.T) {
	h := browserHeaders()
	h["User-Agent"] = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1"
	for _, key := range []string{"Sec-Ch-Ua", "Sec-Fetch-Mode", "Sec-Fetch-Site", "Sec-Fetch-Dest", "Accept-Encoding"} {
		delete(h, key)
	}
	require.Equal(t, checkinHeaderMaxScore, clientEnvironmentScore(contextWithHeaders(h)))
}
func TestCheckinRiskZeroQuotaLogIsNotUsageEvidence(t *testing.T) {
	setupCheckinTestDB(t)
	require.NoError(t, model.LOG_DB.Create(&model.Log{UserId: 9504, Type: model.LogTypeConsume, Quota: 0, CreatedAt: time.Now().Unix()}).Error)
	used, err := model.UserHasConsumptionSince(9504, time.Now().Add(-time.Hour).Unix())
	require.NoError(t, err)
	require.False(t, used)
}

func TestCheckinRiskCorrelatedAccountsCannotHideBehindBrowserHeaders(t *testing.T) {
	setupCheckinTestDB(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = true
	t.Cleanup(func() { *setting = old })
	c := contextWithHeaders(browserHeaders())
	now := time.Now()
	network := checkinRiskHash("network", c.ClientIP())
	client := checkinRiskHash("client", strings.ToLower(c.Request.UserAgent()))
	device := EnsureCheckinRiskDevice(c)
	for i := 0; i < 12; i++ {
		require.NoError(t, model.DB.Create(&model.Checkin{UserId: 9600 + i, CheckinDate: now.Format("2006-01-02"), CreatedAt: now.Unix() - int64(i), RiskNetwork: network, RiskClient: client, RiskDevice: device}).Error)
	}
	risk := assessCheckinRiskAt(c, 9700, now)
	require.Less(t, risk.Score, 50)
	require.Contains(t, risk.Evidence.Detail, "multi_account_device")
	require.Contains(t, risk.Evidence.Detail, "synchronized_account_cluster")
}
func TestCheckinRiskSharedNetworkAloneDoesNotPenalise(t *testing.T) {
	setupCheckinTestDB(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = true
	t.Cleanup(func() { *setting = old })
	c := contextWithHeaders(browserHeaders())
	now := time.Now()
	network := checkinRiskHash("network", c.ClientIP())
	for i := 0; i < 20; i++ {
		require.NoError(t, model.DB.Create(&model.Checkin{UserId: 9800 + i, CheckinDate: now.Format("2006-01-02"), CreatedAt: now.Unix() - 1, RiskNetwork: network, RiskClient: fmt.Sprint(i), RiskDevice: fmt.Sprint(i)}).Error)
	}
	require.Equal(t, 100, assessCheckinRiskAt(c, 9900, now).Score)
}
func TestCheckinRiskForgedDeviceMarkerCannotImpersonateOthers(t *testing.T) {
	c := contextWithHeaders(browserHeaders())
	c.Request.AddCookie(&http.Cookie{Name: "checkin_device", Value: strings.Repeat("a", 32) + "." + strings.Repeat("0", 64)})
	got := EnsureCheckinRiskDevice(c)
	require.NotEqual(t, checkinRiskHash("device", strings.Repeat("a", 32)+"."+strings.Repeat("0", 64)), got)
	require.NotEmpty(t, got)
}

func TestCheckinRiskSharedBrowserVersionBehindNATIsNotEnough(t *testing.T) {
	setupCheckinTestDB(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = true
	t.Cleanup(func() { *setting = old })
	c := contextWithHeaders(browserHeaders())
	now := time.Now()
	network := checkinRiskHash("network", c.ClientIP())
	client := checkinRiskHash("client", strings.ToLower(c.Request.UserAgent()))
	for i := 0; i < 20; i++ {
		require.NoError(t, model.DB.Create(&model.Checkin{UserId: 10000 + i, CheckinDate: now.Format("2006-01-02"), CreatedAt: now.Unix() - 1, RiskNetwork: network, RiskClient: client, RiskDevice: fmt.Sprint(i)}).Error)
	}
	require.Equal(t, 100, assessCheckinRiskAt(c, 10100, now).Score)
}
func TestCheckinRiskBusyUserKeepsMultiDayUsageEvidence(t *testing.T) {
	setupCheckinTestDB(t)
	now := time.Now()
	require.NoError(t, model.LOG_DB.Create(&model.Log{UserId: 10101, Type: model.LogTypeConsume, Quota: 10, CreatedAt: now.AddDate(0, 0, -1).Unix()}).Error)
	for i := 0; i < 220; i++ {
		require.NoError(t, model.LOG_DB.Create(&model.Log{UserId: 10101, Type: model.LogTypeConsume, Quota: 10, CreatedAt: now.Unix()}).Error)
	}
	usage, err := model.CheckinUsageSince(10101, now.AddDate(0, 0, -7).Unix())
	require.NoError(t, err)
	require.GreaterOrEqual(t, usage.Days, 2)
}
