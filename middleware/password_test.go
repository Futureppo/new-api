package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPasswordSetupGate(t *testing.T) {
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			db := setupAuthMiddlewareTestDB(t)
			accessToken := "password-setup-management-token"
			user := model.User{Username: "no-password", Role: role, Status: common.UserStatusEnabled, AccessToken: &accessToken, Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			createAuthMiddlewareToken(t, user.Id, "passwordsetuptest")
			router := newAuthMiddlewareSessionRouter(user)
			ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
			router.GET("/api/user/self", UserAuth(), ok)
			router.PUT("/api/user/self/password", UserAuth(), ok)
			router.PUT("/api/user/self", UserAuth(), ok)
			router.GET("/admin", AdminAuth(), ok)
			router.GET("/root", RootAuth(), ok)
			router.GET("/oauth/test", PasswordSetupForSession(), ok)
			router.POST("/pg/chat/completions", UserAuth(), ok)
			router.GET("/mixed", TokenOrUserAuth(), ok)
			router.POST("/v1/chat/completions", TokenAuth(), ok)
			cookieHeader := createAuthMiddlewareSession(t, router)
			request := func(method, path, cookieValue, token string) int {
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("Cookie", cookieValue)
				req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				if w.Code == http.StatusForbidden {
					require.Contains(t, w.Body.String(), "PASSWORD_SETUP_REQUIRED")
				}
				return w.Code
			}
			for _, path := range []string{"/protected", "/mixed", "/oauth/test"} {
				require.Equal(t, http.StatusForbidden, request("GET", path, cookieHeader, ""))
			}
			if role >= common.RoleAdminUser {
				require.Equal(t, http.StatusForbidden, request("GET", "/admin", cookieHeader, ""))
			}
			if role >= common.RoleRootUser {
				require.Equal(t, http.StatusForbidden, request("GET", "/root", cookieHeader, ""))
			}
			require.Equal(t, http.StatusForbidden, request("PUT", "/api/user/self", cookieHeader, ""))
			require.Equal(t, http.StatusForbidden, request("POST", "/pg/chat/completions", cookieHeader, ""))
			require.Equal(t, http.StatusForbidden, request("GET", "/protected", "", accessToken))
			require.Equal(t, http.StatusNoContent, request("GET", "/api/user/self", cookieHeader, ""))
			require.Equal(t, http.StatusNoContent, request("PUT", "/api/user/self/password", cookieHeader, ""))
			require.Equal(t, http.StatusNoContent, request("POST", "/v1/chat/completions", "", "sk-passwordsetuptest"))
			require.Equal(t, http.StatusNoContent, request("GET", "/mixed", "", "sk-passwordsetuptest"))
			require.Equal(t, http.StatusNoContent, request("GET", "/oauth/test", "", ""))
			// The same already-issued session is unblocked immediately after setup.
			require.NoError(t, db.Model(&user).Update("password", "existing-hash").Error)
			require.Equal(t, http.StatusOK, request("GET", "/protected", cookieHeader, ""))
		})
	}
}

func TestPasswordStateReadFailureDoesNotAllowAccess(t *testing.T) {
	db := setupAuthMiddlewareTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&model.User{}))
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("password-test"))))
	router.GET("/test", func(c *gin.Context) {
		if requireUserPassword(c, 1) {
			t.Fatal("database failure allowed access")
		}
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
