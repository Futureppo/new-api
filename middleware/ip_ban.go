package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func IPBan() gin.HandlerFunc {
	return func(c *gin.Context) {
		ban, matched := model.MatchIPBan(c.ClientIP())
		if !matched {
			c.Next()
			return
		}
		if ban.AutoBanUser && ban.ExpiresAt == 0 {
			autoBanUserForIPBan(c, ban)
		}
		c.Set(SkipAccessLogKey, true)
		c.String(http.StatusForbidden, "该ip已被封禁，原因："+ban.Reason)
		c.Abort()
	}
}

func autoBanUserForIPBan(c *gin.Context, ban *model.IPBan) {
	userId := getIPBanRequestUserId(c)
	if userId == 0 {
		return
	}
	disabled, err := model.DisableUserByIPBan(userId, ban.Reason)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to auto ban user %d for ip ban #%d: %s", userId, ban.Id, err.Error()))
		return
	}
	if disabled {
		common.SysLog(fmt.Sprintf("auto banned user %d for ip ban #%d target=%s", userId, ban.Id, ban.Target))
	}
}

func getIPBanRequestUserId(c *gin.Context) int {
	if userId := getIPBanSessionUserId(c); userId > 0 {
		return userId
	}
	return getIPBanTokenUserId(c)
}

func getIPBanSessionUserId(c *gin.Context) int {
	if _, ok := c.Get(sessions.DefaultKey); !ok {
		return 0
	}
	return toPositiveInt(sessions.Default(c).Get("id"))
}

func getIPBanTokenUserId(c *gin.Context) int {
	key := extractIPBanTokenKey(c)
	if key == "" {
		return 0
	}
	token, err := model.GetTokenByKey(key, false)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysLog("failed to get token for ip ban auto user ban: " + err.Error())
		}
		return 0
	}
	if token.Status != common.TokenStatusEnabled {
		return 0
	}
	if token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp() {
		return 0
	}
	if !token.UnlimitedQuota && token.RemainQuota <= 0 {
		return 0
	}
	return token.UserId
}

func extractIPBanTokenKey(c *gin.Context) string {
	if key := extractIPBanWebSocketTokenKey(c); key != "" {
		return key
	}

	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/messages") || strings.Contains(path, "/v1/models") {
		if key := normalizeIPBanTokenKey(c.Request.Header.Get("x-api-key")); key != "" {
			return key
		}
	}
	if strings.HasPrefix(path, "/v1beta/models") ||
		strings.HasPrefix(path, "/v1beta/openai/models") ||
		strings.HasPrefix(path, "/v1/models/") {
		if key := normalizeIPBanTokenKey(c.Query("key")); key != "" {
			return key
		}
		if key := normalizeIPBanTokenKey(c.Request.Header.Get("x-goog-api-key")); key != "" {
			return key
		}
	}

	key := strings.TrimSpace(c.Request.Header.Get("Authorization"))
	if key == "" || key == "midjourney-proxy" {
		key = c.Request.Header.Get("mj-api-secret")
	}
	return normalizeIPBanTokenKey(key)
}

func extractIPBanWebSocketTokenKey(c *gin.Context) string {
	protocol := c.Request.Header.Get("Sec-WebSocket-Protocol")
	if protocol == "" {
		return ""
	}
	for _, part := range strings.Split(protocol, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "openai-insecure-api-key.") {
			return normalizeIPBanTokenKey(strings.TrimPrefix(part, "openai-insecure-api-key."))
		}
	}
	return ""
}

func normalizeIPBanTokenKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) > len("Bearer ") && strings.EqualFold(key[:len("Bearer ")], "Bearer ") {
		key = strings.TrimSpace(key[len("Bearer "):])
	}
	key = strings.TrimPrefix(key, "sk-")
	parts := strings.Split(key, "-")
	return strings.TrimSpace(parts[0])
}

func toPositiveInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}
