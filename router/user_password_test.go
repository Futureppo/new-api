package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSelfProfileAndPasswordShareSensitiveRateLimit(t *testing.T) {
	previousDB, previousRedis := model.DB, common.RedisEnabled
	previousGlobalLimit, previousCriticalLimit := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	previousLimitNum, previousLimitDuration := common.CriticalRateLimitNum, common.CriticalRateLimitDuration
	previousExpiration, previousMode := common.RateLimitKeyExpirationDuration, gin.Mode()
	t.Cleanup(func() {
		model.DB, common.RedisEnabled = previousDB, previousRedis
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousGlobalLimit, previousCriticalLimit
		common.CriticalRateLimitNum, common.CriticalRateLimitDuration = previousLimitNum, previousLimitDuration
		common.RateLimitKeyExpirationDuration = previousExpiration
		gin.SetMode(previousMode)
	})
	gin.SetMode(gin.TestMode)
	common.RedisEnabled, common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, false, true
	common.CriticalRateLimitNum, common.CriticalRateLimitDuration = 1, 600
	common.RateLimitKeyExpirationDuration = time.Hour
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}))
	hash, err := common.Password2Hash("Password123!")
	require.NoError(t, err)
	user := model.User{Username: "rate-limit-user", Password: hash, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("password-rate-limit-test-secret"))))
	engine.Use(func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		c.Next()
	})
	SetApiRouter(engine)
	request := func(path, ip string, payload gin.H) *httptest.ResponseRecorder {
		body, err := common.Marshal(payload)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
		req.RemoteAddr = ip + ":4321"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	passwordAttempt := gin.H{"password": "Another123!", "original_password": "wrong"}
	profileAttempt := gin.H{"display_name": "changed", "original_password": "wrong"}
	for i, firstPath := range []string{"/api/user/self/password", "/api/user/self"} {
		ip := "198.18.1." + strconv.Itoa(i+1)
		firstPayload := passwordAttempt
		if firstPath == "/api/user/self" {
			firstPayload = profileAttempt
		}
		first := request(firstPath, ip, firstPayload)
		require.Equal(t, http.StatusOK, first.Code)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(first.Body.Bytes(), &result))
		require.False(t, result.Success)
		require.Equal(t, http.StatusTooManyRequests, request("/api/user/self/password", ip, passwordAttempt).Code)
		require.Equal(t, http.StatusTooManyRequests, request("/api/user/self", ip, profileAttempt).Code)
	}
	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.Equal(t, hash, stored.Password)
	require.Empty(t, stored.DisplayName)
}
