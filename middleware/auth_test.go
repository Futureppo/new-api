package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUserAuthDisabledUserMessageIncludesReason(t *testing.T) {
	setupAuthMiddlewareTestDB(t)
	require.NoError(t, i18n.Init())
	user := createAuthMiddlewareUser(t, common.UserStatusDisabled, "违反使用规则")

	router := newAuthMiddlewareSessionRouter(user)
	cookieHeader := createAuthMiddlewareSession(t, router)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
	req.Header.Set("Accept-Language", "zh-CN")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decodeAuthMiddlewareResponse(t, recorder.Body.Bytes())
	require.False(t, body.Success)
	require.Contains(t, body.Message, "该用户已被禁用，原因：违反使用规则")
}

func TestUserAuthDisabledUserMessageWithoutReasonUsesDefault(t *testing.T) {
	setupAuthMiddlewareTestDB(t)
	require.NoError(t, i18n.Init())
	user := createAuthMiddlewareUser(t, common.UserStatusDisabled, "")

	router := newAuthMiddlewareSessionRouter(user)
	cookieHeader := createAuthMiddlewareSession(t, router)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
	req.Header.Set("Accept-Language", "zh-CN")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := decodeAuthMiddlewareResponse(t, recorder.Body.Bytes())
	require.False(t, body.Success)
	require.Equal(t, "用户已被封禁", body.Message)
	require.NotContains(t, body.Message, "原因：")
}

func TestTokenAuthDisabledUserMessageIncludesReason(t *testing.T) {
	setupAuthMiddlewareTestDB(t)
	require.NoError(t, i18n.Init())
	user := createAuthMiddlewareUser(t, common.UserStatusDisabled, "恶意请求")
	createAuthMiddlewareToken(t, user.Id, "disabledreason")

	router := gin.New()
	router.GET("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-disabledreason")
	req.Header.Set("Accept-Language", "zh-CN")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	require.Contains(t, body.Error.Message, "该用户已被禁用，原因：恶意请求")
}

func setupAuthMiddlewareTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRedisEnabled := common.RedisEnabled
	originalUsingSQLite := common.UsingSQLite
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	common.UsingSQLite = true
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.RedisEnabled = originalRedisEnabled
		common.UsingSQLite = originalUsingSQLite
	})

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))
	return db
}

func createAuthMiddlewareUser(t *testing.T, status int, reason string) model.User {
	t.Helper()
	user := model.User{
		Username:      "auth-user-" + strconv.Itoa(status) + "-" + strconv.Itoa(len(reason)),
		Password:      "password",
		Role:          common.RoleCommonUser,
		Status:        status,
		DisableReason: reason,
		Group:         "default",
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func createAuthMiddlewareToken(t *testing.T, userId int, key string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         userId,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)
}

func newAuthMiddlewareSessionRouter(user model.User) *gin.Engine {
	router := gin.New()
	store := cookie.NewStore([]byte("auth-disabled-reason-test-secret"))
	router.Use(sessions.Sessions("session", store))
	router.GET("/session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("id", user.Id)
		session.Set("username", user.Username)
		session.Set("role", user.Role)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", user.Group)
		_ = session.Save()
		c.Status(http.StatusNoContent)
	})
	router.GET("/protected", UserAuth(), func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	return router
}

func createAuthMiddlewareSession(t *testing.T, router *gin.Engine) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/session", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	cookies := recorder.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].String()
}

func decodeAuthMiddlewareResponse(t *testing.T, data []byte) struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
} {
	t.Helper()
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(data, &body))
	return body
}
