package operation_setting

import (
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/setting/config"
)

// 签到额度过期回收模式
const (
	// CheckinExpireModeUnused 仅回收当日签到未消耗的部分（签到额度优先消耗）
	CheckinExpireModeUnused = "unused"
	// CheckinExpireModeAll 次日全额回收当日签到发放，无论是否消耗
	CheckinExpireModeAll = "all"
)

// CheckinSetting 签到功能配置
type CheckinSetting struct {
	Enabled        bool   `json:"enabled"`         // 是否启用签到功能
	MinQuota       int    `json:"min_quota"`       // 签到最小额度奖励
	MaxQuota       int    `json:"max_quota"`       // 签到最大额度奖励
	SpecialEnabled bool   `json:"special_enabled"` // 是否启用特殊星期签到奖励
	SpecialWeekday int    `json:"special_weekday"` // 特殊星期，1=周一，7=周日
	SpecialQuota   int    `json:"special_quota"`   // 特殊星期固定额度奖励
	ExpireEnabled  bool   `json:"expire_enabled"`  // 是否启用签到额度当日有效（次日回收）
	ExpireMode     string `json:"expire_mode"`     // 回收模式，见 CheckinExpireMode*
	// ClientCheckEnabled 启用后，非浏览器环境的签到会被压低到区间下沿而非被拒绝。
	ClientCheckEnabled bool `json:"client_check_enabled"`
}

// 默认配置
var checkinSetting = CheckinSetting{
	Enabled:        false, // 默认关闭
	MinQuota:       1000,  // 默认最小额度 1000 (约 0.002 USD)
	MaxQuota:       10000, // 默认最大额度 10000 (约 0.02 USD)
	SpecialEnabled: false,
	SpecialWeekday: 1,
	SpecialQuota:   0,
	ExpireEnabled:  false, // 默认关闭，保持既有站点行为不变
	ExpireMode:     CheckinExpireModeUnused,

	ClientCheckEnabled: false,
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

// IsExpireEnabled 是否启用签到额度当日有效。签到功能本身关闭时该开关无意义。
func (setting CheckinSetting) IsExpireEnabled() bool {
	return setting.Enabled && setting.ExpireEnabled
}

// NormalizedExpireMode 返回归一化后的回收模式，未识别的值一律按 unused 处理。
func (setting CheckinSetting) NormalizedExpireMode() string {
	if setting.ExpireMode == CheckinExpireModeAll {
		return CheckinExpireModeAll
	}
	return CheckinExpireModeUnused
}

// ApplyClientScore 按客户端环境分压制签到奖励，score 取值 0-100。
//
// score=100 原样返回；score=0 压到 MinQuota；中间线性插值，不设阈值。
// 结果始终落在 [MinQuota, reward] 内，因此从响应上无法与一次「运气不好的」
// 正常签到区分开——这正是「压低而非拦截」的关键：被压制的一方拿不到任何
// 可用于验证绕过是否成功的信号。
//
// 特殊星期的固定奖励同样适用，避免脚本专挑大额日收割。
func (setting CheckinSetting) ApplyClientScore(reward int, score int) int {
	if score >= 100 {
		return reward
	}
	if score < 0 {
		score = 0
	}
	floor := setting.MinQuota
	if floor < 0 {
		floor = 0
	}
	if reward <= floor {
		return reward
	}
	return floor + int(int64(reward-floor)*int64(score)/100)
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
