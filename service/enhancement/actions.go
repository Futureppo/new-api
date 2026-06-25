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
	return redemptionToSummary(redemption, true), nil
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
	return redemptionToSummary(redemption, true), nil
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

func PurgeSoftDeletedUsers(operatorId int, operatorRole int) (int64, error) {
	if operatorRole < common.RoleAdminUser {
		return 0, errors.New("admin permission required")
	}

	maxDeletedRole := common.RoleAdminUser
	if operatorRole >= common.RoleRootUser {
		maxDeletedRole = common.RoleRootUser
	}

	result := model.DB.Unscoped().
		Where("deleted_at IS NOT NULL").
		Where("role < ?", maxDeletedRole).
		Delete(&model.User{})
	if result.Error != nil {
		return 0, result.Error
	}
	audit(operatorId, "enhancements.users", "purge_soft_deleted", map[string]interface{}{
		"count":    result.RowsAffected,
		"max_role": maxDeletedRole,
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

func EnableAllRecordIPLog(operatorId int) (map[string]interface{}, error) {
	var users []struct {
		Id      int    `gorm:"column:id"`
		Setting string `gorm:"column:setting"`
	}
	if err := model.DB.Model(&model.User{}).
		Select("id, setting").
		Find(&users).Error; err != nil {
		return nil, err
	}

	updatedIDs := make([]int, 0)
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		for _, user := range users {
			nextSetting, changed, err := forceRecordIPLogSetting(user.Setting)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if err := tx.Model(&model.User{}).Where("id = ?", user.Id).Update("setting", nextSetting).Error; err != nil {
				return err
			}
			updatedIDs = append(updatedIDs, user.Id)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, userId := range updatedIDs {
		_ = model.InvalidateUserCache(userId)
	}
	audit(operatorId, "enhancements.risk", "enable_all_record_ip_log", map[string]interface{}{
		"updated": len(updatedIDs),
	})
	stats, err := IPLogCoverageStats()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"updated":  len(updatedIDs),
		"coverage": stats,
	}, nil
}

func BanSharedTokenIPUsers(ip string, query IPRiskQuery, operatorId int, operatorRole int, reason string) (map[string]interface{}, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, errors.New("ip cannot be empty")
	}
	query = normalizeIPRiskQuery(query)
	rows, err := listIPRiskLogs(query.Start, query.End)
	if err != nil {
		return nil, err
	}

	usersByID := make(map[int]*IPRiskUserRef)
	for _, row := range rows {
		if row.IP != ip || row.UserId <= 0 || row.TokenId <= 0 {
			continue
		}
		if user, ok := usersByID[row.UserId]; ok {
			user.RequestCount++
			if user.Username == "" {
				user.Username = row.Username
			}
			continue
		}
		usersByID[row.UserId] = &IPRiskUserRef{
			UserId:       row.UserId,
			Username:     row.Username,
			RequestCount: 1,
		}
	}

	users := sortedIPRiskUsers(usersByID)
	if len(users) == 0 {
		return nil, errors.New("no users found for this ip in the selected window")
	}
	if len(users) > MaxBatchOperation {
		return nil, fmt.Errorf("users under this ip exceeds limit %d", MaxBatchOperation)
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = fmt.Sprintf("risk shared ip ban: %s", ip)
	}
	success := 0
	failures := make([]map[string]interface{}, 0)
	for _, user := range users {
		if err := BanUser(user.UserId, operatorId, operatorRole, reason); err != nil {
			failures = append(failures, map[string]interface{}{
				"id":       user.UserId,
				"username": user.Username,
				"message":  err.Error(),
			})
			continue
		}
		success++
	}

	audit(operatorId, "enhancements.risk", "ban_shared_ip_users", map[string]interface{}{
		"ip":      ip,
		"total":   len(users),
		"success": success,
		"fail":    len(failures),
		"start":   query.Start,
		"end":     query.End,
	})
	return map[string]interface{}{
		"ip":          ip,
		"total_users": len(users),
		"success":     success,
		"failures":    failures,
		"users":       users,
	}, nil
}

func UpdateToken(tokenId int, req UpdateTokenRequest, operatorId int) (TokenSummary, error) {
	if tokenId <= 0 {
		return TokenSummary{}, errors.New("invalid token id")
	}
	if len([]rune(req.Name)) > 50 {
		return TokenSummary{}, errors.New("token name is too long")
	}
	switch req.Status {
	case common.TokenStatusEnabled, common.TokenStatusDisabled, common.TokenStatusExpired, common.TokenStatusExhausted:
	default:
		return TokenSummary{}, errors.New("invalid token status")
	}
	if !req.UnlimitedQuota {
		if req.RemainQuota < 0 {
			return TokenSummary{}, errors.New("remain quota cannot be negative")
		}
		maxQuotaValue := int(1000000000 * common.QuotaPerUnit)
		if req.RemainQuota > maxQuotaValue {
			return TokenSummary{}, fmt.Errorf("remain quota cannot exceed %d", maxQuotaValue)
		}
	}
	if len([]rune(req.Group)) > 64 {
		return TokenSummary{}, errors.New("group is too long")
	}
	if len([]rune(req.ModelLimits)) > 4096 {
		return TokenSummary{}, errors.New("model limits are too long")
	}
	if len([]rune(req.AllowIps)) > 4096 {
		return TokenSummary{}, errors.New("allow ips are too long")
	}

	var token model.Token
	if err := model.DB.Where("id = ?", tokenId).First(&token).Error; err != nil {
		return TokenSummary{}, err
	}

	token.Name = strings.TrimSpace(req.Name)
	token.Status = req.Status
	token.ExpiredTime = req.ExpiredTime
	token.RemainQuota = req.RemainQuota
	token.UnlimitedQuota = req.UnlimitedQuota
	token.ModelLimitsEnabled = req.ModelLimitsEnabled && strings.TrimSpace(req.ModelLimits) != ""
	token.ModelLimits = strings.TrimSpace(req.ModelLimits)
	allowIps := strings.TrimSpace(req.AllowIps)
	token.AllowIps = &allowIps
	token.Group = strings.TrimSpace(req.Group)

	if token.Status == common.TokenStatusEnabled {
		if token.ExpiredTime != -1 && token.ExpiredTime <= common.GetTimestamp() {
			return TokenSummary{}, errors.New("expired tokens cannot be enabled")
		}
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			return TokenSummary{}, errors.New("exhausted tokens cannot be enabled")
		}
	}

	if err := token.Update(); err != nil {
		return TokenSummary{}, err
	}
	audit(operatorId, "enhancements.tokens", "update", map[string]interface{}{
		"token_id": token.Id,
		"user_id":  token.UserId,
	})
	return tokenToSummary(token), nil
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

const (
	maxModelStatusIgnoredErrorKeywords     = 100
	maxModelStatusIgnoredErrorKeywordRunes = 200
)

func parseModelStatusIgnoredErrorKeywords(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return []string{}, nil
	}

	var raw []string
	if strings.HasPrefix(value, "[") {
		if err := common.Unmarshal([]byte(value), &raw); err != nil {
			return nil, errors.New("ignored error keywords must be a JSON string array or newline separated text")
		}
	} else {
		value = strings.ReplaceAll(value, "\r\n", "\n")
		value = strings.ReplaceAll(value, "\r", "\n")
		raw = strings.Split(value, "\n")
	}
	return normalizeModelStatusIgnoredErrorKeywords(raw)
}

func normalizeModelStatusIgnoredErrorKeywords(raw []string) ([]string, error) {
	keywords := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		keyword := strings.TrimSpace(item)
		if keyword == "" {
			continue
		}
		if len([]rune(keyword)) > maxModelStatusIgnoredErrorKeywordRunes {
			return nil, fmt.Errorf("ignored error keyword must be at most %d characters", maxModelStatusIgnoredErrorKeywordRunes)
		}
		key := strings.ToLower(keyword)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		keywords = append(keywords, keyword)
		if len(keywords) > maxModelStatusIgnoredErrorKeywords {
			return nil, fmt.Errorf("ignored error keywords must be at most %d entries", maxModelStatusIgnoredErrorKeywords)
		}
	}
	return keywords, nil
}

func SaveModelStatusOption(key string, value string, operatorId int) error {
	allowed := map[string]struct{}{
		"public_embed_enabled":                       {},
		"model_status_time_window_mins":              {},
		"model_status_refresh_seconds":               {},
		"model_status_slot_minutes":                  {},
		"model_status_green_threshold":               {},
		"model_status_yellow_threshold":              {},
		"model_status_show_zero_requests":            {},
		"model_status_ignore_error_keywords_enabled": {},
		"model_status_ignored_error_keywords":        {},
		"model_status_theme":                         {},
		"model_status_sort_mode":                     {},
		"model_status_site_title":                    {},
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
		if err != nil {
			value = strconv.Itoa(ModelStatusWindowToMinutes(value))
			minutes, err = strconv.Atoi(value)
		}
		if err != nil || !IsAllowedModelStatusWindowMinutes(minutes) {
			return errors.New("time window must be today, 24h, 7d, or 30d")
		}
	case "model_status_refresh_seconds":
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 60 || seconds > 24*60*60 {
			return errors.New("refresh interval must be between 1 and 1440 minutes")
		}
	case "model_status_slot_minutes":
		minutes, err := strconv.Atoi(value)
		if err != nil || minutes < 5 || minutes > 24*60 {
			return errors.New("slot granularity must be between 5 and 1440 minutes")
		}
	case "model_status_green_threshold", "model_status_yellow_threshold":
		threshold, err := strconv.ParseFloat(value, 64)
		if err != nil || threshold <= 0 || threshold > 100 {
			return errors.New("status threshold must be between 0 and 100")
		}
	case "model_status_show_zero_requests":
		if _, err := strconv.ParseBool(value); err != nil {
			return errors.New("show zero request models must be boolean")
		}
	case "model_status_ignore_error_keywords_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return errors.New("ignore error keywords enabled must be boolean")
		}
	case "model_status_ignored_error_keywords":
		keywords, err := parseModelStatusIgnoredErrorKeywords(value)
		if err != nil {
			return err
		}
		bytes, err := common.Marshal(keywords)
		if err != nil {
			return err
		}
		value = string(bytes)
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
	ClearModelStatusPublicCache()
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
