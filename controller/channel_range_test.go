package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelRangeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.User{}))
	model.DB = db
	common.MemoryCacheEnabled, common.RedisEnabled = false, false
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		model.DB = previousDB
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
		require.NoError(t, sqlDB.Close())
	})
	return db
}

type channelRangeResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    int64  `json:"data"`
}

func requestChannelRange(t *testing.T, handler http.Handler, body, token string) channelRangeResponse {
	t.Helper()
	return requestChannelBatchDeletion(t, handler, "/api/channel/batch/range", body, token)
}

func requestChannelBatchDeletion(t *testing.T, handler http.Handler, path, body, token string) channelRangeResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("New-Api-User", "1")
	}
	handler.ServeHTTP(recorder, request)
	var response channelRangeResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestDeleteChannelRangeValidation(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Key: "test"}).Error)
	router := gin.New()
	router.POST("/api/channel/batch/range", DeleteChannelRange)
	for _, body := range []string{
		`{}`, `{"start_id":1}`, `{"end_id":2}`, `{"start_id":null,"end_id":2}`,
		`{"start_id":0,"end_id":2}`, `{"start_id":-1,"end_id":2}`,
		`{"start_id":2,"end_id":1}`, `{"start_id":1.5,"end_id":2}`,
		`{"start_id":"1","end_id":2}`, `{"start_id":1,"end_id":9007199254740992}`,
		`{"start_id":1,"end_id":999999999999999999999}`, `invalid JSON`,
	} {
		t.Run(body, func(t *testing.T) {
			response := requestChannelRange(t, router, body, "")
			require.False(t, response.Success)
			require.NotEmpty(t, response.Message)
		})
	}
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestDeleteChannelRangeRefreshesCacheAndRPM(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	ids := []int{970001, 970003, 970004}
	settings := &dto.ChannelRPMProtectionSettings{Enabled: true, RPMLimit: 1}
	for _, id := range ids {
		require.NoError(t, db.Create(&model.Channel{Id: id, Key: "test", Models: "model", Group: "default", Status: 1}).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: id, Model: "model", Group: "default", Enabled: true}).Error)
		service.ResetChannelRPMState(id)
		require.True(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
		require.False(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
	}
	t.Cleanup(func() { service.ResetChannelRPMStates(ids) })
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	_, err := model.CacheGetChannel(ids[0])
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/channel/batch/range", DeleteChannelRange)
	response := requestChannelRange(t, router, `{"start_id":970001,"end_id":970003}`, "")
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data)
	for _, id := range ids[:2] {
		_, err := model.CacheGetChannel(id)
		require.Error(t, err)
		require.True(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
	}
	_, err = model.CacheGetChannel(ids[2])
	require.NoError(t, err)
	require.False(t, service.TryAcquireChannelRPM(context.Background(), ids[2], settings).Allowed)
	response = requestChannelRange(t, router, `{"start_id":970001,"end_id":970003}`, "")
	require.True(t, response.Success)
	require.Zero(t, response.Data)
}

func TestDeleteChannelRangeFailurePreservesCacheAndRPM(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	const id = 970005
	require.NoError(t, db.Create(&model.Channel{Id: id, Key: "test", Models: "model", Group: "default"}).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: id, Model: "model", Group: "default", Enabled: true}).Error)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	settings := &dto.ChannelRPMProtectionSettings{Enabled: true, RPMLimit: 1}
	service.ResetChannelRPMState(id)
	t.Cleanup(func() { service.ResetChannelRPMState(id) })
	require.True(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
	require.NoError(t, db.Callback().Delete().Before("gorm:delete").Register("test:range_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "abilities" {
			tx.AddError(errors.New("injected deletion failure"))
		}
	}))
	router := gin.New()
	router.POST("/api/channel/batch/range", DeleteChannelRange)
	response := requestChannelRange(t, router, `{"start_id":970005,"end_id":970005}`, "")
	require.False(t, response.Success)
	_, err := model.CacheGetChannel(id)
	require.NoError(t, err)
	require.False(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestDeleteChannelRangeAdminAuth(t *testing.T) {
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			db := setupChannelRangeTestDB(t)
			token := ""
			if role != 0 {
				token = "channel-range-test-access-token"
				require.NoError(t, db.Create(&model.User{Id: 1, Username: "range-admin", Password: "existing-password-hash", Role: role, Status: common.UserStatusEnabled, AccessToken: &token}).Error)
			}
			require.NoError(t, db.Create(&model.Channel{Id: 1, Key: "test"}).Error)
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("channel-range-test-secret"))))
			channels := router.Group("/api/channel", middleware.AdminAuth())
			channels.POST("/batch/range", DeleteChannelRange)
			response := requestChannelRange(t, router, `{"start_id":1,"end_id":1}`, token)
			var remaining int64
			require.NoError(t, db.Model(&model.Channel{}).Count(&remaining).Error)
			if role >= common.RoleAdminUser {
				require.True(t, response.Success)
				require.EqualValues(t, 1, response.Data)
				require.Zero(t, remaining)
			} else {
				require.False(t, response.Success)
				require.EqualValues(t, 1, remaining)
			}
		})
	}
}
