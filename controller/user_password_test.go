package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLoginPasswordSetupFlow(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	accessToken := "setup-flow-management-token"
	user := model.User{Username: "third-party", GitHubId: "123", DisplayName: "original", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AccessToken: &accessToken}
	require.NoError(t, db.Create(&user).Error)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("password-setup-flow-secret"))))
	router.GET("/login", func(c *gin.Context) {
		// Password was deliberately omitted: login status must still be correct.
		loaded, err := model.GetUserById(user.Id, false)
		require.NoError(t, err)
		setupLogin(loaded, c)
	})
	router.GET("/api/user/self", middleware.UserAuth(), GetSelf)
	router.PUT("/api/user/self/password", middleware.UserAuth(), UpdateSelfPassword)
	router.PUT("/api/user/self", middleware.UserAuth(), UpdateSelf)
	var cookieHeader string
	request := func(method, path string, payload gin.H, session bool) (*httptest.ResponseRecorder, map[string]any) {
		body, err := common.Marshal(payload)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
		if session {
			req.Header.Set("Cookie", cookieHeader)
		} else {
			req.Header.Set("Authorization", "Bearer "+accessToken)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var response map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		return w, response
	}
	w, result := request("GET", "/login", nil, true)
	cookieHeader = w.Result().Cookies()[0].String()
	require.Equal(t, false, result["data"].(map[string]any)["has_password"])
	_, result = request("GET", "/api/user/self", nil, true)
	data := result["data"].(map[string]any)
	require.Equal(t, false, data["has_password"])
	require.NotContains(t, data, "password")
	w, _ = request("PUT", "/api/user/self/password", gin.H{"password": "Password123!"}, false)
	require.Equal(t, http.StatusForbidden, w.Code)
	_, result = request("PUT", "/api/user/self/password", gin.H{"password": "password123"}, true)
	require.Equal(t, false, result["success"])
	w, _ = request("PUT", "/api/user/self", gin.H{"password": "Password123!"}, true)
	require.Equal(t, http.StatusForbidden, w.Code)
	_, result = request("PUT", "/api/user/self/password", gin.H{"password": "Password123!", "role": common.RoleRootUser, "display_name": "injected"}, true)
	require.Equal(t, true, result["success"])
	_, result = request("GET", "/api/user/self", nil, true)
	data = result["data"].(map[string]any)
	require.Equal(t, true, data["has_password"])
	require.Equal(t, "original", data["display_name"])
	require.EqualValues(t, common.RoleCommonUser, data["role"])
	_, result = request("GET", "/login", nil, true)
	require.Equal(t, true, result["data"].(map[string]any)["has_password"])
	_, result = request("PUT", "/api/user/self/password", gin.H{"password": "Another123!"}, true)
	require.Equal(t, false, result["success"])
	_, result = request("PUT", "/api/user/self", gin.H{"password": "weak", "original_password": "Password123!"}, true)
	require.Equal(t, false, result["success"])
	_, result = request("PUT", "/api/user/self", gin.H{"password": "Another123!", "original_password": "Password123!"}, true)
	require.Equal(t, true, result["success"])
	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.True(t, common.ValidatePasswordAndHash("Another123!", stored.Password))
	_, result = request("PUT", "/api/user/self", gin.H{"display_name": "changed"}, true)
	require.Equal(t, false, result["success"], "profile changes retain their existing password verification")
}
