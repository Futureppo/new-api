package enhancement

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
)

func clampPage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return DefaultPageSize
	}
	if limit > MaxPageSize {
		return MaxPageSize
	}
	return limit
}

func offset(page int, limit int) int {
	return (clampPage(page) - 1) * clampLimit(limit)
}

func queryWindow(start int64, end int64, maxWindow time.Duration) (int64, int64) {
	now := common.GetTimestamp()
	if end <= 0 || end > now {
		end = now
	}
	if start <= 0 {
		start = end - int64(DefaultQueryWindow.Seconds())
	}
	minStart := end - int64(maxWindow.Seconds())
	if start < minStart {
		start = minStart
	}
	if start > end {
		start = end - int64(time.Hour.Seconds())
	}
	return start, end
}

func timeWindowMinutes(minutes int, public bool) int {
	if minutes <= 0 {
		minutes = setting.GetEnhancementSetting().ModelStatusTimeWindowMins
	}
	if minutes <= 0 {
		minutes = 60
	}
	maxMinutes := int(MaxAdminQueryWindow.Minutes())
	if public {
		maxMinutes = int(MaxPublicQueryWindow.Minutes())
	}
	if minutes > maxMinutes {
		minutes = maxMinutes
	}
	return minutes
}

func maskedKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + "********" + key[len(key)-4:]
}

func userToSummary(user model.User) UserSummary {
	email := strings.TrimSpace(user.Email)
	if email == "" {
		email = "未绑定"
	}
	return UserSummary{
		Id:            user.Id,
		Username:      user.Username,
		DisplayName:   user.DisplayName,
		Role:          user.Role,
		Status:        user.Status,
		DisableReason: user.DisableReason,
		Email:         email,
		Quota:         user.Quota,
		UsedQuota:     user.UsedQuota,
		RequestCount:  user.RequestCount,
		Group:         user.Group,
		InviterId:     user.InviterId,
		AffCount:      user.AffCount,
		LinuxDOId:     user.LinuxDOId,
	}
}

func tokenToSummary(token model.Token) TokenSummary {
	allowIps := ""
	if token.AllowIps != nil {
		allowIps = *token.AllowIps
	}
	return TokenSummary{
		Id:                 token.Id,
		UserId:             token.UserId,
		Name:               token.Name,
		Key:                token.GetFullKey(),
		Status:             token.Status,
		Group:              token.Group,
		CreatedTime:        token.CreatedTime,
		AccessedTime:       token.AccessedTime,
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UsedQuota:          token.UsedQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		AllowIps:           allowIps,
	}
}

func redemptionToSummary(redemption model.Redemption, revealKey bool) RedemptionSummary {
	key := maskedKey(redemption.Key)
	if revealKey {
		key = redemption.Key
	}
	return RedemptionSummary{
		Id:           redemption.Id,
		UserId:       redemption.UserId,
		Key:          key,
		Status:       redemption.Status,
		Name:         redemption.Name,
		Quota:        redemption.Quota,
		CreatedTime:  redemption.CreatedTime,
		RedeemedTime: redemption.RedeemedTime,
		UsedUserId:   redemption.UsedUserId,
		ExpiredTime:  redemption.ExpiredTime,
	}
}

func redemptionToSummaryWithUsername(redemption model.Redemption, revealKey bool, usedUsername string) RedemptionSummary {
	summary := redemptionToSummary(redemption, revealKey)
	summary.UsedUsername = usedUsername
	return summary
}

func userSettingFromString(raw string) dto.UserSetting {
	settingMap := dto.UserSetting{}
	if strings.TrimSpace(raw) == "" {
		return settingMap
	}
	if err := common.UnmarshalJsonStr(raw, &settingMap); err != nil {
		common.SysLog("failed to unmarshal user setting: " + err.Error())
		return dto.UserSetting{}
	}
	return settingMap
}

func isRecordIPLogEnabled(raw string) bool {
	return userSettingFromString(raw).RecordIpLog
}

func forceRecordIPLogSetting(raw string) (string, bool, error) {
	settingMap := userSettingFromString(raw)
	if settingMap.RecordIpLog {
		return raw, false, nil
	}
	settingMap.RecordIpLog = true
	bytes, err := common.Marshal(settingMap)
	if err != nil {
		return "", false, err
	}
	return string(bytes), true, nil
}

func selectedModelSet() map[string]struct{} {
	selected := setting.GetEnhancementSetting().SelectedModels
	out := make(map[string]struct{}, len(selected))
	for _, modelName := range selected {
		modelName = strings.TrimSpace(modelName)
		if modelName != "" {
			out[modelName] = struct{}{}
		}
	}
	return out
}

func filterPublicModels(models []string) []string {
	allowed := selectedModelSet()
	if len(allowed) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(models))
	for _, modelName := range models {
		if _, ok := allowed[modelName]; ok {
			out = append(out, modelName)
		}
	}
	sort.Strings(out)
	if len(out) > MaxPublicModelCount {
		return out[:MaxPublicModelCount]
	}
	return out
}

func validateModelList(models []string, public bool) ([]string, error) {
	maxCount := MaxAdminModelCount
	if public {
		maxCount = MaxPublicModelCount
	}
	if len(models) > maxCount {
		return nil, fmt.Errorf("model count exceeds limit %d", maxCount)
	}
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, modelName := range models {
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		if len(modelName) > 128 {
			return nil, errors.New("model name is too long")
		}
		if _, ok := seen[modelName]; ok {
			continue
		}
		seen[modelName] = struct{}{}
		out = append(out, modelName)
	}
	if public {
		out = filterPublicModels(out)
	}
	return out, nil
}

func requirePublicEmbedEnabled() error {
	if !setting.GetEnhancementSetting().PublicEmbedEnabled {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func RequirePublicEmbedEnabled() error {
	return requirePublicEmbedEnabled()
}

func audit(operatorId int, module string, action string, payload map[string]interface{}) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	payload["module"] = module
	payload["action"] = action
	payload["operator_id"] = operatorId
	bytes, err := common.Marshal(payload)
	if err != nil {
		model.RecordLog(operatorId, model.LogTypeManage, module+"."+action)
		return
	}
	model.RecordLog(operatorId, model.LogTypeManage, string(bytes))
}
