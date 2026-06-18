package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIPBanMiddlewareBlocksWithoutAccessLog(t *testing.T) {
	setupIPBanMiddlewareTestDB(t)
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target: "203.0.113.10",
		Reason: "abuse",
	}))
	model.InitIPBanCache()

	var logBuffer bytes.Buffer
	oldWriter := gin.DefaultWriter
	gin.DefaultWriter = &logBuffer
	t.Cleanup(func() {
		gin.DefaultWriter = oldWriter
	})

	router := gin.New()
	router.Use(IPBan())
	SetUpLogger(router)
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "该ip已被封禁，原因：abuse", recorder.Body.String())
	require.Empty(t, logBuffer.String())

	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
	require.EqualValues(t, 0, count)
}

func TestIPBanMiddlewareAutoBansCommonUserFromToken(t *testing.T) {
	db := setupIPBanMiddlewareTestDB(t)
	user := createIPBanMiddlewareUser(t, common.RoleCommonUser)
	createIPBanMiddlewareToken(t, user.Id, "autobantoken")
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target:      "203.0.113.10",
		Reason:      "abuse from banned ip",
		AutoBanUser: true,
	}))
	model.InitIPBanCache()

	router := gin.New()
	router.Use(IPBan())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Authorization", "Bearer sk-autobantoken")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	var updated model.User
	require.NoError(t, db.First(&updated, "id = ?", user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, updated.Status)
	require.Equal(t, "abuse from banned ip", updated.DisableReason)
}

func TestIPBanMiddlewareDoesNotAutoBanPrivilegedUsers(t *testing.T) {
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			db := setupIPBanMiddlewareTestDB(t)
			user := createIPBanMiddlewareUser(t, role)
			createIPBanMiddlewareToken(t, user.Id, "privilegedtoken")
			require.NoError(t, model.CreateIPBan(&model.IPBan{
				Target:      "203.0.113.10",
				Reason:      "privileged request",
				AutoBanUser: true,
			}))
			model.InitIPBanCache()

			router := gin.New()
			router.Use(IPBan())
			router.GET("/", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "203.0.113.10:1234"
			req.Header.Set("Authorization", "Bearer sk-privilegedtoken")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusForbidden, recorder.Code)
			var updated model.User
			require.NoError(t, db.First(&updated, "id = ?", user.Id).Error)
			require.Equal(t, common.UserStatusEnabled, updated.Status)
			require.Empty(t, updated.DisableReason)
		})
	}
}

func setupIPBanMiddlewareTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})
	require.NoError(t, db.AutoMigrate(&model.IPBan{}, &model.Log{}, &model.User{}, &model.Token{}))
	return db
}

func createIPBanMiddlewareUser(t *testing.T, role int) model.User {
	t.Helper()
	user := model.User{
		Username: "user-" + strconv.Itoa(role),
		Password: "password",
		Role:     role,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func createIPBanMiddlewareToken(t *testing.T, userId int, key string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         userId,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)
}
