package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSessionRiskSettingsPersistenceAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEnhancementOptionControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.User{}))
	cfg := setting.GetEnhancementSetting()
	previous := cfg.SessionRiskRecordingEnabled
	t.Cleanup(func() { cfg.SessionRiskRecordingEnabled = previous })
	cfg.SessionRiskRecordingEnabled = false
	router := newEnhancementRouter()
	request := func(method string, body any) enhancementAPIResponse {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, enhancementJSONRequest(t, method, "/api/enhancements/risk/session-settings", body))
		return decodeEnhancementAPIResponse(t, recorder)
	}
	require.Equal(t, false, request(http.MethodGet, nil).Data["recording_enabled"])
	for _, enabled := range []bool{true, false, true} {
		response := request(http.MethodPut, gin.H{"recording_enabled": enabled})
		require.True(t, response.Success, response.Message)
		require.Equal(t, enabled, response.Data["recording_enabled"])
		var option model.Option
		require.NoError(t, db.Where(&model.Option{Key: "enhancement_setting.session_risk_recording_enabled"}).First(&option).Error)
		require.Equal(t, strconv.FormatBool(enabled), option.Value)
		// Reload through the startup path, with the in-memory value deliberately changed.
		cfg.SessionRiskRecordingEnabled = !enabled
		model.InitOptionMap()
		require.Equal(t, enabled, setting.IsSessionRiskRecordingEnabled())
	}
	for _, body := range []any{gin.H{}, gin.H{"recording_enabled": nil}, gin.H{"recording_enabled": "false"}, gin.H{"recording_enabled": 0}} {
		require.False(t, request(http.MethodPut, body).Success)
		require.True(t, setting.IsSessionRiskRecordingEnabled())
	}
	// A failed database save must not change the live switch or claim success.
	require.NoError(t, db.Migrator().DropTable(&model.Option{}))
	require.False(t, request(http.MethodPut, gin.H{"recording_enabled": false}).Success)
	require.True(t, setting.IsSessionRiskRecordingEnabled())
}

func TestSessionRiskAPIPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEnhancementOptionControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.User{}))
	cfg := setting.GetEnhancementSetting()
	previous := cfg.SessionRiskRecordingEnabled
	t.Cleanup(func() { cfg.SessionRiskRecordingEnabled = previous })
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			user := model.User{Username: "session-role-" + strconv.Itoa(role), AffCode: "sess" + strconv.Itoa(role), Password: "non-empty-password-hash", Role: role, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			router := gin.New()
			router.Use(sessions.Sessions("session", cookie.NewStore([]byte("session-risk-permission-test-secret"))))
			router.Use(func(c *gin.Context) {
				session := sessions.Default(c)
				session.Set("id", user.Id)
				session.Set("username", user.Username)
				session.Set("role", user.Role)
				c.Next()
			})
			RegisterEnhancementRoutes(router.Group("/api/enhancements", middleware.AdminAuth()))
			RegisterEnhancementRootRoutes(router.Group("/api/enhancements", middleware.RootAuth()))
			for _, path := range []string{"session-settings", "shared-sessions?kind=session", "shared-sessions?kind=cache"} {
				recorder := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/api/enhancements/risk/"+path, nil)
				req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
				router.ServeHTTP(recorder, req)
				response := decodeEnhancementAPIResponse(t, recorder)
				require.Equal(t, role >= common.RoleAdminUser, response.Success, response.Message)
			}
			recorder := httptest.NewRecorder()
			req := enhancementJSONRequest(t, http.MethodPut, "/api/enhancements/risk/session-settings", gin.H{"recording_enabled": true})
			req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
			router.ServeHTTP(recorder, req)
			response := decodeEnhancementAPIResponse(t, recorder)
			require.Equal(t, role == common.RoleRootUser, response.Success, response.Message)
			if role == common.RoleCommonUser {
				recorder = httptest.NewRecorder()
				req = enhancementJSONRequest(t, http.MethodPost, "/api/enhancements/risk/shared-sessions/ban-users", gin.H{})
				req.Header.Set("New-Api-User", strconv.Itoa(user.Id))
				router.ServeHTTP(recorder, req)
				response = decodeEnhancementAPIResponse(t, recorder)
				require.False(t, response.Success)
				require.NotContains(t, response.Message, "invalid session identifier")
			}
		})
	}
}
