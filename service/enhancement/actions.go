package enhancement

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
)

func GenerateRedemptions(req GenerateRedemptionsRequest, operatorId int) ([]RedemptionSummary, error) {
	if req.Count <= 0 || req.Count > MaxGenerateRedemption {
		return nil, fmt.Errorf("count must be between 1 and %d", MaxGenerateRedemption)
	}
	if req.Quota <= 0 {
		return nil, errors.New("quota must be greater than 0")
	}
	if len([]rune(req.Name)) > 64 {
		return nil, errors.New("name is too long")
	}
	if req.ExpiredTime < 0 {
		return nil, errors.New("expired_time is invalid")
	}

	redemptions := make([]model.Redemption, 0, req.Count)
	now := common.GetTimestamp()
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "enhancement"
	}
	for i := 0; i < req.Count; i++ {
		redemptions = append(redemptions, model.Redemption{
			UserId:      operatorId,
			Key:         common.GetUUID(),
			Status:      common.RedemptionCodeStatusEnabled,
			Name:        name,
			Quota:       req.Quota,
			CreatedTime: now,
			ExpiredTime: req.ExpiredTime,
		})
	}
	if err := model.DB.Create(&redemptions).Error; err != nil {
		return nil, err
	}
	audit(operatorId, "enhancements.redemptions", "generate", map[string]interface{}{
		"count":        req.Count,
		"quota":        req.Quota,
		"expired_time": req.ExpiredTime,
	})
	out := make([]RedemptionSummary, 0, len(redemptions))
	for _, redemption := range redemptions {
		out = append(out, redemptionToSummary(redemption, true))
	}
	return out, nil
}

func DeleteRedemption(id int, operatorId int, force bool) error {
	if id <= 0 {
		return errors.New("invalid redemption id")
	}
	var redemption model.Redemption
	if err := model.DB.Where("id = ?", id).First(&redemption).Error; err != nil {
		return err
	}
	if !force && redemption.Status == common.RedemptionCodeStatusUsed {
		return errors.New("used redemption codes require root permission to delete")
	}
	if err := redemption.Delete(); err != nil {
		return err
	}
	audit(operatorId, "enhancements.redemptions", "delete", map[string]interface{}{
		"redemption_id": id,
		"force":         force,
	})
	return nil
}

func DisableRedemption(id int, operatorId int) (RedemptionSummary, error) {
	if id <= 0 {
		return RedemptionSummary{}, errors.New("invalid redemption id")
	}
	var redemption model.Redemption
	if err := model.DB.Where("id = ?", id).First(&redemption).Error; err != nil {
		return RedemptionSummary{}, err
	}
	if redemption.Status == common.RedemptionCodeStatusUsed {
		return RedemptionSummary{}, errors.New("used redemption codes cannot be disabled")
	}
	if redemption.Status != common.RedemptionCodeStatusDisabled {
		redemption.Status = common.RedemptionCodeStatusDisabled
		if err := model.DB.Model(&redemption).Update("status", common.RedemptionCodeStatusDisabled).Error; err != nil {
			return RedemptionSummary{}, err
		}
		audit(operatorId, "enhancements.redemptions", "disable", map[string]interface{}{
			"redemption_id": id,
		})
	}
	return redemptionToSummary(redemption, false), nil
}

func EnableRedemption(id int, operatorId int) (RedemptionSummary, error) {
	if id <= 0 {
		return RedemptionSummary{}, errors.New("invalid redemption id")
	}
	var redemption model.Redemption
	if err := model.DB.Where("id = ?", id).First(&redemption).Error; err != nil {
		return RedemptionSummary{}, err
	}
	if redemption.Status == common.RedemptionCodeStatusUsed {
		return RedemptionSummary{}, errors.New("used redemption codes cannot be enabled")
	}
	if redemption.Status != common.RedemptionCodeStatusEnabled {
		redemption.Status = common.RedemptionCodeStatusEnabled
		if err := model.DB.Model(&redemption).Update("status", common.RedemptionCodeStatusEnabled).Error; err != nil {
			return RedemptionSummary{}, err
		}
		audit(operatorId, "enhancements.redemptions", "enable", map[string]interface{}{
			"redemption_id": id,
		})
	}
	return redemptionToSummary(redemption, false), nil
}

func BatchDeleteRedemptions(ids []int, operatorId int, force bool) (map[string]interface{}, error) {
	if len(ids) == 0 {
		return nil, errors.New("ids cannot be empty")
	}
	if len(ids) > MaxBatchOperation {
		return nil, fmt.Errorf("ids exceeds limit %d", MaxBatchOperation)
	}
	success := 0
	failures := make([]map[string]interface{}, 0)
	for _, id := range ids {
		if err := DeleteRedemption(id, operatorId, force); err != nil {
			failures = append(failures, map[string]interface{}{
				"id":      id,
				"message": err.Error(),
			})
			continue
		}
		success++
	}
	audit(operatorId, "enhancements.redemptions", "batch_delete", map[string]interface{}{
		"total":   len(ids),
		"success": success,
		"force":   force,
	})
	return map[string]interface{}{
		"success":  success,
		"failures": failures,
	}, nil
}

func ensureCanOperateUser(operatorId int, operatorRole int, target model.User) error {
	if target.Id <= 0 {
		return errors.New("target user is invalid")
	}
	if target.Role >= common.RoleRootUser {
		return errors.New("root user is protected")
	}
	if target.Id == operatorId {
		return errors.New("current operator cannot modify itself from enhancements")
	}
	if operatorRole < common.RoleRootUser && target.Role >= common.RoleAdminUser {
		return errors.New("admin users require root permission")
	}
	return nil
}

func BanUser(userId int, operatorId int, operatorRole int, reason string) error {
	var user model.User
	if err := model.DB.Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if err := ensureCanOperateUser(operatorId, operatorRole, user); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > 255 {
		return errors.New("reason is too long")
	}
	if reason == "" {
		reason = "enhancement ban"
	}
	user.Status = common.UserStatusDisabled
	user.DisableReason = reason
	if err := user.Update(false); err != nil {
		return err
	}
	_ = model.InvalidateUserTokensCache(user.Id)
	audit(operatorId, "enhancements.users", "ban", map[string]interface{}{
		"target_user_id": user.Id,
		"reason":         reason,
	})
	return nil
}

func UnbanUser(userId int, operatorId int, operatorRole int) error {
	var user model.User
	if err := model.DB.Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if err := ensureCanOperateUser(operatorId, operatorRole, user); err != nil {
		return err
	}
	user.Status = common.UserStatusEnabled
	user.DisableReason = ""
	if err := user.Update(false); err != nil {
		return err
	}
	_ = model.InvalidateUserTokensCache(user.Id)
	audit(operatorId, "enhancements.users", "unban", map[string]interface{}{
		"target_user_id": user.Id,
	})
	return nil
}

func DeleteUser(userId int, operatorId int, operatorRole int) error {
	var user model.User
	if err := model.DB.Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if err := ensureCanOperateUser(operatorId, operatorRole, user); err != nil {
		return err
	}
	if err := user.Delete(); err != nil {
		return err
	}
	_ = model.InvalidateUserTokensCache(user.Id)
	audit(operatorId, "enhancements.users", "delete", map[string]interface{}{
		"target_user_id": user.Id,
	})
	return nil
}

func BatchDeleteUsers(ids []int, operatorId int, operatorRole int) (map[string]interface{}, error) {
	if len(ids) == 0 {
		return nil, errors.New("ids cannot be empty")
	}
	if len(ids) > MaxBatchOperation {
		return nil, fmt.Errorf("ids exceeds limit %d", MaxBatchOperation)
	}
	success := 0
	failures := make([]map[string]interface{}, 0)
	for _, id := range ids {
		if err := DeleteUser(id, operatorId, operatorRole); err != nil {
			failures = append(failures, map[string]interface{}{
				"id":      id,
				"message": err.Error(),
			})
			continue
		}
		success++
	}
	audit(operatorId, "enhancements.users", "batch_delete", map[string]interface{}{
		"total":   len(ids),
		"success": success,
	})
	return map[string]interface{}{
		"success":  success,
		"failures": failures,
	}, nil
}

func PurgeSoftDeletedUsers(operatorId int) (int64, error) {
	var users []model.User
	if err := model.DB.Unscoped().Where("deleted_at IS NOT NULL").Find(&users).Error; err != nil {
		return 0, err
	}
	for _, user := range users {
		if user.Role >= common.RoleRootUser || user.Id == operatorId {
			return 0, errors.New("soft-deleted set contains protected users")
		}
	}
	result := model.DB.Unscoped().Where("deleted_at IS NOT NULL").Delete(&model.User{})
	if result.Error != nil {
		return 0, result.Error
	}
	audit(operatorId, "enhancements.users", "purge_soft_deleted", map[string]interface{}{
		"count": result.RowsAffected,
	})
	return result.RowsAffected, nil
}

func DisableToken(tokenId int, operatorId int) error {
	if tokenId <= 0 {
		return errors.New("invalid token id")
	}
	var token model.Token
	if err := model.DB.Where("id = ?", tokenId).First(&token).Error; err != nil {
		return err
	}
	token.Status = common.TokenStatusDisabled
	if err := token.Update(); err != nil {
		return err
	}
	audit(operatorId, "enhancements.tokens", "disable", map[string]interface{}{
		"token_id": tokenId,
		"user_id":  token.UserId,
	})
	return nil
}

func ClearEnhancementCache(operatorId int) map[string]interface{} {
	audit(operatorId, "enhancements.system", "clear_cache", map[string]interface{}{
		"scope": "enhancements",
	})
	return map[string]interface{}{
		"cleared": true,
		"scope":   "enhancements",
	}
}

func SaveSelectedModels(models []string, operatorId int) error {
	models, err := validateModelList(models, false)
	if err != nil {
		return err
	}
	bytes, err := common.Marshal(models)
	if err != nil {
		return err
	}
	if err := model.UpdateOption("enhancement_setting.selected_models", string(bytes)); err != nil {
		return err
	}
	audit(operatorId, "enhancements.model_status", "save_selected_models", map[string]interface{}{
		"count": len(models),
	})
	return nil
}

func SaveModelStatusOption(key string, value string, operatorId int) error {
	allowed := map[string]struct{}{
		"public_embed_enabled":          {},
		"model_status_time_window_mins": {},
		"model_status_refresh_seconds":  {},
		"model_status_theme":            {},
		"model_status_sort_mode":        {},
		"model_status_site_title":       {},
	}
	if _, ok := allowed[key]; !ok {
		return errors.New("unsupported model status option")
	}
	value = strings.TrimSpace(value)
	switch key {
	case "public_embed_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return errors.New("public embed enabled must be boolean")
		}
	case "model_status_time_window_mins":
		minutes, err := strconv.Atoi(value)
		if err != nil || minutes < 1 || minutes > int(MaxAdminQueryWindow.Minutes()) {
			return fmt.Errorf("time window must be between 1 and %d minutes", int(MaxAdminQueryWindow.Minutes()))
		}
	case "model_status_refresh_seconds":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 5 || seconds > 3600 {
			return errors.New("refresh interval must be between 5 and 3600 seconds")
		}
	case "model_status_theme":
		if value != "light" && value != "dark" && value != "system" {
			return errors.New("unsupported model status theme")
		}
	case "model_status_sort_mode":
		if value != "name" && value != "status" && value != "requests" && value != "error_rate" && value != "custom" {
			return errors.New("unsupported model status sort mode")
		}
	case "model_status_site_title":
		if len([]rune(value)) > 80 {
			return errors.New("site title is too long")
		}
	}
	if err := model.UpdateOption("enhancement_setting."+key, value); err != nil {
		return err
	}
	audit(operatorId, "enhancements.model_status", "save_option", map[string]interface{}{
		"key": key,
	})
	return nil
}

func AIBanConfig() map[string]interface{} {
	cfg := setting.GetEnhancementSetting()
	return map[string]interface{}{
		"enabled":       cfg.AIBanEnabled,
		"dry_run":       cfg.AIBanDryRun,
		"model":         cfg.AIBanModel,
		"base_url":      common.MaskSensitiveInfo(cfg.AIBanBaseURL),
		"api_key_set":   cfg.AIBanAPIKey != "",
		"safe_defaults": true,
	}
}

func isBlockedExternalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// 100.64.0.0/10 carrier-grade NAT and 198.18.0.0/15 benchmarking ranges
		// are not globally routable and should not be accepted for external AI calls.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return true
		}
	}
	return false
}

func validateExternalURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("only http and https URLs are allowed")
	}
	host := parsed.Hostname()
	if host == "" {
		return errors.New("host is required")
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if isBlockedExternalIP(ip) {
			return errors.New("private, loopback, link-local, and non-global addresses are not allowed")
		}
	}
	return nil
}

func SaveAIBanConfig(values map[string]interface{}, operatorId int) error {
	cfg := setting.GetEnhancementSetting()
	baseURL := cfg.AIBanBaseURL
	modelName := cfg.AIBanModel
	apiKey := cfg.AIBanAPIKey
	enabled := cfg.AIBanEnabled
	dryRun := cfg.AIBanDryRun
	apiKeyChanged := false

	if raw, ok := values["base_url"].(string); ok {
		baseURL = strings.TrimSpace(raw)
		if err := validateExternalURL(baseURL); err != nil {
			return err
		}
	}
	if raw, ok := values["model"].(string); ok {
		modelName = strings.TrimSpace(raw)
		if len([]rune(modelName)) > 128 {
			return errors.New("model is too long")
		}
	}
	if raw, ok := values["enabled"].(bool); ok {
		enabled = raw
	}
	if raw, ok := values["dry_run"].(bool); ok {
		dryRun = raw
	}
	if raw, ok := values["api_key"].(string); ok && strings.TrimSpace(raw) != "" {
		apiKey = strings.TrimSpace(raw)
		apiKeyChanged = true
	}
	if enabled && !dryRun && (modelName == "" || apiKey == "") {
		return errors.New("AI ban requires model and API key before disabling dry-run")
	}

	if _, ok := values["base_url"].(string); ok {
		if err := model.UpdateOption("enhancement_setting.ai_ban_base_url", baseURL); err != nil {
			return err
		}
	}
	if _, ok := values["model"].(string); ok {
		if err := model.UpdateOption("enhancement_setting.ai_ban_model", modelName); err != nil {
			return err
		}
	}
	if _, ok := values["enabled"].(bool); ok {
		if err := model.UpdateOption("enhancement_setting.ai_ban_enabled", fmt.Sprintf("%t", enabled)); err != nil {
			return err
		}
	}
	if _, ok := values["dry_run"].(bool); ok {
		if err := model.UpdateOption("enhancement_setting.ai_ban_dry_run", fmt.Sprintf("%t", dryRun)); err != nil {
			return err
		}
	}
	if apiKeyChanged {
		if err := model.UpdateOption("enhancement_setting.ai_ban_api_key", apiKey); err != nil {
			return err
		}
	}
	audit(operatorId, "enhancements.ai_ban", "save_config", map[string]interface{}{
		"api_key_changed": apiKeyChanged,
	})
	return nil
}

func AutoGroupConfig() map[string]interface{} {
	return map[string]interface{}{
		"default_use_auto_group": setting.DefaultUseAutoGroup,
		"auto_groups":            setting.GetAutoGroups(),
		"dry_run_default":        true,
	}
}

func AutoGroupPreview(limit int) ([]UserSummary, error) {
	limit = clampLimit(limit)
	autoGroups := setting.GetAutoGroups()
	if len(autoGroups) == 0 {
		return []UserSummary{}, nil
	}
	var users []model.User
	if err := model.DB.Omit("password").
		Where("status = ?", common.UserStatusEnabled).
		Order("used_quota DESC").
		Limit(limit).
		Find(&users).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	out := make([]UserSummary, 0, len(users))
	for _, user := range users {
		if user.Role >= common.RoleRootUser {
			continue
		}
		out = append(out, userToSummary(user))
	}
	return out, nil
}
