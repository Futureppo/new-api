package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func IPBan() gin.HandlerFunc {
	return func(c *gin.Context) {
		ban, matched := model.MatchIPBan(c.ClientIP())
		if !matched {
			c.Next()
			return
		}
		c.Set(SkipAccessLogKey, true)
		c.String(http.StatusForbidden, "该ip已被封禁，原因："+ban.Reason)
		c.Abort()
	}
}
