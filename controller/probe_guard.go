package controller

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func PreviewProbeGuardText(c *gin.Context) {
	var req struct {
		Text  string `json:"text"`
		Rules string `json:"rules"`
	}
	if err := common.DecodeJson(http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024), &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if _, err := setting.ParseProbeGuardTextRules(req.Rules); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Text) > 64*1024 {
		common.ApiError(c, fmt.Errorf("测试文本不能超过64 KiB"))
		return
	}
	match := service.MatchProbeGuardText(req.Text, req.Rules)
	common.ApiSuccess(c, gin.H{"matched": match != nil, "match": match})
}

func GetProbeGuardUserState(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiError(c, fmt.Errorf("用户 ID 无效"))
		return
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	state, err := model.GetProbeUserAbuseState(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"state": state, "status": user.Status, "disable_reason": user.DisableReason, "disable_until": user.DisableUntil})
}

func ResetProbeGuardUserState(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiError(c, fmt.Errorf("用户 ID 无效"))
		return
	}
	if err := model.ResetProbeUserAbuseState(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLogWithAdminInfo(userId, model.LogTypeManage, "管理员重置单轮测活违规次数（账号封禁状态不变）", map[string]interface{}{
		"admin_id": c.GetInt("id"), "admin_username": c.GetString("username"),
	})
	common.ApiSuccess(c, nil)
}
