package operation_setting

import (
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

// CheckinSetting 签到功能配置
type CheckinSetting struct {
	Enabled        bool `json:"enabled"`         // 是否启用签到功能
	MinQuota       int  `json:"min_quota"`       // 签到最小额度奖励
	MaxQuota       int  `json:"max_quota"`       // 签到最大额度奖励
	SpecialEnabled bool `json:"special_enabled"` // 是否启用特殊星期签到奖励
	SpecialWeekday int  `json:"special_weekday"` // 特殊星期，1=周一，7=周日
	SpecialQuota   int  `json:"special_quota"`   // 特殊星期固定额度奖励
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:        false, // 默认关闭
	MinQuota:       1000,  // 默认最小额度 1000 (约 0.002 USD)
	MaxQuota:       10000, // 默认最大额度 10000 (约 0.02 USD)
	SpecialEnabled: false,
	SpecialWeekday: 1,
	SpecialQuota:   0,
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("checkin_setting", &checkinSetting)
}

// GetCheckinSetting 获取签到配置
func GetCheckinSetting() *CheckinSetting {
	return &checkinSetting
}

// IsCheckinEnabled 是否启用签到功能
func IsCheckinEnabled() bool {
	return checkinSetting.Enabled
}

// GetCheckinQuotaRange 获取签到额度范围
func GetCheckinQuotaRange() (min, max int) {
	return checkinSetting.MinQuota, checkinSetting.MaxQuota
}

// CheckinWeekday 将 Go 的 Weekday 映射为签到配置使用的 1=周一 ... 7=周日。
func CheckinWeekday(now time.Time) int {
	weekday := int(now.Weekday())
	if weekday == 0 {
		return 7
	}
	return weekday
}

// IsSpecialRewardDay 判断指定时间是否命中特殊星期固定奖励。
func (setting CheckinSetting) IsSpecialRewardDay(now time.Time) bool {
	return setting.SpecialEnabled &&
		setting.SpecialWeekday >= 1 &&
		setting.SpecialWeekday <= 7 &&
		setting.SpecialWeekday == CheckinWeekday(now)
}

// RewardQuota 获取指定时间的签到奖励额度，特殊星期命中时覆盖随机奖励。
func (setting CheckinSetting) RewardQuota(now time.Time) int {
	if setting.IsSpecialRewardDay(now) {
		return setting.SpecialQuota
	}

	quotaAwarded := setting.MinQuota
	if setting.MaxQuota > setting.MinQuota {
		quotaAwarded = setting.MinQuota + rand.Intn(setting.MaxQuota-setting.MinQuota+1)
	}
	return quotaAwarded
}
