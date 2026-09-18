package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// OAuth callbacks are public during login, but become account-management
// operations when an authenticated session is present (account binding).
func PasswordSetupForSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		if id, ok := sessions.Default(c).Get("id").(int); ok {
			if !requireUserPassword(c, id) {
				return
			}
		}
		c.Next()
	}
}

func requireUserPassword(c *gin.Context, userID int) bool {
	hasPassword, err := model.HasUserPassword(userID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgDatabaseError)})
		return false
	}
	c.Set("has_password", hasPassword)
	if hasPassword || (c.Request.Method == http.MethodGet && c.FullPath() == "/api/user/self") ||
		(c.Request.Method == http.MethodPut && c.FullPath() == "/api/user/self/password") {
		return true
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"success": false,
		"code":    "PASSWORD_SETUP_REQUIRED",
		"message": common.TranslateMessage(c, "user.password_setup_required"),
	})
	return false
}
