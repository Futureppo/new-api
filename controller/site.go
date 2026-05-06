package controller

import (
	"errors"
	"fmt"
	"math"
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
	Group  string   `json:"group"`
	Mode   string   `json:"mode"`
	Quota  int      `json:"quota"`
	Factor *float64 `json:"factor"`
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
	mode := strings.TrimSpace(c.Query("mode"))
	quota, factor, err := parseGroupBalanceValue(mode, c.Query("quota"), c.Query("factor"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	group, mode, quota, factor, err := validateGroupBalance(c.Query("group"), mode, quota, factor)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	preview, err := model.PreviewActiveUsersGroupBalance(group, mode, quota, factor)
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

	group, mode, quota, factor, err := validateGroupBalance(req.Group, req.Mode, req.Quota, req.Factor)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	preview, _, err := model.UpdateActiveUsersGroupBalance(group, mode, quota, factor)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	adminID := c.GetInt("id")
	adminUsername := c.GetString("username")
	operationValue := groupBalanceOperationValue(mode, quota, factor)
	logMeta := map[string]interface{}{
		"admin_id":       adminID,
		"admin_username": adminUsername,
		"group":          group,
		"mode":           mode,
		"quota":          quota,
		"affected":       preview.Affected,
		"total_delta":    preview.TotalDelta,
	}
	if factor != nil {
		logMeta["factor"] = *factor
	}
	model.RecordLogWithAdminInfo(adminID, model.LogTypeManage,
		fmt.Sprintf("管理员 %s 批量%s分组 %s 用户余额 %s，影响 %d 人，总变化 %s",
			adminUsername, groupBalanceModeText(mode), group, operationValue, preview.Affected, logger.LogQuota(int(preview.TotalDelta))),
		logMeta,
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

func parseGroupBalanceValue(mode string, quotaRaw string, factorRaw string) (int, *float64, error) {
	if mode == model.UserGroupBalanceModeMultiply {
		factor, err := strconv.ParseFloat(strings.TrimSpace(factorRaw), 64)
		if err != nil {
			return 0, nil, errors.New("倍数必须为大于 0 的数字")
		}
		return 0, &factor, nil
	}

	quota, err := strconv.Atoi(strings.TrimSpace(quotaRaw))
	if err != nil {
		return 0, nil, errors.New("额度必须为正整数")
	}
	return quota, nil, nil
}

func validateGroupBalance(group string, mode string, quota int, factor *float64) (string, string, int, *float64, error) {
	group = strings.TrimSpace(group)
	mode = strings.TrimSpace(mode)
	if group == "" {
		return "", "", 0, nil, errors.New("分组不能为空")
	}
	switch mode {
	case model.UserGroupBalanceModeAdd, model.UserGroupBalanceModeSubtract, model.UserGroupBalanceModeOverride:
		if quota <= 0 {
			return "", "", 0, nil, errors.New("额度必须为正整数")
		}
		return group, mode, quota, nil, nil
	case model.UserGroupBalanceModeMultiply:
		if factor == nil || *factor <= 0 || math.IsNaN(*factor) || math.IsInf(*factor, 0) {
			return "", "", 0, nil, errors.New("倍数必须为大于 0 的数字")
		}
		factorValue := *factor
		return group, mode, 0, &factorValue, nil
	default:
		return "", "", 0, nil, errors.New("余额调整方式无效")
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
	case model.UserGroupBalanceModeMultiply:
		return "翻倍"
	default:
		return mode
	}
}

func groupBalanceOperationValue(mode string, quota int, factor *float64) string {
	if mode == model.UserGroupBalanceModeMultiply && factor != nil {
		return strconv.FormatFloat(*factor, 'f', -1, 64) + " 倍"
	}
	return logger.LogQuota(quota)
}
