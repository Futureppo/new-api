package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestUserCheckinSpecialWeekdayUpdatesQuotaAndPreventsRepeat(t *testing.T) {
	truncateTables(t)

	checkinSetting := operation_setting.GetCheckinSetting()
	oldCheckinSetting := *checkinSetting
	now := time.Now()
	checkinSetting.Enabled = true
	checkinSetting.MinQuota = 100
	checkinSetting.MaxQuota = 100
	checkinSetting.SpecialEnabled = true
	checkinSetting.SpecialWeekday = operation_setting.CheckinWeekday(now)
	checkinSetting.SpecialQuota = 321
	t.Cleanup(func() {
		*checkinSetting = oldCheckinSetting
	})

	user := &User{
		Id:       501,
		Username: "checkin_user",
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, DB.Create(user).Error)

	checkin, err := UserCheckin(user.Id)
	require.NoError(t, err)
	require.Equal(t, now.Format("2006-01-02"), checkin.CheckinDate)
	require.Equal(t, 321, checkin.QuotaAwarded)

	var reloaded User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&reloaded).Error)
	require.Equal(t, 321, reloaded.Quota)

	var count int64
	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Equal(t, int64(1), count)

	_, err = UserCheckin(user.Id)
	require.Error(t, err)
	require.Contains(t, err.Error(), "今日已签到")

	require.NoError(t, DB.Model(&Checkin{}).Where("user_id = ?", user.Id).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestUserCheckinPersistsZeroRiskScore(t *testing.T) {
	truncateTables(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	t.Cleanup(func() { *setting = old })
	setting.Enabled, setting.MinQuota, setting.MaxQuota, setting.SpecialEnabled = true, 10, 100, false
	user := seedCheckinUser(t, 517, 100)
	row, err := UserCheckinWithRisk(user.Id, 0, CheckinRiskEvidence{Detail: `{"version":2}`})
	require.NoError(t, err)
	var stored Checkin
	require.NoError(t, DB.First(&stored, row.Id).Error)
	require.NotNil(t, stored.RiskScore)
	require.Zero(t, *stored.RiskScore)
	require.Equal(t, 10, stored.QuotaAwarded)
}
