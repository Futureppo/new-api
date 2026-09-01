package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetStatusIncludesInviteCodeRequirement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := setting.GetEnhancementSetting()
	original := cfg.InviteCodeRequired
	cfg.InviteCodeRequired = true
	t.Cleanup(func() {
		cfg.InviteCodeRequired = original
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/status", nil)
	GetStatus(ctx)

	var response struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, true, response.Data["invite_code_required"])
}

func TestGetStatusIncludesDomainEmailRegistrationSetting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := common.DomainEmailRegistrationEnabled
	common.DomainEmailRegistrationEnabled = true
	t.Cleanup(func() {
		common.DomainEmailRegistrationEnabled = original
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/status", nil)
	GetStatus(ctx)

	var response struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Equal(t, true, response.Data["domain_email_registration_enabled"])
}

func TestSendEmailVerificationRejectsBlacklistedDomain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := append([]string(nil), common.EmailDomainBlacklist...)
	common.EmailDomainBlacklist = []string{"*.hdu.edu.cn"}
	t.Cleanup(func() {
		common.EmailDomainBlacklist = original
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/verification?email=user@mail.hdu.edu.cn", nil)
	SendEmailVerification(ctx)

	var response registrationAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
	require.Equal(t, i18n.MsgUserEmailDomainBlacklisted, response.Message)
}
