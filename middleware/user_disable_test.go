package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTimedDisableAuthAndStaleSessions(t *testing.T) {
	for _, mode := range []string{"session", "access_token", "api_token", "readonly_token", "token_or_session"} {
		t.Run(mode, func(t *testing.T) {
			db := setupAuthMiddlewareTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Log{}))
			require.NoError(t, i18n.Init())
			user := createAuthMiddlewareUser(t, common.UserStatusDisabled, "time review")
			require.NoError(t, db.Model(&user).Updates(map[string]interface{}{"disable_duration_minutes": 2, "disable_until": time.Now().Unix() + 120, "access_token": "management-token"}).Error)
			createAuthMiddlewareToken(t, user.Id, "timedkey")
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("timed-ban-test-secret"))))
			router.GET("/session", func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("id", user.Id)
				session.Set("username", user.Username)
				session.Set("role", user.Role)
				session.Set("status", common.UserStatusDisabled)
				session.Set("group", "default")
				require.NoError(t, session.Save())
				c.Status(http.StatusNoContent)
			})
			auth := UserAuth()
			switch mode {
			case "api_token":
				auth = TokenAuth()
			case "readonly_token":
				auth = TokenAuthReadOnly()
			case "token_or_session":
				auth = TokenOrUserAuth()
			}
			router.GET("/protected", auth, func(c *gin.Context) { c.String(http.StatusOK, "allowed") })
			cookieHeader := createAuthMiddlewareSession(t, router)
			request := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodGet, "/protected", nil)
				req.Header.Set("Accept-Language", "zh-CN")
				req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
				switch mode {
				case "session", "token_or_session":
					req.Header.Set("Cookie", cookieHeader)
				case "access_token":
					req.Header.Set("Authorization", "Bearer management-token")
				default:
					req.Header.Set("Authorization", "Bearer sk-timedkey")
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)
				return recorder
			}
			denied := request()
			require.Contains(t, denied.Body.String(), "time review")
			require.Contains(t, denied.Body.String(), "封禁 2 分钟")
			require.Contains(t, denied.Body.String(), "解禁时间")
			require.NoError(t, db.Model(&user).Update("disable_until", time.Now().Unix()-1).Error)
			allowed := request()
			require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
			require.Equal(t, "allowed", allowed.Body.String())
		})
	}
}
