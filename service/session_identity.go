package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// CaptureRequestSessionIdentity runs after the distributor has cached the original
// request body and before any channel configuration can rewrite it.
func CaptureRequestSessionIdentity(c *gin.Context) {
	if !setting.IsSessionRiskRecordingEnabled() || c == nil || c.Request == nil {
		return
	}
	if _, exists := c.Get(common.RequestSessionIdentityKey); exists {
		return
	}
	var body []byte
	// Never consume an uncached body here (including uploads and WebSocket handshakes).
	if !strings.Contains(c.GetHeader("Content-Type"), "multipart/") {
		if cached, ok := c.Get(common.KeyBodyStorage); ok {
			if storage, ok := cached.(common.BodyStorage); ok {
				body, _ = storage.Bytes()
			}
		} else if cached, ok := c.Get(common.KeyRequestBody); ok {
			body, _ = cached.([]byte)
		}
	}
	c.Set(common.RequestSessionIdentityKey, common.ExtractRequestSessionIdentity(c.Request.Header, body))
}
