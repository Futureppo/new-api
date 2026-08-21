package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// seedCheckinUser 创建一个带初始余额的用户。
func seedCheckinUser(t *testing.T, id int, quota int) *User {
	t.Helper()
	user := &User{
		Id:       id,
		Username: fmt.Sprintf("expiry_user_%d", id),
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
	return user
}

// seedCheckinRow 写入一条指定日期的签到记录。
func seedCheckinRow(t *testing.T, userId int, date string, awarded int) {
	t.Helper()
	start, _, err := checkinDateRange(date)
	require.NoError(t, err)
	require.NoError(t, DB.Create(&Checkin{
		UserId:       userId,
		CheckinDate:  date,
		QuotaAwarded: awarded,
		CreatedAt:    start + 3600,
	}).Error)
}

// seedConsumeLog 写入一条落在指定日期内的消费日志。
func seedConsumeLog(t *testing.T, userId int, date string, quota int) {
	t.Helper()
	start, _, err := checkinDateRange(date)
	require.NoError(t, err)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    userId,
		Type:      LogTypeConsume,
		Quota:     quota,
		CreatedAt: start + 7200,
	}).Error)
}

func userQuota(t *testing.T, id int) int {
	t.Helper()
	var u User
	require.NoError(t, DB.Select("quota").Where("id = ?", id).First(&u).Error)
	return u.Quota
}

func yesterday() string {
	return time.Now().AddDate(0, 0, -1).Format("2006-01-02")
}

func TestSettleCheckinDateUnusedModeReclaimsOnlyLeftover(t *testing.T) {
	truncateTables(t)
	date := yesterday()

	// 发放 100，当天消耗 30，应回收 70
	seedCheckinUser(t, 601, 500)
	seedCheckinRow(t, 601, date, 100)
	seedConsumeLog(t, 601, date, 30)

	settled, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeUnused, 100)
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, int64(70), reclaimed)
	require.Equal(t, 430, userQuota(t, 601))
}

func TestSettleCheckinDateAllModeReclaimsFullGrant(t *testing.T) {
	truncateTables(t)
	date := yesterday()

	// 全额回收模式下，消耗记录不影响回收量
	seedCheckinUser(t, 602, 500)
	seedCheckinRow(t, 602, date, 100)
	seedConsumeLog(t, 602, date, 30)

	settled, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeAll, 100)
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, int64(100), reclaimed)
	require.Equal(t, 400, userQuota(t, 602))
}

func TestSettleCheckinDateFullySpentReclaimsNothing(t *testing.T) {
	truncateTables(t)
	date := yesterday()

	// 消耗超过发放，不应回收，也不应变成负数
	seedCheckinUser(t, 603, 500)
	seedCheckinRow(t, 603, date, 100)
	seedConsumeLog(t, 603, date, 250)

	settled, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeUnused, 100)
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, int64(0), reclaimed)
	require.Equal(t, 500, userQuota(t, 603))
}

func TestSettleCheckinDateNeverDrivesBalanceNegative(t *testing.T) {
	truncateTables(t)
	date := yesterday()

	// 余额低于应回收量时只回收到 0 为止
	seedCheckinUser(t, 604, 20)
	seedCheckinRow(t, 604, date, 100)

	settled, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeAll, 100)
	require.NoError(t, err)
	require.Equal(t, 1, settled)
	require.Equal(t, int64(20), reclaimed)
	require.Equal(t, 0, userQuota(t, 604))
}

func TestSettleCheckinDateIsIdempotent(t *testing.T) {
	truncateTables(t)
	date := yesterday()

	seedCheckinUser(t, 605, 500)
	seedCheckinRow(t, 605, date, 100)

	_, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeAll, 100)
	require.NoError(t, err)
	require.Equal(t, int64(100), reclaimed)
	require.Equal(t, 400, userQuota(t, 605))

	// 第二次执行不得重复扣减
	settled, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeAll, 100)
	require.NoError(t, err)
	require.Equal(t, 0, settled)
	require.Equal(t, int64(0), reclaimed)
	require.Equal(t, 400, userQuota(t, 605))
}

func TestSettleCheckinRecordsLedgerFields(t *testing.T) {
	truncateTables(t)
	date := yesterday()

	seedCheckinUser(t, 606, 500)
	seedCheckinRow(t, 606, date, 100)
	seedConsumeLog(t, 606, date, 40)

	_, _, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeUnused, 100)
	require.NoError(t, err)

	var row Checkin
	require.NoError(t, DB.Where("user_id = ? AND checkin_date = ?", 606, date).First(&row).Error)
	require.Equal(t, 60, row.ExpiredQuota)
	require.NotZero(t, row.SettledAt)
}

func TestOldestUnsettledCheckinDateIgnoresToday(t *testing.T) {
	truncateTables(t)
	today := time.Now().Format("2006-01-02")
	date := yesterday()

	seedCheckinUser(t, 607, 500)
	seedCheckinRow(t, 607, today, 100)
	seedCheckinRow(t, 607, date, 100)

	got, err := OldestUnsettledCheckinDate(today)
	require.NoError(t, err)
	require.Equal(t, date, got, "当天的签到不应参与清算")

	// 结算完昨天之后就没有待处理日期了，今天的记录仍原样保留
	_, _, err = SettleCheckinDate(date, operation_setting.CheckinExpireModeAll, 100)
	require.NoError(t, err)

	got, err = OldestUnsettledCheckinDate(today)
	require.NoError(t, err)
	require.Equal(t, "", got)

	var row Checkin
	require.NoError(t, DB.Where("user_id = ? AND checkin_date = ?", 607, today).First(&row).Error)
	require.Zero(t, row.SettledAt)
}

func TestWriteOffCheckinDateMarksSettledWithoutReclaiming(t *testing.T) {
	truncateTables(t)
	stale := time.Now().AddDate(0, 0, -30).Format("2006-01-02")

	seedCheckinUser(t, 608, 500)
	seedCheckinRow(t, 608, stale, 100)

	n, err := WriteOffCheckinDate(stale, 100)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.Equal(t, 500, userQuota(t, 608), "历史积压不应被追溯扣减")

	var row Checkin
	require.NoError(t, DB.Where("user_id = ? AND checkin_date = ?", 608, stale).First(&row).Error)
	require.NotZero(t, row.SettledAt)
	require.Equal(t, 0, row.ExpiredQuota)
}

func TestSettleCheckinDateOnlyCountsSameDayConsumption(t *testing.T) {
	truncateTables(t)
	date := yesterday()
	other := time.Now().AddDate(0, 0, -5).Format("2006-01-02")

	// 别的日期的消耗不能抵扣当日签到额度
	seedCheckinUser(t, 609, 500)
	seedCheckinRow(t, 609, date, 100)
	seedConsumeLog(t, 609, other, 90)

	_, reclaimed, err := SettleCheckinDate(date, operation_setting.CheckinExpireModeUnused, 100)
	require.NoError(t, err)
	require.Equal(t, int64(100), reclaimed)
}
