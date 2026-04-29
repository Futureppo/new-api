package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestReserveChannelDailySuccessUnlimited(t *testing.T) {
	truncateTables(t)

	channel := &Channel{Name: "unlimited", DailySuccessLimit: 0}
	require.NoError(t, DB.Create(channel).Error)

	reservation, err := ReserveChannelDailySuccess(channel)
	require.NoError(t, err)
	require.Nil(t, reservation)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, channel.Id).Error)
	require.Equal(t, 0, reloaded.DailySuccessCount)
	require.Equal(t, "", reloaded.DailySuccessDate)
}

func TestReserveChannelDailySuccessLimitAndRelease(t *testing.T) {
	truncateTables(t)

	channel := &Channel{Name: "limited", DailySuccessLimit: 1}
	require.NoError(t, DB.Create(channel).Error)

	reservation, err := ReserveChannelDailySuccess(channel)
	require.NoError(t, err)
	require.NotNil(t, reservation)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, channel.Id).Error)
	require.Equal(t, 1, reloaded.DailySuccessCount)
	require.Equal(t, time.Now().Format("2006-01-02"), reloaded.DailySuccessDate)

	_, err = ReserveChannelDailySuccess(channel)
	require.ErrorIs(t, err, ErrChannelDailySuccessLimitExceeded)

	ReleaseChannelDailySuccess(reservation)
	require.NoError(t, DB.First(&reloaded, channel.Id).Error)
	require.Equal(t, 0, reloaded.DailySuccessCount)

	reservation, err = ReserveChannelDailySuccess(channel)
	require.NoError(t, err)
	require.NotNil(t, reservation)
}

func TestReserveChannelDailySuccessResetsStaleDate(t *testing.T) {
	truncateTables(t)

	channel := &Channel{
		Name:              "stale",
		DailySuccessLimit: 1,
		DailySuccessCount: 1,
		DailySuccessDate:  time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
	}
	require.NoError(t, DB.Create(channel).Error)

	reservation, err := ReserveChannelDailySuccess(channel)
	require.NoError(t, err)
	require.NotNil(t, reservation)

	var reloaded Channel
	require.NoError(t, DB.First(&reloaded, channel.Id).Error)
	require.Equal(t, 1, reloaded.DailySuccessCount)
	require.Equal(t, time.Now().Format("2006-01-02"), reloaded.DailySuccessDate)
}

func TestGetRandomSatisfiedChannelExcludesDailyLimitedChannel(t *testing.T) {
	truncateTables(t)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	highPriority := int64(10)
	lowPriority := int64(1)
	first := &Channel{Name: "first", Status: common.ChannelStatusEnabled}
	second := &Channel{Name: "second", Status: common.ChannelStatusEnabled}
	require.NoError(t, DB.Create(first).Error)
	require.NoError(t, DB.Create(second).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: first.Id,
		Enabled:   true,
		Priority:  &highPriority,
		Weight:    100,
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "gpt-4o",
		ChannelId: second.Id,
		Enabled:   true,
		Priority:  &lowPriority,
		Weight:    100,
	}).Error)

	selected, err := GetRandomSatisfiedChannelWithExclusions("default", "gpt-4o", 0, map[int]bool{first.Id: true})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, second.Id, selected.Id)
}
