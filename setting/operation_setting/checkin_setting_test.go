package operation_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckinWeekdayMapping(t *testing.T) {
	require.Equal(t, 1, CheckinWeekday(time.Date(2026, 6, 22, 12, 0, 0, 0, time.Local)))
	require.Equal(t, 7, CheckinWeekday(time.Date(2026, 6, 28, 12, 0, 0, 0, time.Local)))
}

func TestCheckinRewardQuotaSpecialRules(t *testing.T) {
	monday := time.Date(2026, 6, 22, 12, 0, 0, 0, time.Local)
	tuesday := time.Date(2026, 6, 23, 12, 0, 0, 0, time.Local)
	sunday := time.Date(2026, 6, 28, 12, 0, 0, 0, time.Local)

	tests := []struct {
		name     string
		setting  CheckinSetting
		now      time.Time
		expected int
	}{
		{
			name: "special weekday overrides random range",
			setting: CheckinSetting{
				MinQuota:       100,
				MaxQuota:       100,
				SpecialEnabled: true,
				SpecialWeekday: 1,
				SpecialQuota:   500,
			},
			now:      monday,
			expected: 500,
		},
		{
			name: "non matching weekday keeps regular reward",
			setting: CheckinSetting{
				MinQuota:       100,
				MaxQuota:       100,
				SpecialEnabled: true,
				SpecialWeekday: 1,
				SpecialQuota:   500,
			},
			now:      tuesday,
			expected: 100,
		},
		{
			name: "disabled special rule keeps regular reward",
			setting: CheckinSetting{
				MinQuota:       100,
				MaxQuota:       100,
				SpecialEnabled: false,
				SpecialWeekday: 1,
				SpecialQuota:   500,
			},
			now:      monday,
			expected: 100,
		},
		{
			name: "sunday uses seven",
			setting: CheckinSetting{
				MinQuota:       100,
				MaxQuota:       100,
				SpecialEnabled: true,
				SpecialWeekday: 7,
				SpecialQuota:   700,
			},
			now:      sunday,
			expected: 700,
		},
		{
			name: "special zero quota is preserved",
			setting: CheckinSetting{
				MinQuota:       100,
				MaxQuota:       100,
				SpecialEnabled: true,
				SpecialWeekday: 1,
				SpecialQuota:   0,
			},
			now:      monday,
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, tc.setting.RewardQuota(tc.now))
		})
	}
}

func TestCheckinIsExpireEnabledRequiresCheckinEnabled(t *testing.T) {
	require.False(t, CheckinSetting{Enabled: false, ExpireEnabled: true}.IsExpireEnabled(),
		"签到功能关闭时过期开关应无效")
	require.False(t, CheckinSetting{Enabled: true, ExpireEnabled: false}.IsExpireEnabled())
	require.True(t, CheckinSetting{Enabled: true, ExpireEnabled: true}.IsExpireEnabled())
}

func TestCheckinNormalizedExpireMode(t *testing.T) {
	require.Equal(t, CheckinExpireModeAll,
		CheckinSetting{ExpireMode: CheckinExpireModeAll}.NormalizedExpireMode())
	require.Equal(t, CheckinExpireModeUnused,
		CheckinSetting{ExpireMode: CheckinExpireModeUnused}.NormalizedExpireMode())
	// 空值与非法值一律回落到更保守的 unused
	require.Equal(t, CheckinExpireModeUnused, CheckinSetting{}.NormalizedExpireMode())
	require.Equal(t, CheckinExpireModeUnused,
		CheckinSetting{ExpireMode: "bogus"}.NormalizedExpireMode())
}

func TestCheckinExpireDefaultsAreOff(t *testing.T) {
	// 默认必须关闭，避免升级后既有站点行为突变
	require.False(t, checkinSetting.ExpireEnabled)
	require.Equal(t, CheckinExpireModeUnused, checkinSetting.NormalizedExpireMode())
}

func TestCheckinApplyClientScorePinsScriptsToMinimum(t *testing.T) {
	s := CheckinSetting{MinQuota: 5000, MaxQuota: 5000000} // $0.01 - $10

	// 满分原样返回
	require.Equal(t, 1234567, s.ApplyClientScore(1234567, 100))
	require.Equal(t, 5000000, s.ApplyClientScore(5000000, 100))

	// 零分一律压到下沿，且下沿本身就是区间内的合法结果，
	// 因此对方无法从数值上区分「被识别」和「运气差」
	require.Equal(t, 5000, s.ApplyClientScore(1234567, 0))
	require.Equal(t, 5000, s.ApplyClientScore(5000000, 0))

	// 中间分线性插值，没有可被二分定位的跳变点
	require.Equal(t, 5000+(5000000-5000)/2, s.ApplyClientScore(5000000, 50))
	require.Equal(t, 5000+(5000000-5000)/4, s.ApplyClientScore(5000000, 25))

	// 越界输入按边界处理
	require.Equal(t, 5000, s.ApplyClientScore(5000000, -10))
	require.Equal(t, 5000000, s.ApplyClientScore(5000000, 150))
}

func TestCheckinApplyClientScoreNeverRaisesRewardOrGoesBelowFloor(t *testing.T) {
	s := CheckinSetting{MinQuota: 5000, MaxQuota: 5000000}
	// 奖励本来就不高于下沿时保持不变，不会被"抬高"
	require.Equal(t, 5000, s.ApplyClientScore(5000, 0))
	require.Equal(t, 3000, s.ApplyClientScore(3000, 0))
	// 任意分数下结果都落在 [reward 下沿, reward] 内
	for score := 0; score <= 100; score += 7 {
		got := s.ApplyClientScore(5000000, score)
		require.GreaterOrEqual(t, got, 5000)
		require.LessOrEqual(t, got, 5000000)
	}
}

func TestCheckinApplyClientScoreCoversSpecialWeekdayGrant(t *testing.T) {
	// 特殊星期的固定大额同样会被压制，避免脚本专挑大额日收割
	s := CheckinSetting{MinQuota: 5000, MaxQuota: 5000000, SpecialQuota: 25000000}
	require.Equal(t, 5000, s.ApplyClientScore(s.SpecialQuota, 0))
	require.Equal(t, 25000000, s.ApplyClientScore(s.SpecialQuota, 100))
}

func TestCheckinClientCheckDefaultsOff(t *testing.T) {
	require.False(t, checkinSetting.ClientCheckEnabled)
}
