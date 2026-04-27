package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openChannelRetryControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestUpdateChannelClearsRetryTimes(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	retryTimes := 2
	autoBan := 1
	channel := model.Channel{
		Type:       1,
		Key:        "test-key",
		Status:     common.ChannelStatusEnabled,
		Name:       "retry-test",
		Models:     "gpt-4o",
		Group:      "default",
		AutoBan:    &autoBan,
		RetryTimes: &retryTimes,
	}
	require.NoError(t, db.Create(&channel).Error)

	body := fmt.Sprintf(`{
		"id": %d,
		"type": 1,
		"key": "test-key",
		"status": %d,
		"name": "retry-test",
		"models": "gpt-4o",
		"group": "default",
		"auto_ban": 1,
		"retry_times": null
	}`, channel.Id, common.ChannelStatusEnabled)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Nil(t, reloaded.RetryTimes)
}
