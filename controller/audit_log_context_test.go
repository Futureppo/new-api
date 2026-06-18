package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAuditLogControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.Redemption{}, &model.Checkin{}))

	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		_ = sqlDB.Close()
	})

	return db
}

func createAuditLogTestUser(t *testing.T, db *gorm.DB, userID int, username string) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{
		Id:       userID,
		Username: username,
		Password: "password123",
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}).Error)
}

func newAuditLogTestContext(method string, target string, body string, userID int, username string, userAgent string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
	c.Request.RemoteAddr = "203.0.113.10:1234"
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", userAgent)
	c.Set("id", userID)
	c.Set("username", username)
	return c, recorder
}

func TestTopUpWithRedemptionRecordsIPAndUserAgent(t *testing.T) {
	db := setupAuditLogControllerTestDB(t)
	const userID = 1
	createAuditLogTestUser(t, db, userID, "alice")

	key := "0123456789abcdef0123456789abcdef"
	redemption := model.Redemption{
		UserId:      userID,
		Name:        "test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       100,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, db.Create(&redemption).Error)

	c, recorder := newAuditLogTestContext(http.MethodPost, "/api/user/topup", fmt.Sprintf(`{"key":"%s"}`, key), userID, "alice", "Codex Test/1.0")
	TopUp(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", userID, model.LogTypeTopup).First(&log).Error)
	require.Equal(t, "203.0.113.10", log.Ip)
	require.Equal(t, "Codex Test/1.0", log.UserAgent)
	require.Contains(t, log.Content, fmt.Sprintf("兑换码ID %d", redemption.Id))
}

func TestDoCheckinRecordsIPAndUserAgent(t *testing.T) {
	db := setupAuditLogControllerTestDB(t)
	const userID = 1
	createAuditLogTestUser(t, db, userID, "alice")

	checkinSetting := operation_setting.GetCheckinSetting()
	oldCheckinSetting := *checkinSetting
	checkinSetting.Enabled = true
	checkinSetting.MinQuota = 100
	checkinSetting.MaxQuota = 100
	t.Cleanup(func() {
		*checkinSetting = oldCheckinSetting
	})

	c, recorder := newAuditLogTestContext(http.MethodPost, "/api/user/checkin", "", userID, "alice", "Codex Test/2.0")
	DoCheckin(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)

	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", userID, model.LogTypeSystem).First(&log).Error)
	require.Equal(t, "203.0.113.10", log.Ip)
	require.Equal(t, "Codex Test/2.0", log.UserAgent)
	require.Contains(t, log.Content, "用户签到，获得额度")
}
