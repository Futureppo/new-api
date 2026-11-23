package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAzureDiscoveryRoutesRequireChannelPrivileges(t *testing.T) {
	previousDB, previousRedis, previousMemory := model.DB, common.RedisEnabled, common.MemoryCacheEnabled
	previousLimit, previousMode := common.GlobalApiRateLimitEnable, gin.Mode()
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.MemoryCacheEnabled = previousDB, previousRedis, previousMemory
		common.GlobalApiRateLimitEnable = previousLimit
		gin.SetMode(previousMode)
	})
	gin.SetMode(gin.TestMode)
	common.RedisEnabled, common.MemoryCacheEnabled, common.GlobalApiRateLimitEnable = false, false, false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}))

	for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		user := model.User{Username: "azure-role-" + strconv.Itoa(role), Password: "test-password", Role: role, AffCode: "azure-" + strconv.Itoa(role), Status: common.UserStatusEnabled}
		if role > 0 {
			require.NoError(t, db.Create(&user).Error)
		}
		engine := gin.New()
		engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("azure-route-test-secret"))))
		engine.Use(func(c *gin.Context) {
			if role > 0 {
				session := sessions.Default(c)
				session.Set("id", user.Id)
				session.Set("username", user.Username)
				session.Set("role", role)
			}
			c.Next()
		})
		SetApiRouter(engine)
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			path := "/api/channel/azure/discover"
			if method == http.MethodGet {
				path += "/99999"
			}
			request := httptest.NewRequest(method, path, strings.NewReader(`{"channel_id":99999}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("New-Api-User", strconv.Itoa(user.Id))
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			if role == 0 {
				require.Equal(t, http.StatusUnauthorized, recorder.Code)
			} else if role == common.RoleRootUser || (role == common.RoleAdminUser && method == http.MethodGet) {
				// Authorization passed and reached the read-only missing-channel check.
				require.Equal(t, http.StatusNotFound, recorder.Code, recorder.Body.String())
			} else {
				require.Equal(t, http.StatusOK, recorder.Code)
				require.Contains(t, recorder.Body.String(), `"success":false`)
				require.NotContains(t, recorder.Body.String(), "渠道不存在")
			}
		}
	}
}
