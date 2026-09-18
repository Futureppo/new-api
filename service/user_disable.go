package service

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

var userDisableExpiryOnce sync.Once

func StartUserDisableExpiryTask() {
	if !common.IsMasterNode {
		return
	}
	userDisableExpiryOnce.Do(func() {
		go func() {
			run := func() {
				if err := model.ExpireDueUserDisables(time.Now().Unix()); err != nil {
					common.SysLog("user disable expiry task failed: " + err.Error())
				}
			}
			run()
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				run()
			}
		}()
	})
}

func UserDisabledMessage(c *gin.Context, user *model.UserBase, fallback string) string {
	message := common.TranslateMessage(c, fallback)
	if reason := strings.TrimSpace(user.DisableReason); reason != "" {
		message = common.TranslateMessage(c, i18n.MsgUserDisabledWithReason, map[string]any{"Reason": reason})
	}
	if user.DisableUntil <= 0 {
		return message + " (" + common.TranslateMessage(c, "user.disable_permanent") + ")"
	}
	return message + " (" + common.TranslateMessage(c, "user.disable_period", map[string]any{
		"Minutes": user.DisableDurationMinutes,
		"Until":   time.Unix(user.DisableUntil, 0).Format(time.RFC3339),
	}) + ")"
}
