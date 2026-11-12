package middleware

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type turnstileCheckResponse struct {
	Success bool `json:"success"`
}

var turnstileHTTPClient = &http.Client{Timeout: 10 * time.Second}

func TurnstileCheck() gin.HandlerFunc {
	return turnstileCheck(false)
}

// Email sends require a fresh, single-use token even when the visitor already
// passed another challenge in this session. Siteverify enforces token reuse.
func TurnstileCheckPerRequest() gin.HandlerFunc {
	return turnstileCheck(true)
}

func turnstileCheck(perRequest bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.TurnstileCheckEnabled {
			if !perRequest {
				if sessions.Default(c).Get("turnstile") != nil {
					c.Next()
					return
				}
			}
			response := c.Query("turnstile")
			if response == "" {
				c.JSON(http.StatusOK, gin.H{
					"success": false,
					"message": "Turnstile token 为空",
				})
				c.Abort()
				return
			}
			form := url.Values{
				"secret":   {common.TurnstileSecretKey},
				"response": {response},
				"remoteip": {c.ClientIP()},
			}
			req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost,
				"https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
			if err != nil {
				rejectTurnstile(c)
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rawRes, err := turnstileHTTPClient.Do(req)
			if err != nil {
				rejectTurnstile(c)
				return
			}
			defer rawRes.Body.Close()
			if rawRes.StatusCode != http.StatusOK {
				rejectTurnstile(c)
				return
			}
			var res turnstileCheckResponse
			err = common.DecodeJson(io.LimitReader(rawRes.Body, 64*1024), &res)
			if err != nil || !res.Success {
				rejectTurnstile(c)
				return
			}
			if !perRequest {
				session := sessions.Default(c)
				session.Set("turnstile", true)
				if err = session.Save(); err != nil {
					c.AbortWithStatusJSON(http.StatusOK, gin.H{
						"message": "无法保存会话信息，请重试",
						"success": false,
					})
					return
				}
			}
		}
		c.Next()
	}
}

func rejectTurnstile(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusOK, gin.H{
		"success": false,
		"message": "Turnstile 校验失败，请刷新重试！",
	})
}
