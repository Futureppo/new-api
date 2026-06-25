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
