package model

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserDisableTestDB(t *testing.T) {
	t.Helper()
	db := setupUserListTestDB(t)
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	require.NoError(t, db.AutoMigrate(&Token{}, &Log{}))
}

func seedTimedDisable(t *testing.T, name string, until int64) User {
	t.Helper()
	user := User{Username: name, AffCode: name, Password: "secret-hash", Status: common.UserStatusDisabled, Role: common.RoleCommonUser,
		DisableReason: "review", DisableDurationMinutes: 1, DisableUntil: until}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func TestUserDisablePeriodValidation(t *testing.T) {
	period, err := NewUserDisablePeriod(0, 1700000000)
	require.NoError(t, err)
	require.Zero(t, period.Until)
	period, err = NewUserDisablePeriod(60, 1700000000)
	require.NoError(t, err)
	require.Equal(t, int64(1700003600), period.Until)
	for _, minutes := range []int64{-1, math.MaxInt64, 5000000000} {
		_, err = NewUserDisablePeriod(minutes, 1700000000)
		require.Error(t, err)
	}
}

func TestUserDisableExpiryBoundaryAndIdempotency(t *testing.T) {
	setupUserDisableTestDB(t)
	user := seedTimedDisable(t, "boundary", 1700000060)
	require.NoError(t, ResolveUserDisableExpiry(&user, 1700000059))
	require.Equal(t, common.UserStatusDisabled, user.Status)
	require.NoError(t, ResolveUserDisableExpiry(&user, 1700000060))
	require.Equal(t, common.UserStatusEnabled, user.Status)
	require.Empty(t, user.DisableReason)
	require.Zero(t, user.DisableDurationMinutes)
	require.Zero(t, user.DisableUntil)
	require.NoError(t, ResolveUserDisableExpiry(&user, 1700000061))
	var logs []Log
	require.NoError(t, LOG_DB.Where("user_id = ? AND type = ?", user.Id, LogTypeSystem).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Contains(t, logs[0].Content, "review")
	require.Contains(t, logs[0].Content, "1 分钟")
}

func TestUserDisableExpiryPreservesNewBanAndDeletedUsers(t *testing.T) {
	for _, action := range []string{"new_timed", "new_permanent", "deleted", "early_enable"} {
		t.Run(action, func(t *testing.T) {
			setupUserDisableTestDB(t)
			stale := seedTimedDisable(t, action, 1700000060)
			switch action {
			case "new_timed":
				require.NoError(t, DB.Model(&User{}).Where("id = ?", stale.Id).Updates((UserDisablePeriod{Minutes: 20, Until: 1700001260}).Updates("new reason")).Error)
			case "new_permanent":
				require.NoError(t, DB.Model(&User{}).Where("id = ?", stale.Id).Updates((UserDisablePeriod{}).Updates("new reason")).Error)
			case "deleted":
				require.NoError(t, DB.Delete(&User{}, stale.Id).Error)
			case "early_enable":
				require.NoError(t, DB.Model(&User{}).Where("id = ?", stale.Id).Updates(UserEnableUpdates()).Error)
			}
			err := ResolveUserDisableExpiry(&stale, 1700000060)
			if action == "deleted" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			var current User
			require.NoError(t, DB.Unscoped().First(&current, stale.Id).Error)
			if action == "early_enable" {
				require.Equal(t, common.UserStatusEnabled, current.Status)
			} else {
				require.Equal(t, common.UserStatusDisabled, current.Status)
			}
			if action == "new_timed" {
				require.Equal(t, int64(1700001260), current.DisableUntil)
			}
			if action == "new_permanent" {
				require.Zero(t, current.DisableUntil)
				require.Equal(t, "new reason", current.DisableReason)
			}
			var count int64
			require.NoError(t, LOG_DB.Model(&Log{}).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestUserDisableSweepAndReadDoNotExposePasswords(t *testing.T) {
	setupUserDisableTestDB(t)
	now := time.Now().Unix()
	due := seedTimedDisable(t, "due", now-1)
	permanent := seedTimedDisable(t, "permanent", 0)
	future := seedTimedDisable(t, "future", now+120)
	deleted := seedTimedDisable(t, "deleted", now-1)
	require.NoError(t, DB.Delete(&deleted).Error)
	// An ordinary admin read restores the user without leaking the password.
	read, err := GetUserById(due.Id, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, read.Status)
	require.Empty(t, read.Password)
	other := seedTimedDisable(t, "startup-due", now-1)
	require.NoError(t, ExpireDueUserDisables(now))
	for _, id := range []int{permanent.Id, future.Id, deleted.Id} {
		var user User
		require.NoError(t, DB.Unscoped().First(&user, id).Error)
		require.Equal(t, common.UserStatusDisabled, user.Status)
	}
	read, err = GetUserById(other.Id, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, read.Status)
}

func TestUserDisablePasswordAndIdentityLogin(t *testing.T) {
	setupUserDisableTestDB(t)
	now := time.Now().Unix()
	user := seedTimedDisable(t, "login", now+60)
	hash, err := common.Password2Hash("password123")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
		"password": hash, "email": "login@example.com", "github_id": "github", "discord_id": "discord", "oidc_id": "oidc", "wechat_id": "wechat", "telegram_id": "telegram", "linux_do_id": "linuxdo",
	}).Error)
	login := User{Username: user.Username, Password: "password123"}
	require.ErrorIs(t, login.ValidateAndFill(), ErrUserDisabled)
	loaders := []func() error{
		func() error {
			login = User{Username: user.Username, Password: "password123"}
			return login.ValidateAndFill()
		},
		func() error { login = User{Id: user.Id}; return login.FillUserById() },
		func() error { login = User{Email: "login@example.com"}; return login.FillUserByEmail() },
		func() error { login = User{GitHubId: "github"}; return login.FillUserByGitHubId() },
		func() error { login = User{DiscordId: "discord"}; return login.FillUserByDiscordId() },
		func() error { login = User{OidcId: "oidc"}; return login.FillUserByOidcId() },
		func() error { login = User{WeChatId: "wechat"}; return login.FillUserByWeChatId() },
		func() error { login = User{TelegramId: "telegram"}; return login.FillUserByTelegramId() },
		func() error { login = User{LinuxDOId: "linuxdo"}; return login.FillUserByLinuxDOId() },
	}
	for _, load := range loaders {
		require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates((UserDisablePeriod{Minutes: 1, Until: now - 1}).Updates("review")).Error)
		require.NoError(t, load())
		require.Equal(t, common.UserStatusEnabled, login.Status)
	}
}

func TestUserDisableRedisExpiredCacheRechecksDatabase(t *testing.T) {
	address := os.Getenv("TEST_USER_DISABLE_REDIS_ADDR")
	if address == "" {
		t.Skip("set TEST_USER_DISABLE_REDIS_ADDR to use an isolated Redis test server")
	}
	setupUserDisableTestDB(t)
	oldClient := common.RDB
	client := redis.NewClient(&redis.Options{Addr: address})
	require.NoError(t, client.Ping(context.Background()).Err())
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() { common.RedisEnabled = false; common.RDB = oldClient; _ = client.Close() })
	user := seedTimedDisable(t, "redis", time.Now().Unix()-1)
	require.NoError(t, updateUserCache(user))
	// A newer permanent ban must win over the expired snapshot in Redis.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates((UserDisablePeriod{}).Updates("permanent")).Error)
	cache, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusDisabled, cache.Status)
	require.Zero(t, cache.DisableUntil)
	require.Equal(t, "permanent", cache.DisableReason)
	// Wait for the existing asynchronous cache writer before changing the fixture.
	require.Eventually(t, func() bool { v, e := cacheGetUserBase(user.Id); return e == nil && v.DisableUntil == 0 }, time.Second, time.Millisecond)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Updates((UserDisablePeriod{Minutes: 1, Until: user.DisableUntil}).Updates("expired")).Error)
	require.NoError(t, updateUserCache(user))
	cache, err = GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, cache.Status)
	require.Eventually(t, func() bool {
		v, e := cacheGetUserBase(user.Id)
		return e == nil && v.Status == common.UserStatusEnabled
	}, time.Second, time.Millisecond)
	require.NoError(t, client.Del(context.Background(), getUserCacheKey(user.Id)).Err())
}

func TestUserDisableExpiryDatabaseFailureDoesNotEnableUser(t *testing.T) {
	setupUserDisableTestDB(t)
	user := seedTimedDisable(t, "db-failure", time.Now().Unix()-1)
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test:reject_expiry", func(tx *gorm.DB) { tx.AddError(errors.New("database update unavailable")) }))
	defer DB.Callback().Update().Remove("test:reject_expiry")
	require.Error(t, ResolveUserDisableExpiry(&user, time.Now().Unix()))
	require.Equal(t, common.UserStatusDisabled, user.Status)
	cached, err := GetUserCache(user.Id)
	require.Error(t, err)
	require.Nil(t, cached)
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, stored.Status)
}

func TestEditingDisableReasonPreservesDeadline(t *testing.T) {
	setupUserDisableTestDB(t)
	user := seedTimedDisable(t, "edit-reason", time.Now().Unix()+300)
	update := User{Id: user.Id, Username: user.Username, DisableReason: "edited reason"}
	require.NoError(t, update.Edit(false, true))
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	require.Equal(t, "edited reason", stored.DisableReason)
	require.Equal(t, user.DisableUntil, stored.DisableUntil)
	require.Equal(t, user.DisableDurationMinutes, stored.DisableDurationMinutes)
}
