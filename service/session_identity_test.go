package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestSessionCaptureOriginalAndSwitch(t *testing.T) {
	original := setting.GetEnhancementSetting().SessionRiskRecordingEnabled
	t.Cleanup(func() { setting.GetEnhancementSetting().SessionRiskRecordingEnabled = original })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude","metadata":{"user_id":"user_device_account__session_original"}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	setting.GetEnhancementSetting().SessionRiskRecordingEnabled = false
	CaptureRequestSessionIdentity(c)
	_, captured := c.Get(common.RequestSessionIdentityKey)
	require.False(t, captured)
	var request map[string]interface{}
	require.NoError(t, common.UnmarshalBodyReusable(c, &request))
	defer common.CleanupBodyStorage(c)
	setting.GetEnhancementSetting().SessionRiskRecordingEnabled = true
	CaptureRequestSessionIdentity(c)
	c.Request.Header.Set("x-claude-code-session-id", "rewritten-upstream")
	CaptureRequestSessionIdentity(c)
	identity := c.MustGet(common.RequestSessionIdentityKey).(common.RequestSessionIdentity)
	require.Equal(t, "original", identity.SessionID)
	var reread map[string]interface{}
	require.NoError(t, common.UnmarshalBodyReusable(c, &reread))
	require.Equal(t, request, reread)
}
