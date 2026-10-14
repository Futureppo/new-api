package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestUserCacheFillCannotRestoreStateAfterInvalidation(t *testing.T) {
	address := os.Getenv("TEST_USER_DISABLE_REDIS_ADDR")
	if address == "" {
		t.Skip("set TEST_USER_DISABLE_REDIS_ADDR to use an isolated Redis test server")
	}
	setupUserDisableTestDB(t)
	oldClient := common.RDB
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(context.Background()).Err())
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() { common.RedisEnabled = false; common.RDB = oldClient; _ = client.Close() })
	user := User{Username: "cache-fill", AffCode: "cache-fill", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, invalidateUserCache(user.Id))
	t.Cleanup(func() { _ = invalidateUserCache(user.Id) })

	for _, permanent := range []bool{false, true} {
		// Capture an enabled snapshot before the ban, with no existing cache key.
		stale := user
		staleTicket := beginUserCacheFill(user.Id)
		require.NotEmpty(t, staleTicket)
		period := UserDisablePeriod{}
		if !permanent {
			period = UserDisablePeriod{Minutes: 10, Until: time.Now().Unix() + 600}
		}
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(period.Updates("probe ban")).Error)
		InvalidateUserAuthCaches(user.Id)
		require.NoError(t, finishUserCacheFill(stale, staleTicket))
		_, err := cacheGetUserBase(user.Id)
		require.Error(t, err, "an invalidated fill must not create a cache entry")
		banned, err := GetUserCache(user.Id)
		require.NoError(t, err)
		require.Equal(t, common.UserStatusDisabled, banned.Status)
		require.Equal(t, "probe ban", banned.DisableReason)
		require.Equal(t, period.Until, banned.DisableUntil)
		// A delayed old fill must not overwrite a newer, populated cache either.
		require.NoError(t, finishUserCacheFill(stale, staleTicket))
		cached, err := cacheGetUserBase(user.Id)
		require.NoError(t, err)
		require.Equal(t, common.UserStatusDisabled, cached.Status)

		var disabled User
		require.NoError(t, DB.First(&disabled, user.Id).Error)
		disabledTicket := beginUserCacheFill(user.Id)
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(UserEnableUpdates()).Error)
		InvalidateUserAuthCaches(user.Id)
		require.NoError(t, finishUserCacheFill(disabled, disabledTicket))
		enabled, err := GetUserCache(user.Id)
		require.NoError(t, err)
		require.Equal(t, common.UserStatusEnabled, enabled.Status)
		require.Empty(t, enabled.DisableReason)
	}
}
