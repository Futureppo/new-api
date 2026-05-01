package enhancement

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"gorm.io/gorm"
)

func DashboardOverview() (map[string]interface{}, error) {
	var userCount, enabledUsers, disabledUsers int64
	var tokenCount, channelCount, redemptionCount int64
	if err := model.DB.Model(&model.User{}).Count(&userCount).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.User{}).Where("status = ?", common.UserStatusEnabled).Count(&enabledUsers).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.User{}).Where("status = ?", common.UserStatusDisabled).Count(&disabledUsers).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.Token{}).Count(&tokenCount).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.Channel{}).Count(&channelCount).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.Redemption{}).Count(&redemptionCount).Error; err != nil {
		return nil, err
	}

	since := common.GetTimestamp() - int64(DefaultQueryWindow.Seconds())
	usage, err := UsageSummary(since, common.GetTimestamp())
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"users": map[string]interface{}{
			"total":    userCount,
			"enabled":  enabledUsers,
			"disabled": disabledUsers,
		},
		"tokens":       tokenCount,
		"channels":     channelCount,
		"redemptions":  redemptionCount,
		"last_24h":     usage,
		"generated_at": common.GetTimestamp(),
	}, nil
}

func UsageSummary(start int64, end int64) (UsageTotals, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	var totals UsageTotals
	err := model.LOG_DB.Model(&model.Log{}).
		Select("COUNT(*) AS requests, COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS completion_tokens, COALESCE(AVG(use_time), 0) AS avg_use_time").
		Where("type = ? AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, start, end).
		Scan(&totals).Error
	return totals, err
}

func UsageTrend(start int64, end int64, bucket string) ([]TimePoint, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	type logRow struct {
		CreatedAt        int64
		Quota            int
		PromptTokens     int
		CompletionTokens int
	}
	var rows []logRow
	if err := model.LOG_DB.Model(&model.Log{}).
		Select("created_at, quota, prompt_tokens, completion_tokens").
		Where("type = ? AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, start, end).
		Order("created_at ASC").
		Limit(50000).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	layout := "2006-01-02 15:00"
	truncate := time.Hour
	if bucket == "daily" || bucket == "day" {
		layout = "2006-01-02"
		truncate = 24 * time.Hour
	}

	points := make(map[int64]*TimePoint)
	for _, row := range rows {
		t := time.Unix(row.CreatedAt, 0)
		if truncate == 24*time.Hour {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		} else {
			t = t.Truncate(truncate)
		}
		ts := t.Unix()
		point, ok := points[ts]
		if !ok {
			point = &TimePoint{
				Time:      t.Format(layout),
				Timestamp: ts,
			}
			points[ts] = point
		}
		point.Requests++
		point.Quota += int64(row.Quota)
		point.PromptTokens += int64(row.PromptTokens)
		point.CompletionTokens += int64(row.CompletionTokens)
	}

	keys := make([]int64, 0, len(points))
	for ts := range points {
		keys = append(keys, ts)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]TimePoint, 0, len(keys))
	for _, ts := range keys {
		out = append(out, *points[ts])
	}
	return out, nil
}

func ModelUsageList(start int64, end int64, limit int) ([]ModelUsage, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	limit = clampLimit(limit)
	var models []ModelUsage
	err := model.LOG_DB.Model(&model.Log{}).
		Select("model_name, COUNT(*) AS requests, COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS completion_tokens, COALESCE(AVG(use_time), 0) AS avg_use_time").
		Where("type = ? AND model_name <> '' AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, start, end).
		Group("model_name").
		Order("quota DESC").
		Limit(limit).
		Scan(&models).Error
	if err != nil {
		return nil, err
	}
	var errorsByModel []struct {
		ModelName string `json:"model_name"`
		Count     int64  `json:"count"`
	}
	if err := model.LOG_DB.Model(&model.Log{}).
		Select("model_name, COUNT(*) AS count").
		Where("type = ? AND model_name <> '' AND created_at >= ? AND created_at <= ?", model.LogTypeError, start, end).
		Group("model_name").
		Scan(&errorsByModel).Error; err != nil {
		return nil, err
	}
	errorMap := make(map[string]int64, len(errorsByModel))
	for _, item := range errorsByModel {
		errorMap[item.ModelName] = item.Count
	}
	for i := range models {
		models[i].ErrorCount = errorMap[models[i].ModelName]
	}
	return models, nil
}

func TopUsers(start int64, end int64, limit int) ([]UserUsage, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	limit = clampLimit(limit)
	var users []UserUsage
	err := model.LOG_DB.Model(&model.Log{}).
		Select("user_id, username, COUNT(*) AS requests, COALESCE(SUM(quota), 0) AS quota, COALESCE(MAX(created_at), 0) AS last_activity").
		Where("type = ? AND user_id > 0 AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, start, end).
		Group("user_id, username").
		Order("quota DESC").
		Limit(limit).
		Scan(&users).Error
	return users, err
}

func ChannelSummaries(limit int) ([]ChannelSummary, error) {
	limit = clampLimit(limit)
	var channels []model.Channel
	if err := model.DB.Model(&model.Channel{}).
		Omit("key").
		Order("used_quota DESC").
		Limit(limit).
		Find(&channels).Error; err != nil {
		return nil, err
	}
	out := make([]ChannelSummary, 0, len(channels))
	for _, channel := range channels {
		out = append(out, ChannelSummary{
			Id:           channel.Id,
			Name:         channel.Name,
			Type:         channel.Type,
			Status:       channel.Status,
			Group:        channel.Group,
			Models:       len(channel.GetModels()),
			UsedQuota:    channel.UsedQuota,
			ResponseTime: channel.ResponseTime,
			TestTime:     channel.TestTime,
		})
	}
	return out, nil
}

func redemptionSearchUserIDs(keyword string) ([]int, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, nil
	}

	idSet := map[int]struct{}{}
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
		return []int{id}, nil
	}

	var users []struct {
		Id int `gorm:"column:id"`
	}
	if err := model.DB.Model(&model.User{}).
		Select("id").
		Where("username LIKE ?", "%"+keyword+"%").
		Limit(MaxPageSize * 10).
		Scan(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		if user.Id > 0 {
			idSet[user.Id] = struct{}{}
		}
	}

	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}

func redemptionUsedUsernameMap(redemptions []model.Redemption) (map[int]string, error) {
	idSet := map[int]struct{}{}
	for _, redemption := range redemptions {
		if redemption.UsedUserId > 0 {
			idSet[redemption.UsedUserId] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return map[int]string{}, nil
	}
	ids := make([]int, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	var users []struct {
		Id       int    `gorm:"column:id"`
		Username string `gorm:"column:username"`
	}
	if err := model.DB.Model(&model.User{}).
		Select("id, username").
		Where("id IN ?", ids).
		Scan(&users).Error; err != nil {
		return nil, err
	}
	usernames := make(map[int]string, len(users))
	for _, user := range users {
		usernames[user.Id] = user.Username
	}
	return usernames, nil
}

func ListRedemptions(page int, pageSize int, status int, keyword string) (PageResult[RedemptionSummary], error) {
	page = clampPage(page)
	pageSize = clampLimit(pageSize)
	query := model.DB.Model(&model.Redemption{})
	if status > 0 {
		query = query.Where("status = ?", status)
	}
	if strings.TrimSpace(keyword) != "" {
		userIDs, err := redemptionSearchUserIDs(keyword)
		if err != nil {
			return PageResult[RedemptionSummary]{}, err
		}
		if len(userIDs) == 0 {
			return PageResult[RedemptionSummary]{Items: []RedemptionSummary{}, Total: 0, Page: page, PageSize: pageSize}, nil
		}
		query = query.Where("used_user_id IN ?", userIDs)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return PageResult[RedemptionSummary]{}, err
	}
	var redemptions []model.Redemption
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset(page, pageSize)).Find(&redemptions).Error; err != nil {
		return PageResult[RedemptionSummary]{}, err
	}
	usernames, err := redemptionUsedUsernameMap(redemptions)
	if err != nil {
		return PageResult[RedemptionSummary]{}, err
	}
	items := make([]RedemptionSummary, 0, len(redemptions))
	for _, redemption := range redemptions {
		items = append(items, redemptionToSummaryWithUsername(redemption, false, usernames[redemption.UsedUserId]))
	}
	return PageResult[RedemptionSummary]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func RedemptionStats() (map[string]interface{}, error) {
	statuses := map[string]int{
		"enabled":  common.RedemptionCodeStatusEnabled,
		"disabled": common.RedemptionCodeStatusDisabled,
		"used":     common.RedemptionCodeStatusUsed,
	}
	out := map[string]interface{}{}
	var total int64
	if err := model.DB.Model(&model.Redemption{}).Count(&total).Error; err != nil {
		return nil, err
	}
	out["total"] = total
	for key, status := range statuses {
		var count int64
		if err := model.DB.Model(&model.Redemption{}).Where("status = ?", status).Count(&count).Error; err != nil {
			return nil, err
		}
		out[key] = count
	}
	return out, nil
}

func ListUsers(page int, pageSize int, status int, group string) (PageResult[UserSummary], error) {
	page = clampPage(page)
	pageSize = clampLimit(pageSize)
	query := model.DB.Model(&model.User{}).Omit("password")
	if status > 0 {
		query = query.Where("status = ?", status)
	}
	if group != "" {
		query = query.Where("`group` = ?", group)
		if common.UsingPostgreSQL {
			query = model.DB.Model(&model.User{}).Omit("password").Where(`"group" = ?`, group)
			if status > 0 {
				query = query.Where("status = ?", status)
			}
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return PageResult[UserSummary]{}, err
	}
	var users []model.User
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset(page, pageSize)).Find(&users).Error; err != nil {
		return PageResult[UserSummary]{}, err
	}
	items := make([]UserSummary, 0, len(users))
	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		items = append(items, userToSummary(user))
		userIDs = append(userIDs, user.Id)
	}
	if len(userIDs) > 0 {
		type todayUsage struct {
			UserId            int   `gorm:"column:user_id"`
			TodayRequestCount int64 `gorm:"column:today_request_count"`
			TodayUsedTokens   int64 `gorm:"column:today_used_tokens"`
		}
		var rows []todayUsage
		now := time.Now()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
		if err := model.LOG_DB.Model(&model.Log{}).
			Select("user_id, COUNT(*) AS today_request_count, COALESCE(SUM(prompt_tokens + completion_tokens), 0) AS today_used_tokens").
			Where("type = ? AND user_id IN ? AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, userIDs, todayStart, common.GetTimestamp()).
			Group("user_id").
			Scan(&rows).Error; err != nil {
			return PageResult[UserSummary]{}, err
		}
		usageByUser := make(map[int]todayUsage, len(rows))
		for _, row := range rows {
			usageByUser[row.UserId] = row
		}
		for i := range items {
			if usage, ok := usageByUser[items[i].Id]; ok {
				items[i].TodayRequestCount = usage.TodayRequestCount
				items[i].TodayUsedTokens = usage.TodayUsedTokens
			}
		}
	}
	return PageResult[UserSummary]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func UserActivityStats(start int64, end int64) (map[string]interface{}, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	var activeUsers int64
	if err := model.LOG_DB.Model(&model.Log{}).
		Where("type = ? AND user_id > 0 AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, start, end).
		Distinct("user_id").
		Count(&activeUsers).Error; err != nil {
		return nil, err
	}
	var totalUsers, disabledUsers int64
	if err := model.DB.Model(&model.User{}).Count(&totalUsers).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.User{}).Where("status = ?", common.UserStatusDisabled).Count(&disabledUsers).Error; err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total_users":    totalUsers,
		"active_users":   activeUsers,
		"disabled_users": disabledUsers,
	}, nil
}

func SoftDeletedUserCount() (int64, error) {
	var count int64
	err := model.DB.Unscoped().Model(&model.User{}).Where("deleted_at IS NOT NULL").Count(&count).Error
	return count, err
}

func InvitedUsers(userId int, page int, pageSize int) (PageResult[UserSummary], error) {
	page = clampPage(page)
	pageSize = clampLimit(pageSize)
	query := model.DB.Model(&model.User{}).Omit("password").Where("inviter_id = ?", userId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return PageResult[UserSummary]{}, err
	}
	var users []model.User
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset(page, pageSize)).Find(&users).Error; err != nil {
		return PageResult[UserSummary]{}, err
	}
	items := make([]UserSummary, 0, len(users))
	for _, user := range users {
		items = append(items, userToSummary(user))
	}
	return PageResult[UserSummary]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func ListTokens(page int, pageSize int, status int, group string) (PageResult[TokenSummary], error) {
	page = clampPage(page)
	pageSize = clampLimit(pageSize)
	query := model.DB.Model(&model.Token{})
	if status > 0 {
		query = query.Where("status = ?", status)
	}
	if group != "" {
		query = query.Where("`group` = ?", group)
		if common.UsingPostgreSQL {
			query = model.DB.Model(&model.Token{}).Where(`"group" = ?`, group)
			if status > 0 {
				query = query.Where("status = ?", status)
			}
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return PageResult[TokenSummary]{}, err
	}
	var tokens []model.Token
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset(page, pageSize)).Find(&tokens).Error; err != nil {
		return PageResult[TokenSummary]{}, err
	}
	items := make([]TokenSummary, 0, len(tokens))
	for _, token := range tokens {
		items = append(items, tokenToSummary(token))
	}
	return PageResult[TokenSummary]{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func TokenStats() (map[string]interface{}, error) {
	out := map[string]interface{}{}
	var total, enabled, disabled int64
	if err := model.DB.Model(&model.Token{}).Count(&total).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.Token{}).Where("status = ?", common.TokenStatusEnabled).Count(&enabled).Error; err != nil {
		return nil, err
	}
	if err := model.DB.Model(&model.Token{}).Where("status <> ?", common.TokenStatusEnabled).Count(&disabled).Error; err != nil {
		return nil, err
	}
	var groups []struct {
		GroupName string `gorm:"column:group_name"`
		Count     int64  `gorm:"column:count"`
	}
	groupCol := "`group`"
	if common.UsingPostgreSQL {
		groupCol = `"group"`
	}
	if err := model.DB.Model(&model.Token{}).
		Select(groupCol + " AS group_name, COUNT(*) AS count").
		Group("group_name").
		Scan(&groups).Error; err != nil {
		return nil, err
	}
	groupMap := map[string]int64{}
	for _, item := range groups {
		key := item.GroupName
		if key == "" {
			key = "default"
		}
		groupMap[key] = item.Count
	}
	out["total"] = total
	out["enabled"] = enabled
	out["disabled"] = disabled
	out["groups"] = groupMap
	return out, nil
}

func TokenGroups() (map[string]int64, error) {
	stats, err := TokenStats()
	if err != nil {
		return nil, err
	}
	if groups, ok := stats["groups"].(map[string]int64); ok {
		return groups, nil
	}
	return map[string]int64{}, nil
}

func RiskLeaderboards(start int64, end int64, limit int) ([]UserUsage, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	limit = clampLimit(limit)
	var users []UserUsage
	err := model.LOG_DB.Model(&model.Log{}).
		Select("user_id, username, COUNT(*) AS requests, COALESCE(SUM(quota), 0) AS quota, COUNT(DISTINCT ip) AS distinct_ips").
		Where("user_id > 0 AND created_at >= ? AND created_at <= ?", start, end).
		Group("user_id, username").
		Order("distinct_ips DESC, requests DESC").
		Limit(limit).
		Scan(&users).Error
	if err != nil {
		return nil, err
	}
	for i := range users {
		score := int(users[i].DistinctIPs*10 + users[i].Requests/100)
		if score > 100 {
			score = 100
		}
		users[i].RiskScore = score
	}
	return users, nil
}

func UserRiskAnalysis(userId int, start int64, end int64) (map[string]interface{}, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	var user model.User
	if err := model.DB.Omit("password").Where("id = ?", userId).First(&user).Error; err != nil {
		return nil, err
	}
	var requests int64
	if err := model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND created_at >= ? AND created_at <= ?", userId, start, end).Count(&requests).Error; err != nil {
		return nil, err
	}
	var distinctIPs int64
	if err := model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND ip <> '' AND created_at >= ? AND created_at <= ?", userId, start, end).Distinct("ip").Count(&distinctIPs).Error; err != nil {
		return nil, err
	}
	var errorsCount int64
	if err := model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ? AND created_at >= ? AND created_at <= ?", userId, model.LogTypeError, start, end).Count(&errorsCount).Error; err != nil {
		return nil, err
	}
	score := int(distinctIPs*10 + errorsCount*5 + requests/100)
	if score > 100 {
		score = 100
	}
	return map[string]interface{}{
		"user":         userToSummary(user),
		"requests":     requests,
		"distinct_ips": distinctIPs,
		"errors":       errorsCount,
		"risk_score":   score,
		"window_start": start,
		"window_end":   end,
	}, nil
}

func AvailableModels(public bool) ([]string, error) {
	modelsSet := map[string]struct{}{}
	var channels []model.Channel
	if err := model.DB.Model(&model.Channel{}).Select("models").Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, channel := range channels {
		for _, name := range channel.GetModels() {
			name = strings.TrimSpace(name)
			if name != "" {
				modelsSet[name] = struct{}{}
			}
		}
	}
	models := make([]string, 0, len(modelsSet))
	for name := range modelsSet {
		models = append(models, name)
	}
	sort.Strings(models)
	if public {
		return filterPublicModels(models), nil
	}
	return models, nil
}

func ModelStatusFor(modelName string, minutes int, public bool) (ModelStatus, error) {
	if public {
		if err := requirePublicEmbedEnabled(); err != nil {
			return ModelStatus{}, err
		}
		if _, ok := selectedModelSet()[modelName]; !ok {
			return ModelStatus{}, gorm.ErrRecordNotFound
		}
	}
	minutes = timeWindowMinutes(minutes, public)
	start := common.GetTimestamp() - int64(minutes*60)
	end := common.GetTimestamp()
	status := ModelStatus{
		ModelName:         modelName,
		Status:            "unknown",
		TimeWindowMinutes: minutes,
	}
	if modelName == "" {
		return status, nil
	}
	if err := model.LOG_DB.Model(&model.Log{}).
		Select("COUNT(*) AS requests, COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, COALESCE(SUM(completion_tokens), 0) AS completion_tokens, COALESCE(AVG(use_time), 0) AS avg_use_time, COALESCE(MAX(created_at), 0) AS last_request_at").
		Where("type = ? AND model_name = ? AND created_at >= ? AND created_at <= ?", model.LogTypeConsume, modelName, start, end).
		Scan(&status).Error; err != nil {
		return status, err
	}
	if err := model.LOG_DB.Model(&model.Log{}).
		Where("type = ? AND model_name = ? AND created_at >= ? AND created_at <= ?", model.LogTypeError, modelName, start, end).
		Count(&status.ErrorCount).Error; err != nil {
		return status, err
	}
	total := status.Requests + status.ErrorCount
	if total > 0 {
		status.ErrorRate = float64(status.ErrorCount) / float64(total)
	}
	switch {
	case total == 0:
		status.Status = "unknown"
	case status.ErrorRate > 0.2:
		status.Status = "outage"
	case status.ErrorRate > 0.05:
		status.Status = "degraded"
	default:
		status.Status = "healthy"
	}
	return status, nil
}

func ModelStatuses(modelNames []string, minutes int, public bool) ([]ModelStatus, error) {
	if public {
		if err := requirePublicEmbedEnabled(); err != nil {
			return nil, err
		}
	}
	if len(modelNames) == 0 {
		models, err := AvailableModels(public)
		if err != nil {
			return nil, err
		}
		modelNames = models
	}
	modelNames, err := validateModelList(modelNames, public)
	if err != nil {
		return nil, err
	}
	out := make([]ModelStatus, 0, len(modelNames))
	for _, name := range modelNames {
		status, err := ModelStatusFor(name, minutes, public)
		if err != nil {
			if public && err == gorm.ErrRecordNotFound {
				continue
			}
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}

func ModelStatusConfig(public bool) map[string]interface{} {
	cfg := setting.GetEnhancementSetting()
	selected := append([]string{}, cfg.SelectedModels...)
	if public {
		return map[string]interface{}{
			"site_title":       cfg.ModelStatusSiteTitle,
			"theme":            cfg.ModelStatusTheme,
			"refresh_interval": cfg.ModelStatusRefreshSeconds,
			"sort_mode":        cfg.ModelStatusSortMode,
			"selected_models":  selected,
			"public":           true,
		}
	}
	return map[string]interface{}{
		"site_title":           cfg.ModelStatusSiteTitle,
		"theme":                cfg.ModelStatusTheme,
		"refresh_interval":     cfg.ModelStatusRefreshSeconds,
		"sort_mode":            cfg.ModelStatusSortMode,
		"selected_models":      selected,
		"time_window_minutes":  cfg.ModelStatusTimeWindowMins,
		"public_embed_enabled": cfg.PublicEmbedEnabled,
	}
}

func SystemInfo() map[string]interface{} {
	dbStatus := "ok"
	if err := model.PingDB(); err != nil {
		dbStatus = "error"
	}
	return map[string]interface{}{
		"database": map[string]interface{}{
			"status":       dbStatus,
			"using_mysql":  common.UsingMySQL,
			"using_pg":     common.UsingPostgreSQL,
			"using_sqlite": common.UsingSQLite,
			"log_db_split": model.LOG_DB != model.DB,
		},
		"cache": map[string]interface{}{
			"redis_enabled":        common.RedisEnabled,
			"memory_cache_enabled": common.MemoryCacheEnabled,
		},
		"runtime": map[string]interface{}{
			"generated_at": common.GetTimestamp(),
		},
	}
}

func LinuxDOLookup(id string) (map[string]interface{}, error) {
	var user model.User
	err := model.DB.Omit("password").Where("linux_do_id = ?", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return map[string]interface{}{
				"id":       id,
				"username": "",
				"bound":    false,
				"cached":   false,
			}, nil
		}
		return nil, err
	}
	return map[string]interface{}{
		"id":       id,
		"username": user.Username,
		"user_id":  user.Id,
		"bound":    true,
		"cached":   false,
	}, nil
}
