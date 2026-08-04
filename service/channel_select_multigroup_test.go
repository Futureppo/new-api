package service

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMultiGroupChannelSelectionTest(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB := model.DB
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	oldUsingSQLite := common.UsingSQLite
	common.MemoryCacheEnabled = true
	common.UsingSQLite = true

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))
	model.DB = db

	t.Cleanup(func() {
		model.DB = oldDB
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
		common.UsingSQLite = oldUsingSQLite
		if oldDB != nil && oldMemoryCacheEnabled {
			model.InitChannelCache()
		}
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func TestMultiGroupChannelSelectionOrderAndRetryLock(t *testing.T) {
	db := setupMultiGroupChannelSelectionTest(t)
	priority := int64(0)
	channels := []model.Channel{
		{Id: 1, Type: 1, Key: "default-key", Status: common.ChannelStatusEnabled, Name: "default", Group: "default", Models: "shared"},
		{Id: 2, Type: 1, Key: "vip-key", Status: common.ChannelStatusEnabled, Name: "vip", Group: "vip", Models: "shared,vip-only"},
	}
	require.NoError(t, db.Create(&channels).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "shared", ChannelId: 1, Enabled: true, Priority: &priority},
		{Group: "vip", Model: "shared", ChannelId: 2, Enabled: true, Priority: &priority},
		{Group: "vip", Model: "vip-only", ChannelId: 2, Enabled: true, Priority: &priority},
	}).Error)
	model.InitChannelCache()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroups, []string{"default", "vip"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)

	channel, selectedGroup, err := CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "auto", ModelName: "vip-only", Retry: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 2, channel.Id)
	require.Equal(t, "vip", selectedGroup)

	// Start a fresh request for a shared model. Selection order chooses default.
	ctx, _ = gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenGroups, []string{"default", "vip"})
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, false)
	channel, selectedGroup, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "auto", ModelName: "shared", Retry: common.GetPointer(0),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 1, channel.Id)
	require.Equal(t, "default", selectedGroup)

	// Once default handled the request, disabling cross-group retry pins it.
	MarkChannelDailySuccessLimitSkipped(ctx, 1)
	channel, _, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "auto", ModelName: "shared", Retry: common.GetPointer(1),
	})
	require.NoError(t, err)
	require.Nil(t, channel)

	// Enabling cross-group retry allows the same request to continue to vip.
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	channel, selectedGroup, err = CacheGetRandomSatisfiedChannel(&RetryParam{
		Ctx: ctx, TokenGroup: "auto", ModelName: "shared", Retry: common.GetPointer(1),
	})
	require.NoError(t, err)
	require.NotNil(t, channel)
	require.Equal(t, 2, channel.Id)
	require.Equal(t, "vip", selectedGroup)
}
