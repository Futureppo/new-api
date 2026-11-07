package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCheckinRiskFlowKeepsEvidencePrivateAndPreventsDoubleAward(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	t.Cleanup(func() { *setting = old })
	setting.Enabled, setting.ClientCheckEnabled, setting.ExpireEnabled = true, true, true
	setting.MinQuota, setting.MaxQuota, setting.SpecialEnabled = 10, 100, false
	user := &model.User{Username: "checkin-flow", Quota: 100, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(user).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	router.GET("/api/user/checkin", GetCheckinStatus)
	router.POST("/api/user/checkin", DoCheckin)
	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/user/checkin", nil))
	require.NotEmpty(t, status.Result().Cookies())
	var award int
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/user/checkin", nil)
		req.Header.Set("User-Agent", "curl/8.0")
		req.AddCookie(status.Result().Cookies()[0])
		router.ServeHTTP(w, req)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				QuotaAwarded int `json:"quota_awarded"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, i == 0, result.Success)
		require.NotContains(t, w.Body.String(), "risk_")
		if i == 0 {
			award = result.Data.QuotaAwarded
		}
	}
	var stored model.Checkin
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&stored).Error)
	require.NotNil(t, stored.RiskScore)
	require.Less(t, *stored.RiskScore, 100)
	require.Contains(t, stored.RiskEvidence, "client_inconsistent")
	require.Equal(t, award, stored.RemainingQuota)
	var loaded model.User
	require.NoError(t, db.First(&loaded, user.Id).Error)
	require.Equal(t, 100+award, loaded.Quota)
}
