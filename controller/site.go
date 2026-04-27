package controller

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type groupTransferRequest struct {
	SourceGroup string `json:"source_group"`
	TargetGroup string `json:"target_group"`
}

type groupBalanceRequest struct {
	Group string `json:"group"`
	Mode  string `json:"mode"`
	Quota int    `json:"quota"`
}

func GetGroupTransferOptions(c *gin.Context) {
	sourceGroups, err := model.GetActiveUserGroupCounts()
	if err != nil {
		common.ApiError(c, err)
		return
	}

	targetGroups := make([]string, 0)
	for groupName := range ratio_setting.GetGroupRatioCopy() {
		targetGroups = append(targetGroups, groupName)
	}
	sort.Strings(targetGroups)

	common.ApiSuccess(c, gin.H{
		"source_groups": sourceGroups,
		"target_groups": targetGroups,
	})
}

func PreviewGroupBalance(c *gin.Context) {
	quota, err := strconv.Atoi(strings.TrimSpace(c.Query("quota")))
	if err != nil {
		common.ApiError(c, errors.New("额度必须为正整数"))
		return
	}

	group, mode, quota, err := validateGroupBalance(c.Query("group"), c.Query("mode"), quota)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	preview, err := model.PreviewActiveUsersGroupBalance(group, mode, quota)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, preview)
}

func UpdateGroupBalance(c *gin.Context) {
	var req groupBalanceRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, errors.New("请求参数无效"))
		return
	}

	group, mode, quota, err := validateGroupBalance(req.Group, req.Mode, req.Quota)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	preview, _, err := model.UpdateActiveUsersGroupBalance(group, mode, quota)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	adminID := c.GetInt("id")
	adminUsername := c.GetString("username")
	model.RecordLogWithAdminInfo(adminID, model.LogTypeManage,
		fmt.Sprintf("管理员 %s 批量%s分组 %s 用户余额 %s，影响 %d 人，总变化 %s",
			adminUsername, groupBalanceModeText(mode), group, logger.LogQuota(quota), preview.Affected, logger.LogQuota(int(preview.TotalDelta))),
		map[string]interface{}{
			"admin_id":       adminID,
			"admin_username": adminUsername,
			"group":          group,
			"mode":           mode,
			"quota":          quota,
			"affected":       preview.Affected,
			"total_delta":    preview.TotalDelta,
		},
	)

	common.ApiSuccess(c, preview)
}

func PreviewGroupTransfer(c *gin.Context) {
	sourceGroup, targetGroup, err := validateGroupTransfer(c.Query("source_group"), c.Query("target_group"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	count, err := model.CountActiveUsersByGroup(sourceGroup)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, gin.H{
		"source_group": sourceGroup,
		"target_group": targetGroup,
		"affected":     count,
	})
}

func TransferGroupUsers(c *gin.Context) {
	var req groupTransferRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, errors.New("请求参数无效"))
		return
	}

	sourceGroup, targetGroup, err := validateGroupTransfer(req.SourceGroup, req.TargetGroup)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	_, affected, err := model.TransferActiveUsersGroup(sourceGroup, targetGroup)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	adminID := c.GetInt("id")
	adminUsername := c.GetString("username")
	model.RecordLogWithAdminInfo(adminID, model.LogTypeManage,
		fmt.Sprintf("管理员 %s 将 %d 个用户从 %s 分组调整为 %s", adminUsername, affected, sourceGroup, targetGroup),
		map[string]interface{}{
			"admin_id":       adminID,
			"admin_username": adminUsername,
			"source_group":   sourceGroup,
			"target_group":   targetGroup,
			"affected":       affected,
		},
	)

	common.ApiSuccess(c, gin.H{
		"source_group": sourceGroup,
		"target_group": targetGroup,
		"affected":     affected,
	})
}

func validateGroupTransfer(sourceGroup string, targetGroup string) (string, string, error) {
	sourceGroup = strings.TrimSpace(sourceGroup)
	targetGroup = strings.TrimSpace(targetGroup)
	if sourceGroup == "" || targetGroup == "" {
		return "", "", errors.New("源分组和目标分组不能为空")
	}
	if sourceGroup == targetGroup {
		return "", "", errors.New("源分组和目标分组不能相同")
	}
	if !ratio_setting.ContainsGroupRatio(targetGroup) {
		return "", "", errors.New("目标分组不存在")
	}
	return sourceGroup, targetGroup, nil
}

func validateGroupBalance(group string, mode string, quota int) (string, string, int, error) {
	group = strings.TrimSpace(group)
	mode = strings.TrimSpace(mode)
	if group == "" {
		return "", "", 0, errors.New("分组不能为空")
	}
	if quota <= 0 {
		return "", "", 0, errors.New("额度必须为正整数")
	}
	switch mode {
	case model.UserGroupBalanceModeAdd, model.UserGroupBalanceModeSubtract, model.UserGroupBalanceModeOverride:
		return group, mode, quota, nil
	default:
		return "", "", 0, errors.New("余额调整方式无效")
	}
}

func groupBalanceModeText(mode string) string {
	switch mode {
	case model.UserGroupBalanceModeAdd:
		return "增加"
	case model.UserGroupBalanceModeSubtract:
		return "减少"
	case model.UserGroupBalanceModeOverride:
		return "覆盖"
	default:
		return mode
	}
}
