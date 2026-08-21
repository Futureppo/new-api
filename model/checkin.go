package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// Checkin 签到记录
type Checkin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: YYYY-MM-DD
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	// 以下字段服务于「签到额度当日有效」，未启用该功能时保持为 0
	ExpiredQuota int   `json:"expired_quota" gorm:"not null;default:0"`           // 清算时实际回收的额度
	SettledAt    int64 `json:"settled_at" gorm:"bigint;not null;default:0;index"` // 清算时间戳，0 表示尚未清算
}

// CheckinRecord 用于API返回的签到记录（不包含敏感字段）
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetUserCheckinRecords 获取用户在指定日期范围内的签到记录
func GetUserCheckinRecords(userId int, startDate, endDate string) ([]Checkin, error) {
	var records []Checkin
	err := DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// UserCheckin 执行用户签到，不做客户端环境判定。
func UserCheckin(userId int) (*Checkin, error) {
	return UserCheckinWithClientScore(userId, 100)
}

// UserCheckinWithClientScore 执行用户签到，clientScore（0-100）用于压制
// 非浏览器环境拿到的奖励，详见 CheckinSetting.ApplyClientScore。
// MySQL 和 PostgreSQL 使用事务保证原子性
// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
func UserCheckinWithClientScore(userId int, clientScore int) (*Checkin, error) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		return nil, errors.New("签到功能未启用")
	}

	// 检查今天是否已签到
	hasChecked, err := HasCheckedInToday(userId)
	if err != nil {
		return nil, err
	}
	if hasChecked {
		return nil, errors.New("今日已签到")
	}

	now := time.Now()
	quotaAwarded := setting.ApplyClientScore(setting.RewardQuota(now), clientScore)
	today := now.Format("2006-01-02")
	checkin := &Checkin{
		UserId:       userId,
		CheckinDate:  today,
		QuotaAwarded: quotaAwarded,
		CreatedAt:    now.Unix(),
	}

	// 根据数据库类型选择不同的策略
	if common.UsingSQLite {
		// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
		return userCheckinWithoutTransaction(checkin, userId, quotaAwarded)
	}

	// MySQL 和 PostgreSQL 支持事务，使用事务保证原子性
	return userCheckinWithTransaction(checkin, userId, quotaAwarded)
}

// userCheckinWithTransaction 使用事务执行签到（适用于 MySQL 和 PostgreSQL）
func userCheckinWithTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	err := DB.Transaction(func(tx *gorm.DB) error {
		// 步骤1: 创建签到记录
		// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}

		// 步骤2: 在事务中增加用户额度
		if err := tx.Model(&User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", quotaAwarded)).Error; err != nil {
			return errors.New("签到失败：更新额度出错")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// 事务成功后，异步更新缓存
	go func() {
		_ = cacheIncrUserQuota(userId, int64(quotaAwarded))
	}()

	return checkin, nil
}

// userCheckinWithoutTransaction 不使用事务执行签到（适用于 SQLite）
func userCheckinWithoutTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	// 步骤1: 创建签到记录
	// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
	if err := DB.Create(checkin).Error; err != nil {
		return nil, errors.New("签到失败，请稍后重试")
	}

	// 步骤2: 增加用户额度
	// 使用 db=true 强制直接写入数据库，不使用批量更新
	if err := IncreaseUserQuota(userId, quotaAwarded, true); err != nil {
		// 如果增加额度失败，需要回滚签到记录
		DB.Delete(checkin)
		return nil, errors.New("签到失败：更新额度出错")
	}

	return checkin, nil
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]interface{}, error) {
	// 获取指定月份的所有签到记录
	startDate := month + "-01"
	endDate := month + "-31"

	records, err := GetUserCheckinRecords(userId, startDate, endDate)
	if err != nil {
		return nil, err
	}

	// 转换为不包含敏感字段的记录
	checkinRecords := make([]CheckinRecord, len(records))
	for i, r := range records {
		checkinRecords[i] = CheckinRecord{
			CheckinDate:  r.CheckinDate,
			QuotaAwarded: r.QuotaAwarded,
		}
	}

	// 检查今天是否已签到
	hasCheckedToday, _ := HasCheckedInToday(userId)

	// 获取用户所有时间的签到统计
	var totalCheckins int64
	var totalQuota int64
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins)
	DB.Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota)

	return map[string]interface{}{
		"total_quota":      totalQuota,      // 所有时间累计获得的额度
		"total_checkins":   totalCheckins,   // 所有时间累计签到次数
		"checkin_count":    len(records),    // 本月签到次数
		"checked_in_today": hasCheckedToday, // 今天是否已签到
		"records":          checkinRecords,  // 本月签到记录详情（不含id和user_id）
	}, nil
}

// ---------------------------------------------------------------------------
// 签到额度当日有效：次日清算，回收当天签到发放中未被消耗的部分。
//
// 设计取舍：users.quota 是单一标量，无法区分「签到来的钱」和「充值来的钱」，
// 因此这里不改动余额结构，而是把已有的 checkins 表当作发放台账，
// 按天结算并在 checkins 上标记，避免重复回收。
// ---------------------------------------------------------------------------

// checkinDateRange 把 YYYY-MM-DD 转为本地时区的 [start, end) 秒级时间戳。
// 使用本地时区是因为签到写入时用的就是 time.Now()，两侧必须一致。
func checkinDateRange(date string) (int64, int64, error) {
	start, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return 0, 0, err
	}
	return start.Unix(), start.AddDate(0, 0, 1).Unix(), nil
}

// OldestUnsettledCheckinDate 返回早于 today 且仍有未清算记录的最早日期。
// 没有待清算数据时返回空串。
func OldestUnsettledCheckinDate(today string) (string, error) {
	var dates []string
	err := DB.Model(&Checkin{}).
		Where("settled_at = 0 AND checkin_date < ?", today).
		Order("checkin_date asc").
		Limit(1).
		Pluck("checkin_date", &dates).Error
	if err != nil {
		return "", err
	}
	if len(dates) == 0 {
		return "", nil
	}
	return dates[0], nil
}

// checkinDaySpent 统计这批用户在指定日期内的消费额度。
// 全额回收模式下无需统计，直接返回空表。
func checkinDaySpent(date string, mode string, rows []Checkin) (map[int]int64, error) {
	spent := make(map[int]int64, len(rows))
	if mode == operation_setting.CheckinExpireModeAll {
		return spent, nil
	}
	start, end, err := checkinDateRange(date)
	if err != nil {
		return nil, err
	}
	userIds := make([]int, 0, len(rows))
	for _, r := range rows {
		userIds = append(userIds, r.UserId)
	}
	var agg []struct {
		UserId int   `gorm:"column:user_id"`
		Total  int64 `gorm:"column:total"`
	}
	// 只统计消费日志。退款(LogTypeRefund)不在此抵扣：回收量最终仍受用户实际余额约束，
	// 少算只会让回收更保守，不会多扣用户的钱。
	if err := LOG_DB.Model(&Log{}).
		Select("user_id, COALESCE(SUM(quota), 0) AS total").
		Where("type = ? AND created_at >= ? AND created_at < ? AND user_id IN ?",
			LogTypeConsume, start, end, userIds).
		Group("user_id").
		Scan(&agg).Error; err != nil {
		return nil, err
	}
	for _, a := range agg {
		spent[a.UserId] = a.Total
	}
	return spent, nil
}

// reclaimUserQuota 从用户余额扣减至多 want 的额度，返回实际扣减量。
// 永远不会把余额扣成负数；并发消耗导致扣减失败时重读余额重试一次。
func reclaimUserQuota(userId int, want int64) int64 {
	if want <= 0 {
		return 0
	}
	for attempt := 0; attempt < 2; attempt++ {
		var current int64
		if err := DB.Model(&User{}).Where("id = ?", userId).
			Select("quota").Scan(&current).Error; err != nil {
			return 0
		}
		if current <= 0 {
			return 0
		}
		amount := want
		if current < amount {
			amount = current
		}
		// 相对更新 + 余额守卫：与 increaseUserQuota/decreaseUserQuota 的写法一致，
		// 因此不会和批量更新器(BatchUpdate)互相覆盖。
		res := DB.Model(&User{}).Where("id = ? AND quota >= ?", userId, amount).
			Update("quota", gorm.Expr("quota - ?", amount))
		if res.Error != nil {
			return 0
		}
		if res.RowsAffected > 0 {
			return amount
		}
	}
	return 0
}

// SettleCheckinDate 清算指定日期的签到额度，单次最多处理 limit 条。
// 返回已清算记录数和实际回收的额度总量。
func SettleCheckinDate(date string, mode string, limit int) (int, int64, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []Checkin
	if err := DB.Where("checkin_date = ? AND settled_at = 0", date).
		Order("id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, 0, err
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}

	spent, err := checkinDaySpent(date, mode, rows)
	if err != nil {
		return 0, 0, err
	}

	settled := 0
	var reclaimedTotal int64
	now := common.GetTimestamp()
	for _, row := range rows {
		want := int64(row.QuotaAwarded)
		if mode != operation_setting.CheckinExpireModeAll {
			want -= spent[row.UserId]
		}
		if want < 0 {
			want = 0
		}
		reclaimed := reclaimUserQuota(row.UserId, want)
		// 先标记已清算再落日志：即使随后崩溃也不会重复回收。
		if err := DB.Model(&Checkin{}).
			Where("id = ? AND settled_at = 0", row.Id).
			Updates(map[string]interface{}{
				"settled_at":    now,
				"expired_quota": reclaimed,
			}).Error; err != nil {
			return settled, reclaimedTotal, err
		}
		settled++
		reclaimedTotal += reclaimed
		if reclaimed > 0 {
			_ = InvalidateUserCache(row.UserId)
			RecordLog(row.UserId, LogTypeSystem, fmt.Sprintf(
				"签到额度当日有效：%s 发放 %s，已消耗后回收 %s",
				date, logger.LogQuota(row.QuotaAwarded), logger.LogQuota(int(reclaimed))))
		}
	}
	return settled, reclaimedTotal, nil
}

// WriteOffCheckinDate 把指定日期的未清算记录直接标记为已清算且不回收任何额度。
// 用于功能刚启用时跳过历史积压：这些额度发放时并未告知用户「当日有效」，
// 追溯扣减会让用户余额毫无预警地大幅缩水。
func WriteOffCheckinDate(date string, limit int) (int, error) {
	if limit <= 0 {
		limit = 500
	}
	var ids []int
	if err := DB.Model(&Checkin{}).
		Where("checkin_date = ? AND settled_at = 0", date).
		Order("id asc").Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := DB.Model(&Checkin{}).Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"settled_at":    common.GetTimestamp(),
			"expired_quota": 0,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

// RecentCheckinTimestamps 返回该用户最近 limit 次签到的时间戳，用于行为特征分析。
// 调用发生在本次签到写入之前，因此返回的都是历史记录。
func RecentCheckinTimestamps(userId int, limit int) ([]int64, error) {
	if limit <= 0 {
		limit = 14
	}
	var timestamps []int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND created_at > 0", userId).
		Order("created_at desc").
		Limit(limit).
		Pluck("created_at", &timestamps).Error
	return timestamps, err
}

// UserHasConsumptionSince 用户在给定时间点之后是否产生过实际消费。
func UserHasConsumptionSince(userId int, since int64) (bool, error) {
	var count int64
	err := LOG_DB.Model(&Log{}).
		Where("user_id = ? AND type = ? AND created_at >= ?", userId, LogTypeConsume, since).
		Limit(1).
		Count(&count).Error
	return count > 0, err
}

// FirstCheckinTimestamp 返回该用户最早一次签到的时间戳，0 表示从未签到。
// 用于判断账号是否「太新以至于没有可分析的行为历史」。
func FirstCheckinTimestamp(userId int) (int64, error) {
	var timestamps []int64
	err := DB.Model(&Checkin{}).
		Where("user_id = ? AND created_at > 0", userId).
		Order("created_at asc").Limit(1).
		Pluck("created_at", &timestamps).Error
	if err != nil || len(timestamps) == 0 {
		return 0, err
	}
	return timestamps[0], nil
}
