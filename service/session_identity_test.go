package service

import (
	"io"
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

func TestRequestSessionCaptureClientAdaptersPreservesOriginal(t *testing.T) {
	original := setting.GetEnhancementSetting().SessionRiskRecordingEnabled
	t.Cleanup(func() { setting.GetEnhancementSetting().SessionRiskRecordingEnabled = original })
	setting.GetEnhancementSetting().SessionRiskRecordingEnabled = true
	cases := []struct {
		name, path, body string
		headers          map[string]string
		want             common.RequestSessionIdentity
	}{
		{
			name: "AstrBot Gemini", path: "/v1beta/models/gemini:generateContent",
			body:    `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`,
			headers: map[string]string{"x-astrbot-conversation-id": "astrbot-original"},
			want:    common.RequestSessionIdentity{SessionID: "astrbot-original", SessionSource: "header:x-astrbot-conversation-id"},
		},
		{
			name: "Kimi Anthropic", path: "/v1/messages",
			body:    `{"model":"claude","metadata":{"user_id":"kimi-original"}}`,
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			want:    common.RequestSessionIdentity{SessionID: "kimi-original", SessionSource: "body:metadata.user_id"},
		},
		{
			name: "Harness Chat Completions cache only", path: "/v1/chat/completions",
			body:    `{"model":"deepseek","prompt_cache_key":"original-cache","messages":[]}`,
			headers: map[string]string{"User-Agent": "deepseek-harness/0.2.0-rc.2"},
			want:    common.RequestSessionIdentity{CacheKey: "original-cache", CacheSource: "body:prompt_cache_key"},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tt.path, strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			for key, value := range tt.headers {
				c.Request.Header.Set(key, value)
			}
			var parsed map[string]interface{}
			require.NoError(t, common.UnmarshalBodyReusable(c, &parsed))
			defer common.CleanupBodyStorage(c)
			CaptureRequestSessionIdentity(c)
			require.Equal(t, tt.want, c.MustGet(common.RequestSessionIdentityKey))
			forwarded, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.JSONEq(t, tt.body, string(forwarded))
			// Channel/header rewrites must not replace the original client identity.
			c.Request.Header.Set("x-astrbot-conversation-id", "upstream-conversation")
			c.Request.Header.Set("x-deepseek-harness-session-id", "upstream-session")
			CaptureRequestSessionIdentity(c)
			require.Equal(t, tt.want, c.MustGet(common.RequestSessionIdentityKey))
		})
	}
}
