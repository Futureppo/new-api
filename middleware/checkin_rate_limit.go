package middleware

import (
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Separate authenticated-user budget. Rotation of client IP cannot bypass it.
func CheckinRateLimit() gin.HandlerFunc {
	// Initialize before any requests arrive; concurrent lazy Init is unsafe.
	memory := &common.InMemoryRateLimiter{}
	memory.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		if userID <= 0 {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		key := fmt.Sprintf("rateLimit:checkin:v2:user:%d", userID)
		allowed := false
		if common.RedisEnabled {
			count, err := common.RDB.Eval(c.Request.Context(), `local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; return n`, []string{key}).Int64()
			if err != nil {
				c.AbortWithStatus(http.StatusServiceUnavailable)
				return
			}
			allowed = count <= 5
		} else {
			allowed = memory.Request(key, 5, 60)
		}
		if !allowed {
			c.Header("Retry-After", "60")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"success": false, "message": "签到请求过于频繁，请稍后重试"})
			return
		}
		c.Next()
	}
}
