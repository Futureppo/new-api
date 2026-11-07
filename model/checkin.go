package model

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

// Checkin 签到记录
type Checkin struct {
	WalletTracked  bool   `json:"-" gorm:"not null;default:false"`
	RemainingQuota int    `json:"remaining_quota" gorm:"not null;default:0"`
	ExpiresAt      int64  `json:"expires_at" gorm:"not null;default:0"`
	RiskNetwork    string `json:"-" gorm:"type:varchar(64);index:idx_checkin_risk_network_client_time,priority:1"`
	RiskClient     string `json:"-" gorm:"type:varchar(64);index:idx_checkin_risk_network_client_time,priority:2"`
	RiskDevice     string `json:"-" gorm:"type:varchar(64);index:idx_checkin_risk_device_time,priority:1"`
	RiskScore      *int   `json:"-"`
	RiskEvidence   string `json:"-" gorm:"type:text"`
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId         int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate    string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: YYYY-MM-DD
	QuotaAwarded   int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt      int64  `json:"created_at" gorm:"bigint;index:idx_checkin_risk_network_client_time,priority:3;index:idx_checkin_risk_device_time,priority:2"`
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
// 签到记录与钱包入账在同一事务中提交，所有数据库使用相同资金语义。
func UserCheckinWithClientScore(userId int, clientScore int) (*Checkin, error) {
	return UserCheckinWithRisk(userId, clientScore, CheckinRiskEvidence{})
}

func UserCheckinWithRisk(userId int, clientScore int, risk CheckinRiskEvidence) (*Checkin, error) {
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
		RiskNetwork: risk.Network, RiskClient: risk.Client, RiskDevice: risk.Device, RiskScore: &clientScore, RiskEvidence: risk.Detail,
		UserId:       userId,
		CheckinDate:  today,
		QuotaAwarded: quotaAwarded,
		CreatedAt:    now.Unix(),
	}

	if setting.IsExpireEnabled() {
		checkin.WalletTracked = true
		checkin.RemainingQuota = quotaAwarded
		checkin.ExpiresAt = checkinExpiresAt(now)
	}
	return userCheckinWithTransaction(checkin, userId, quotaAwarded)
}

func userCheckinWithTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	_, err := walletUserTransaction(userId, func(tx *gorm.DB, u *User, _ int64) error {
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}
		return tx.Model(&User{}).Where("id = ?", u.Id).Update("quota", gorm.Expr("quota + ?", quotaAwarded)).Error
	})
	if err != nil {
		return nil, err
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
// checkinDateRange shares the configured local day with grant issuance.
func checkinDateRange(date string) (int64, int64, error) {
	start, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		return 0, 0, err
	}
	return start.Unix(), start.AddDate(0, 0, 1).Unix(), nil
}
func OldestUnsettledCheckinDate(today string) (string, error) {
	var dates []string
	err := DB.Model(&Checkin{}).Where("settled_at = 0 AND checkin_date < ?", today).Order("checkin_date asc").Limit(1).Pluck("checkin_date", &dates).Error
	if err != nil || len(dates) == 0 {
		return "", err
	}
	return dates[0], nil
}

// Legacy rows have no reliable source allocation and are written off without
// debiting a user's permanent quota. New rows expire only their tracked remainder,
// including when an older client still sends the legacy "all" mode.
func SettleCheckinDate(date, mode string, limit int) (int, int64, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []Checkin
	if err := DB.Where("checkin_date = ? AND settled_at = 0", date).Order("id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, 0, err
	}
	settled := 0
	var total int64
	// Legacy records have no source ledger. Mark them in a bounded batch without
	// scanning usage logs or opening a wallet transaction per historical row.
	var legacyIDs []int
	for _, row := range rows {
		if !row.WalletTracked {
			legacyIDs = append(legacyIDs, row.Id)
		}
	}
	if len(legacyIDs) > 0 {
		res := DB.Model(&Checkin{}).Where("id IN ? AND settled_at = 0 AND wallet_tracked = ?", legacyIDs, false).
			Updates(map[string]interface{}{"settled_at": common.GetTimestamp(), "expired_quota": 0})
		if res.Error != nil {
			return 0, 0, res.Error
		}
		settled += int(res.RowsAffected)
	}
	for _, row := range rows {
		if !row.WalletTracked {
			continue
		}
		expired, err := walletUserTransaction(row.UserId, nil)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Deleted accounts must not stop expiry for everyone else. Do not
			// reconstruct or debit a missing wallet.
			res := DB.Model(&Checkin{}).Where("id = ? AND settled_at = 0", row.Id).
				Updates(map[string]interface{}{"settled_at": common.GetTimestamp(), "remaining_quota": 0})
			if res.Error != nil {
				return settled, total, res.Error
			}
			settled += int(res.RowsAffected)
			continue
		}
		if err != nil {
			return settled, total, err
		}
		for _, grant := range expired {
			if grant.Id == row.Id {
				settled++
				total += int64(grant.ExpiredQuota)
			}
		}
	}
	return settled, total, nil
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
		Where("checkin_date = ? AND settled_at = 0 AND wallet_tracked = ?", date, false).
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
		Where("user_id = ? AND type = ? AND quota > 0 AND created_at >= ?", userId, LogTypeConsume, since).
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
