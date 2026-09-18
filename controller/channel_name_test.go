package controller

import (
	"context"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDeleteChannelByNameValidation(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "keep", Key: "test"}).Error)
	router := gin.New()
	router.POST("/api/channel/batch/name", DeleteChannelByName)
	for _, body := range []string{`{}`, `{"name":null}`, `{"name":""}`, `{"name":" \t\n"}`, `{"name":1}`, `{"name":[]}`, `invalid`} {
		response := requestChannelBatchDeletion(t, router, "/api/channel/batch/name", body, "")
		require.False(t, response.Success)
		require.NotEmpty(t, response.Message)
	}
	var count int64
	require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestDeleteChannelByNameExactMatchAndCache(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	ids := []int{980001, 980002, 980003}
	settings := &dto.ChannelRPMProtectionSettings{Enabled: true, RPMLimit: 1}
	for i, name := range []string{" Exact ", " Exact ", "Exact"} {
		id := ids[i]
		require.NoError(t, db.Create(&model.Channel{Id: id, Name: name, Key: "test", Models: "model", Group: "default", Status: i%3 + 1}).Error)
		require.NoError(t, db.Create(&model.Ability{ChannelId: id, Model: "model", Group: "default", Enabled: true}).Error)
		service.ResetChannelRPMState(id)
		require.True(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
	}
	t.Cleanup(func() { service.ResetChannelRPMStates(ids) })
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	router := gin.New()
	router.POST("/api/channel/batch/name", DeleteChannelByName)
	response := requestChannelBatchDeletion(t, router, "/api/channel/batch/name", `{"name":" Exact "}`, "")
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data)
	for _, id := range ids[:2] {
		_, err := model.CacheGetChannel(id)
		require.Error(t, err)
		require.True(t, service.TryAcquireChannelRPM(context.Background(), id, settings).Allowed)
	}
	_, err := model.CacheGetChannel(ids[2])
	require.NoError(t, err)
	require.False(t, service.TryAcquireChannelRPM(context.Background(), ids[2], settings).Allowed)
	response = requestChannelBatchDeletion(t, router, "/api/channel/batch/name", `{"name":" Exact "}`, "")
	require.True(t, response.Success)
	require.Zero(t, response.Data)
}

func TestDeleteChannelByNameAdminAuth(t *testing.T) {
	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			db := setupChannelRangeTestDB(t)
			token := ""
			if role != 0 {
				token = "channel-name-test-access-token"
				require.NoError(t, db.Create(&model.User{Id: 1, Username: "name-admin", Password: "existing-password-hash", Role: role, Status: common.UserStatusEnabled, AccessToken: &token}).Error)
			}
			require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "delete", Key: "test"}).Error)
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("channel-name-test-secret"))))
			channels := router.Group("/api/channel", middleware.AdminAuth())
			channels.POST("/batch/name", DeleteChannelByName)
			response := requestChannelBatchDeletion(t, router, "/api/channel/batch/name", `{"name":"delete"}`, token)
			var count int64
			require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
			if role >= common.RoleAdminUser {
				require.True(t, response.Success)
				require.Zero(t, count)
			} else {
				require.False(t, response.Success)
				require.EqualValues(t, 1, count)
			}
		})
	}
}
