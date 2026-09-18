package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManualUserDisableDurationAPI(t *testing.T) {
	for _, path := range []string{"/manage", "/batch-disable", "/batch-manage", "/enhancements/users/1/ban", "/enhancements/github-age-ban", "/enhancements/shared-ip/127.0.0.1/ban"} {
		for _, duration := range []string{
			"",
			`,"duration_minutes":0`,
			`,"duration_minutes":2`,
			`,"duration_minutes":-1`,
			`,"duration_minutes":0.5`,
			`,"duration_minutes":"2"`,
			`,"duration_minutes":""`,
			`,"duration_minutes":null`,
			`,"duration_minutes":false`,
			`,"duration_minutes":{}`,
			`,"duration_minutes":[]`,
			`,"duration_minutes":9223372036854775807`,
			`,"duration_minutes":9223372036854775808`,
		} {
			valid := duration == "" || duration == `,"duration_minutes":0` || duration == `,"duration_minutes":2`
			// Successful risk scans are covered by service tests using isolated data.
			if valid && (path == "/enhancements/github-age-ban" || path == "/enhancements/shared-ip/127.0.0.1/ban") {
				continue
			}
			t.Run(path+duration, func(t *testing.T) {
				db := setupEnhancementOptionControllerTestDB(t)
				require.NoError(t, i18n.Init())
				require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}))
				user := model.User{Id: 1, Username: "duration-api", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
				require.NoError(t, db.Create(&user).Error)
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set("id", 999); c.Set("role", common.RoleRootUser); c.Next() })
				router.POST("/manage", ManageUser)
				router.POST("/batch-disable", BatchDisableRelatedUsers)
				router.POST("/batch-manage", BatchManageUsers)
				router.POST("/enhancements/users/:user_id/ban", enhancementBanUser)
				router.POST("/enhancements/github-age-ban", enhancementGitHubAgeBanUsers)
				router.POST("/enhancements/shared-ip/:ip/ban", enhancementBanSharedTokenIPUsers)
				action := "disable"
				if path == "/batch-manage" {
					action = "disable_enabled"
				}
				body := fmt.Sprintf(`{"id":1,"action":%q,"reason":"review","related_user_ids":[]%s}`, action, duration)
				recorder := httptest.NewRecorder()
				before := time.Now().Unix()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body)))
				response := decodeEnhancementAPIResponse(t, recorder)
				require.Equal(t, valid, response.Success, response.Message)
				require.NoError(t, db.First(&user, user.Id).Error)
				if !valid {
					require.Equal(t, common.UserStatusEnabled, user.Status)
					return
				}
				require.Equal(t, common.UserStatusDisabled, user.Status)
				if duration == ",\"duration_minutes\":2" {
					require.Equal(t, int64(2), user.DisableDurationMinutes)
					require.GreaterOrEqual(t, user.DisableUntil, before+120)
					require.LessOrEqual(t, user.DisableUntil, time.Now().Unix()+120)
				} else {
					require.Zero(t, user.DisableUntil)
					require.Zero(t, user.DisableDurationMinutes)
				}
			})
		}
	}
}

func TestManageUserEnableAndPermanentRebanClearDeadline(t *testing.T) {
	db := setupEnhancementOptionControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Log{}))
	user := model.User{Username: "reban", Role: 1, Status: 2, DisableReason: "review", DisableDurationMinutes: 5, DisableUntil: time.Now().Unix() + 300}
	require.NoError(t, db.Create(&user).Error)
	for _, action := range []string{"enable", "disable"} {
		require.NoError(t, db.Model(&user).Updates(map[string]interface{}{"disable_until": time.Now().Unix() + 300, "disable_duration_minutes": 5}).Error)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 999)
		ctx.Set("role", common.RoleRootUser)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/manage", bytes.NewBufferString(fmt.Sprintf(`{"id":%d,"action":%q,"reason":"new"}`, user.Id, action)))
		ManageUser(ctx)
		require.True(t, decodeEnhancementAPIResponse(t, recorder).Success)
		require.NoError(t, db.First(&user, user.Id).Error)
		require.Zero(t, user.DisableUntil)
		require.Zero(t, user.DisableDurationMinutes)
		if action == "enable" {
			require.Empty(t, user.DisableReason)
		} else {
			require.Equal(t, "new", user.DisableReason)
		}
	}
}
