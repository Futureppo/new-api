package enhancement

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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

func redemptionMatchesQuery(item RedemptionSummary, query ListQuery) bool {
	if !matchesKeyword(query.Keyword,
		strconv.Itoa(item.Id),
		strconv.Itoa(item.UserId),
		item.Key,
		item.Name,
		strconv.Itoa(item.Status),
		strconv.Itoa(item.UsedUserId),
		item.UsedUsername,
		strconv.Itoa(item.Quota),
		strconv.FormatInt(item.CreatedTime, 10),
		strconv.FormatInt(item.RedeemedTime, 10),
		strconv.FormatInt(item.ExpiredTime, 10),
	) {
		return false
	}
	return matchesFilters(query.Filters, map[string]func(string) bool{
		"id":            matchInt(int64(item.Id)),
		"user_id":       matchInt(int64(item.UserId)),
		"key":           matchText(item.Key),
		"status":        matchInt(int64(item.Status)),
		"name":          matchText(item.Name),
		"quota":         matchInt(int64(item.Quota)),
		"created_time":  matchInt(item.CreatedTime),
		"redeemed_time": matchInt(item.RedeemedTime),
		"used_user_id":  matchInt(int64(item.UsedUserId)),
		"used_username": matchText(item.UsedUsername),
		"expired_time":  matchInt(item.ExpiredTime),
	})
}

func sortRedemptionSummaries(items []RedemptionSummary, sortKey string, order string) {
	desc := sortDesc(order)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		result := 0
		switch sortKey {
		case "user_id":
			result = compareInt(int64(left.UserId), int64(right.UserId), desc)
		case "key":
			result = compareString(left.Key, right.Key, desc)
		case "status":
			result = compareInt(int64(left.Status), int64(right.Status), desc)
		case "name":
			result = compareString(left.Name, right.Name, desc)
		case "quota":
			result = compareInt(int64(left.Quota), int64(right.Quota), desc)
		case "created_time":
			result = compareInt(left.CreatedTime, right.CreatedTime, desc)
		case "redeemed_time":
			result = compareInt(left.RedeemedTime, right.RedeemedTime, desc)
		case "used_user_id":
			result = compareInt(int64(left.UsedUserId), int64(right.UsedUserId), desc)
		case "used_username":
			result = compareString(left.UsedUsername, right.UsedUsername, desc)
		case "expired_time":
			result = compareInt(left.ExpiredTime, right.ExpiredTime, desc)
		case "id", "":
			result = compareInt(int64(left.Id), int64(right.Id), desc)
		}
		if result != 0 {
			return result < 0
		}
		return left.Id > right.Id
	})
}

func ListRedemptions(query ListQuery) (PageResult[RedemptionSummary], error) {
	query = normalizeListQuery(query)
	var redemptions []model.Redemption
	if err := model.DB.Model(&model.Redemption{}).Order("id DESC").Find(&redemptions).Error; err != nil {
		return PageResult[RedemptionSummary]{}, err
	}
	usernames, err := redemptionUsedUsernameMap(redemptions)
	if err != nil {
		return PageResult[RedemptionSummary]{}, err
	}
	items := make([]RedemptionSummary, 0, len(redemptions))
	for _, redemption := range redemptions {
		item := redemptionToSummaryWithUsername(redemption, true, usernames[redemption.UsedUserId])
		if redemptionMatchesQuery(item, query) {
			items = append(items, item)
		}
	}
	sortRedemptionSummaries(items, query.Sort, query.Order)
	return pageResult(items, query.Page, query.PageSize), nil
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

func attachTodayUsage(items []UserSummary) error {
	userIDs := make([]int, 0, len(items))
	for _, item := range items {
		userIDs = append(userIDs, item.Id)
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
			return err
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
	return nil
}

func attachUserRedemptions(items []UserSummary) error {
	userIDs := make([]int, 0, len(items))
	indexByUserID := make(map[int]int, len(items))
	for index, item := range items {
		userIDs = append(userIDs, item.Id)
		indexByUserID[item.Id] = index
	}
	if len(userIDs) == 0 {
		return nil
	}

	var redemptions []model.Redemption
	if err := model.DB.Model(&model.Redemption{}).
		Where("used_user_id IN ?", userIDs).
		Order("redeemed_time DESC, id DESC").
		Find(&redemptions).Error; err != nil {
		return err
	}

	codesByUserID := make(map[int][]string, len(items))
	for _, redemption := range redemptions {
		if redemption.UsedUserId <= 0 {
			continue
		}
		codesByUserID[redemption.UsedUserId] = append(
			codesByUserID[redemption.UsedUserId],
			redemption.Key,
		)
	}
	for userID, codes := range codesByUserID {
		index, ok := indexByUserID[userID]
		if !ok {
			continue
		}
		items[index].RedemptionCount = len(codes)
		items[index].RedemptionCodes = strings.Join(codes, ", ")
	}
	return nil
}

func userMatchesQuery(item UserSummary, query ListQuery) bool {
	if !matchesKeyword(query.Keyword,
		strconv.Itoa(item.Id),
		item.Username,
		item.DisplayName,
		strconv.Itoa(item.Role),
		strconv.Itoa(item.Status),
		item.DisableReason,
		item.Email,
		item.GitHubId,
		strconv.Itoa(item.Quota),
		strconv.Itoa(item.UsedQuota),
		strconv.Itoa(item.RequestCount),
		strconv.FormatInt(item.TodayRequestCount, 10),
		strconv.FormatInt(item.TodayUsedTokens, 10),
		item.Group,
		strconv.Itoa(item.InviterId),
		item.AffCode,
		strconv.Itoa(item.AffCount),
		strconv.Itoa(item.RedemptionCount),
		item.RedemptionCodes,
		item.LinuxDOId,
	) {
		return false
	}
	return matchesFilters(query.Filters, map[string]func(string) bool{
		"id":                  matchInt(int64(item.Id)),
		"username":            matchText(item.Username),
		"display_name":        matchText(item.DisplayName),
		"role":                matchInt(int64(item.Role)),
		"status":              matchInt(int64(item.Status)),
		"disable_reason":      matchText(item.DisableReason),
		"email":               matchText(item.Email),
		"github_id":           matchText(item.GitHubId),
		"quota":               matchInt(int64(item.Quota)),
		"used_quota":          matchInt(int64(item.UsedQuota)),
		"request_count":       matchInt(int64(item.RequestCount)),
		"today_request_count": matchInt(item.TodayRequestCount),
		"today_used_tokens":   matchInt(item.TodayUsedTokens),
		"group":               matchText(item.Group),
		"inviter_id":          matchInt(int64(item.InviterId)),
		"aff_code":            matchText(item.AffCode),
		"aff_count":           matchInt(int64(item.AffCount)),
		"redemption_count":    matchInt(int64(item.RedemptionCount)),
		"redemption_codes":    matchText(item.RedemptionCodes),
		"linux_do_id":         matchText(item.LinuxDOId),
	})
}

func sortUserSummaries(items []UserSummary, sortKey string, order string) {
	desc := sortDesc(order)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		result := 0
		switch sortKey {
		case "username":
			result = compareString(left.Username, right.Username, desc)
		case "display_name":
			result = compareString(left.DisplayName, right.DisplayName, desc)
		case "role":
			result = compareInt(int64(left.Role), int64(right.Role), desc)
		case "status":
			result = compareInt(int64(left.Status), int64(right.Status), desc)
		case "disable_reason":
			result = compareString(left.DisableReason, right.DisableReason, desc)
		case "email":
			result = compareString(left.Email, right.Email, desc)
		case "github_id":
			result = compareString(left.GitHubId, right.GitHubId, desc)
		case "quota":
			result = compareInt(int64(left.Quota), int64(right.Quota), desc)
		case "used_quota":
			result = compareInt(int64(left.UsedQuota), int64(right.UsedQuota), desc)
		case "request_count":
			result = compareInt(int64(left.RequestCount), int64(right.RequestCount), desc)
		case "today_request_count":
			result = compareInt(left.TodayRequestCount, right.TodayRequestCount, desc)
		case "today_used_tokens":
			result = compareInt(left.TodayUsedTokens, right.TodayUsedTokens, desc)
		case "group":
			result = compareString(left.Group, right.Group, desc)
		case "inviter_id":
			result = compareInt(int64(left.InviterId), int64(right.InviterId), desc)
		case "aff_code":
			result = compareString(left.AffCode, right.AffCode, desc)
		case "aff_count":
			result = compareInt(int64(left.AffCount), int64(right.AffCount), desc)
		case "redemption_count":
			result = compareInt(int64(left.RedemptionCount), int64(right.RedemptionCount), desc)
		case "redemption_codes":
			result = compareString(left.RedemptionCodes, right.RedemptionCodes, desc)
		case "linux_do_id":
			result = compareString(left.LinuxDOId, right.LinuxDOId, desc)
		case "id", "":
			result = compareInt(int64(left.Id), int64(right.Id), desc)
		}
		if result != 0 {
			return result < 0
		}
		return left.Id > right.Id
	})
}

func ListUsers(query ListQuery) (PageResult[UserSummary], error) {
	query = normalizeListQuery(query)
	var users []model.User
	if err := model.DB.Model(&model.User{}).Omit("password").Order("id DESC").Find(&users).Error; err != nil {
		return PageResult[UserSummary]{}, err
	}
	items := make([]UserSummary, 0, len(users))
	for _, user := range users {
		items = append(items, userToSummary(user))
	}
	if err := attachTodayUsage(items); err != nil {
		return PageResult[UserSummary]{}, err
	}
	if err := attachUserRedemptions(items); err != nil {
		return PageResult[UserSummary]{}, err
	}
	filtered := make([]UserSummary, 0, len(items))
	for _, item := range items {
		if userMatchesQuery(item, query) {
			filtered = append(filtered, item)
		}
	}
	sortUserSummaries(filtered, query.Sort, query.Order)
	return pageResult(filtered, query.Page, query.PageSize), nil
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
	var inviteCount int64
	if err := model.DB.Model(&model.User{}).Select("COALESCE(SUM(aff_count), 0)").Scan(&inviteCount).Error; err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total_users":    totalUsers,
		"active_users":   activeUsers,
		"disabled_users": disabledUsers,
		"invite_count":   inviteCount,
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

func tokenKeyColumn() string {
	if common.UsingPostgreSQL {
		return `"key"`
	}
	return "`key`"
}

func tokenMatchesQuery(item TokenSummary, query ListQuery) bool {
	if !matchesKeyword(query.Keyword,
		strconv.Itoa(item.Id),
		strconv.Itoa(item.UserId),
		item.Name,
		item.Key,
		strconv.Itoa(item.Status),
		item.Group,
		strconv.FormatInt(item.CreatedTime, 10),
		strconv.FormatInt(item.AccessedTime, 10),
		strconv.FormatInt(item.ExpiredTime, 10),
		strconv.Itoa(item.RemainQuota),
		strconv.Itoa(item.UsedQuota),
		strconv.FormatBool(item.UnlimitedQuota),
		strconv.FormatBool(item.ModelLimitsEnabled),
		item.ModelLimits,
		item.AllowIps,
	) {
		return false
	}
	return matchesFilters(query.Filters, map[string]func(string) bool{
		"id":                   matchInt(int64(item.Id)),
		"user_id":              matchInt(int64(item.UserId)),
		"name":                 matchText(item.Name),
		"key":                  matchText(item.Key),
		"status":               matchInt(int64(item.Status)),
		"group":                matchText(item.Group),
		"created_time":         matchInt(item.CreatedTime),
		"accessed_time":        matchInt(item.AccessedTime),
		"expired_time":         matchInt(item.ExpiredTime),
		"remain_quota":         matchInt(int64(item.RemainQuota)),
		"used_quota":           matchInt(int64(item.UsedQuota)),
		"unlimited_quota":      matchBool(item.UnlimitedQuota),
		"model_limits_enabled": matchBool(item.ModelLimitsEnabled),
		"model_limits":         matchText(item.ModelLimits),
		"allow_ips":            matchText(item.AllowIps),
	})
}

func sortTokenSummaries(items []TokenSummary, sortKey string, order string) {
	desc := sortDesc(order)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		result := 0
		switch sortKey {
		case "user_id":
			result = compareInt(int64(left.UserId), int64(right.UserId), desc)
		case "name":
			result = compareString(left.Name, right.Name, desc)
		case "key":
			result = compareString(left.Key, right.Key, desc)
		case "status":
			result = compareInt(int64(left.Status), int64(right.Status), desc)
		case "group":
			result = compareString(left.Group, right.Group, desc)
		case "created_time":
			result = compareInt(left.CreatedTime, right.CreatedTime, desc)
		case "accessed_time":
			result = compareInt(left.AccessedTime, right.AccessedTime, desc)
		case "expired_time":
			result = compareInt(left.ExpiredTime, right.ExpiredTime, desc)
		case "remain_quota":
			result = compareInt(int64(left.RemainQuota), int64(right.RemainQuota), desc)
		case "used_quota":
			result = compareInt(int64(left.UsedQuota), int64(right.UsedQuota), desc)
		case "unlimited_quota":
			result = compareString(strconv.FormatBool(left.UnlimitedQuota), strconv.FormatBool(right.UnlimitedQuota), desc)
		case "model_limits_enabled":
			result = compareString(strconv.FormatBool(left.ModelLimitsEnabled), strconv.FormatBool(right.ModelLimitsEnabled), desc)
		case "model_limits":
			result = compareString(left.ModelLimits, right.ModelLimits, desc)
		case "allow_ips":
			result = compareString(left.AllowIps, right.AllowIps, desc)
		case "id", "":
			result = compareInt(int64(left.Id), int64(right.Id), desc)
		}
		if result != 0 {
			return result < 0
		}
		return left.Id > right.Id
	})
}

func ListTokens(query ListQuery) (PageResult[TokenSummary], error) {
	query = normalizeListQuery(query)
	var tokens []model.Token
	if err := model.DB.Model(&model.Token{}).Order("id DESC").Find(&tokens).Error; err != nil {
		return PageResult[TokenSummary]{}, err
	}
	items := make([]TokenSummary, 0, len(tokens))
	for _, token := range tokens {
		item := tokenToSummary(token)
		if tokenMatchesQuery(item, query) {
			items = append(items, item)
		}
	}
	sortTokenSummaries(items, query.Sort, query.Order)
	return pageResult(items, query.Page, query.PageSize), nil
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
	out["total"] = total
	out["enabled"] = enabled
	out["disabled"] = disabled
	return out, nil
}

func TokenGroups() (map[string]int64, error) {
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
	return groupMap, nil
}

func riskLeaderboards(start int64, end int64, limit int) ([]UserUsage, error) {
	start, end = queryWindow(start, end, MaxAdminQueryWindow)
	query := model.LOG_DB.Model(&model.Log{}).
		Select("user_id, username, COUNT(*) AS requests, COALESCE(SUM(quota), 0) AS quota, COUNT(DISTINCT ip) AS distinct_ips").
		Where("user_id > 0 AND created_at >= ? AND created_at <= ?", start, end).
		Group("user_id, username").
		Order("distinct_ips DESC, requests DESC")
	if limit > 0 {
		query = query.Limit(clampLimit(limit))
	}
	var users []UserUsage
	err := query.Scan(&users).Error
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

func RiskLeaderboards(start int64, end int64, limit int) ([]UserUsage, error) {
	return riskLeaderboards(start, end, limit)
}

func userUsageMatchesQuery(item UserUsage, query ListQuery) bool {
	if !matchesKeyword(query.Keyword,
		strconv.Itoa(item.UserId),
		item.Username,
		item.Group,
		strconv.FormatInt(item.Requests, 10),
		strconv.FormatInt(item.Quota, 10),
		strconv.FormatInt(item.DistinctIPs, 10),
		strconv.Itoa(item.RiskScore),
		strconv.Itoa(item.Status),
		strconv.FormatInt(item.LastActivity, 10),
	) {
		return false
	}
	return matchesFilters(query.Filters, map[string]func(string) bool{
		"user_id":       matchInt(int64(item.UserId)),
		"username":      matchText(item.Username),
		"group":         matchText(item.Group),
		"requests":      matchInt(item.Requests),
		"quota":         matchInt(item.Quota),
		"distinct_ips":  matchInt(item.DistinctIPs),
		"risk_score":    matchInt(int64(item.RiskScore)),
		"status":        matchInt(int64(item.Status)),
		"last_activity": matchInt(item.LastActivity),
	})
}

func sortUserUsage(items []UserUsage, sortKey string, order string) {
	desc := sortDesc(order)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		result := 0
		switch sortKey {
		case "user_id":
			result = compareInt(int64(left.UserId), int64(right.UserId), desc)
		case "username":
			result = compareString(left.Username, right.Username, desc)
		case "group":
			result = compareString(left.Group, right.Group, desc)
		case "requests":
			result = compareInt(left.Requests, right.Requests, desc)
		case "quota":
			result = compareInt(left.Quota, right.Quota, desc)
		case "risk_score":
			result = compareInt(int64(left.RiskScore), int64(right.RiskScore), desc)
		case "status":
			result = compareInt(int64(left.Status), int64(right.Status), desc)
		case "last_activity":
			result = compareInt(left.LastActivity, right.LastActivity, desc)
		case "distinct_ips", "":
			result = compareInt(left.DistinctIPs, right.DistinctIPs, desc)
		}
		if result != 0 {
			return result < 0
		}
		if left.Requests != right.Requests {
			return left.Requests > right.Requests
		}
		return left.UserId < right.UserId
	})
}

func RiskLeaderboardsPage(start int64, end int64, query ListQuery) (PageResult[UserUsage], error) {
	query = normalizeListQuery(query)
	users, err := riskLeaderboards(start, end, 0)
	if err != nil {
		return PageResult[UserUsage]{}, err
	}
	filtered := make([]UserUsage, 0, len(users))
	for _, user := range users {
		if userUsageMatchesQuery(user, query) {
			filtered = append(filtered, user)
		}
	}
	sortUserUsage(filtered, query.Sort, query.Order)
	return pageResult(filtered, query.Page, query.PageSize), nil
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

func IPLogCoverageStats() (IPLogCoverage, error) {
	var users []struct {
		Id      int    `gorm:"column:id"`
		Setting string `gorm:"column:setting"`
	}
	if err := model.DB.Model(&model.User{}).
		Select("id, setting").
		Find(&users).Error; err != nil {
		return IPLogCoverage{}, err
	}

	stats := IPLogCoverage{
		TotalUsers:  int64(len(users)),
		GeneratedAt: common.GetTimestamp(),
	}
	for _, user := range users {
		if isRecordIPLogEnabled(user.Setting) {
			stats.EnabledUsers++
		}
	}
	stats.DisabledUsers = stats.TotalUsers - stats.EnabledUsers
	if stats.TotalUsers > 0 {
		stats.EnabledRatio = float64(stats.EnabledUsers) / float64(stats.TotalUsers)
	}
	return stats, nil
}

const (
	ModelStatusWindowToday = "today"
	ModelStatusWindow24h   = "24h"
	ModelStatusWindow7d    = "7d"
	ModelStatusWindow30d   = "30d"
)

type modelStatusWindow struct {
	Key         string
	Label       string
	Start       int64
	End         int64
	SlotCount   int
	SlotSeconds int64
	Minutes     int
}

type modelStatusTarget struct {
	Group string
	Model string
}

func isPublicModelStatusGroupDisplayed(group string) bool {
	if !ratio_setting.IsGroupDisplayed(group) {
		return false
	}
	_, ok := setting.GetUserUsableGroupsCopy()[group]
	return ok
}

var modelStatusPublicCache = struct {
	sync.Mutex
	key       string
	expiresAt int64
	data      []ModelStatus
}{}

func AvailableModels(public bool) ([]string, error) {
	if public {
		if err := requirePublicEmbedEnabled(); err != nil {
			return nil, err
		}
	}

	targets, err := availableModelStatusTargets(public)
	if err != nil {
		return nil, err
	}

	modelsSet := make(map[string]struct{}, len(targets))
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		name := strings.TrimSpace(target.Model)
		if name == "" {
			continue
		}
		if _, ok := modelsSet[name]; ok {
			continue
		}
		modelsSet[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func availableModelStatusTargets(public bool) ([]modelStatusTarget, error) {
	var abilities []model.Ability
	err := model.DB.Model(&model.Ability{}).
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).
		Find(&abilities).Error
	if err != nil {
		return nil, err
	}

	targetSet := make(map[string]struct{}, len(abilities))
	targets := make([]modelStatusTarget, 0, len(abilities))
	for _, ability := range abilities {
		group := strings.TrimSpace(ability.Group)
		if group == "" {
			group = "default"
		}
		if public && !isPublicModelStatusGroupDisplayed(group) {
			continue
		}
		modelName := strings.TrimSpace(ability.Model)
		if modelName == "" {
			continue
		}
		key := group + "\x00" + modelName
		if _, ok := targetSet[key]; ok {
			continue
		}
		targetSet[key] = struct{}{}
		targets = append(targets, modelStatusTarget{
			Group: group,
			Model: modelName,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Group == targets[j].Group {
			return targets[i].Model < targets[j].Model
		}
		return targets[i].Group < targets[j].Group
	})
	return targets, nil
}

func ModelStatusTimeWindows() []map[string]interface{} {
	return []map[string]interface{}{
		{"label": "今日", "value": ModelStatusWindowToday, "minutes": 0},
		{"label": "24h", "value": ModelStatusWindow24h, "minutes": 24 * 60},
		{"label": "7天", "value": ModelStatusWindow7d, "minutes": 7 * 24 * 60},
		{"label": "30天", "value": ModelStatusWindow30d, "minutes": 30 * 24 * 60},
	}
}

func NormalizeModelStatusWindow(window string) string {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case ModelStatusWindowToday:
		return ModelStatusWindowToday
	case ModelStatusWindow24h, "1d":
		return ModelStatusWindow24h
	case ModelStatusWindow7d:
		return ModelStatusWindow7d
	case ModelStatusWindow30d:
		return ModelStatusWindow30d
	default:
		return ModelStatusWindow24h
	}
}

func ModelStatusWindowFromMinutes(minutes int) string {
	switch {
	case minutes <= 0:
		return ModelStatusWindow24h
	case minutes <= 24*60:
		return ModelStatusWindow24h
	case minutes <= 7*24*60:
		return ModelStatusWindow7d
	default:
		return ModelStatusWindow30d
	}
}

func ModelStatusConfigWindowFromMinutes(minutes int) string {
	if minutes == 0 {
		return ModelStatusWindowToday
	}
	return ModelStatusWindowFromMinutes(minutes)
}

func ModelStatusConfiguredWindow() string {
	return ModelStatusConfigWindowFromMinutes(setting.GetEnhancementSetting().ModelStatusTimeWindowMins)
}

func ModelStatusWindowToMinutes(window string) int {
	switch NormalizeModelStatusWindow(window) {
	case ModelStatusWindowToday:
		return 0
	case ModelStatusWindow7d:
		return 7 * 24 * 60
	case ModelStatusWindow30d:
		return 30 * 24 * 60
	default:
		return 24 * 60
	}
}

func IsAllowedModelStatusWindowMinutes(minutes int) bool {
	switch minutes {
	case 0, 24 * 60, 7 * 24 * 60, 30 * 24 * 60:
		return true
	default:
		return false
	}
}

func ModelStatusSlotMinutes() int {
	minutes := setting.GetEnhancementSetting().ModelStatusSlotMinutes
	if minutes <= 0 {
		minutes = 30
	}
	if minutes < 5 {
		return 5
	}
	if minutes > 24*60 {
		return 24 * 60
	}
	return minutes
}

func ModelStatusThresholds() (float64, float64) {
	cfg := setting.GetEnhancementSetting()
	green := cfg.ModelStatusGreenThreshold
	yellow := cfg.ModelStatusYellowThreshold
	if green <= 0 || green > 100 {
		green = 95
	}
	if yellow <= 0 || yellow > 100 {
		yellow = 80
	}
	if green < yellow {
		green = yellow
	}
	return green, yellow
}

func resolveModelStatusWindow(window string) modelStatusWindow {
	now := time.Now()
	end := now.Unix()
	slotSeconds := int64(ModelStatusSlotMinutes() * 60)
	key := NormalizeModelStatusWindow(window)
	switch key {
	case ModelStatusWindowToday:
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
		seconds := end - startOfDay
		slotCount := int((seconds + slotSeconds - 1) / slotSeconds)
		if slotCount < 1 {
			slotCount = 1
		}
		return modelStatusWindow{
			Key:         key,
			Label:       "今日",
			Start:       startOfDay,
			End:         end,
			SlotCount:   slotCount,
			SlotSeconds: slotSeconds,
			Minutes:     int(math.Max(1, math.Ceil(float64(seconds)/60))),
		}
	case ModelStatusWindow7d:
		start := now.AddDate(0, 0, -7).Unix()
		return modelStatusWindow{
			Key:         key,
			Label:       "7天",
			Start:       start,
			End:         end,
			SlotCount:   int((end - start + slotSeconds - 1) / slotSeconds),
			SlotSeconds: slotSeconds,
			Minutes:     7 * 24 * 60,
		}
	case ModelStatusWindow30d:
		start := now.AddDate(0, 0, -30).Unix()
		return modelStatusWindow{
			Key:         key,
			Label:       "30天",
			Start:       start,
			End:         end,
			SlotCount:   int((end - start + slotSeconds - 1) / slotSeconds),
			SlotSeconds: slotSeconds,
			Minutes:     30 * 24 * 60,
		}
	default:
		start := now.Add(-24 * time.Hour).Unix()
		return modelStatusWindow{
			Key:         ModelStatusWindow24h,
			Label:       "24h",
			Start:       start,
			End:         end,
			SlotCount:   int((end - start + slotSeconds - 1) / slotSeconds),
			SlotSeconds: slotSeconds,
			Minutes:     24 * 60,
		}
	}
}

func modelStatusColor(successRate float64, total int64) string {
	greenThreshold, yellowThreshold := ModelStatusThresholds()
	switch {
	case total == 0 || successRate >= greenThreshold:
		return "green"
	case successRate >= yellowThreshold:
		return "yellow"
	default:
		return "red"
	}
}

func legacyModelStatus(status string) string {
	switch status {
	case "green":
		return "healthy"
	case "yellow":
		return "degraded"
	case "red":
		return "outage"
	default:
		return "unknown"
	}
}

func roundedPercent(value float64) float64 {
	return math.Round(value*100) / 100
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func modelStatusForTarget(target modelStatusTarget, window string, public bool) (ModelStatus, error) {
	groupName := strings.TrimSpace(target.Group)
	if groupName == "" {
		groupName = "default"
	}
	modelName := strings.TrimSpace(target.Model)
	return buildModelStatus(groupName, modelName, window, public)
}

func ModelStatusForWindow(modelName string, window string, public bool) (ModelStatus, error) {
	return buildModelStatus("", modelName, window, public)
}

func ModelStatusForGroupWindow(groupName string, modelName string, window string, public bool) (ModelStatus, error) {
	return buildModelStatus(groupName, modelName, window, public)
}

func buildModelStatus(groupName string, modelName string, window string, public bool) (ModelStatus, error) {
	if public {
		if err := requirePublicEmbedEnabled(); err != nil {
			return ModelStatus{}, err
		}
	}
	groupName = strings.TrimSpace(groupName)
	if groupName == "" {
		groupName = "default"
	}
	if public && !isPublicModelStatusGroupDisplayed(groupName) {
		return ModelStatus{}, gorm.ErrRecordNotFound
	}
	modelName = strings.TrimSpace(modelName)

	resolved := resolveModelStatusWindow(window)
	status := ModelStatus{
		ModelName:         modelName,
		Group:             groupName,
		GroupName:         groupName,
		DisplayName:       modelName,
		TimeWindow:        resolved.Key,
		CurrentStatus:     "green",
		Status:            "healthy",
		TimeWindowMinutes: resolved.Minutes,
		GeneratedAt:       common.GetTimestamp(),
	}
	if modelName == "" {
		return status, nil
	}

	slots := make([]ModelStatusSlot, resolved.SlotCount)
	for i := range slots {
		start := resolved.Start + int64(i)*resolved.SlotSeconds
		end := start + resolved.SlotSeconds
		if i == len(slots)-1 || end > resolved.End {
			end = resolved.End
		}
		slots[i] = ModelStatusSlot{
			Slot:      i,
			StartTime: start,
			EndTime:   end,
			Status:    "green",
		}
	}

	var rows []model.Log
	query := model.LOG_DB.Model(&model.Log{}).
		Where("model_name = ? AND created_at >= ? AND created_at <= ? AND type IN ?", modelName, resolved.Start, resolved.End, []int{model.LogTypeConsume, model.LogTypeError})
	if groupName != "" {
		query = query.Where("`group` = ?", groupName)
		if common.UsingPostgreSQL {
			query = model.LOG_DB.Model(&model.Log{}).
				Where("model_name = ? AND created_at >= ? AND created_at <= ? AND type IN ?", modelName, resolved.Start, resolved.End, []int{model.LogTypeConsume, model.LogTypeError}).
				Where(`"group" = ?`, groupName)
		}
	}
	if err := query.Find(&rows).Error; err != nil {
		return status, err
	}

	ignoredErrorKeywords := modelStatusIgnoredErrorKeywordsForMatch()
	var useTimeTotal int64
	for _, row := range rows {
		if row.CreatedAt < resolved.Start || row.CreatedAt > resolved.End {
			continue
		}
		if row.Type == model.LogTypeError && shouldIgnoreModelStatusError(row, ignoredErrorKeywords) {
			continue
		}
		slotIndex := int((row.CreatedAt - resolved.Start) / resolved.SlotSeconds)
		if slotIndex < 0 {
			continue
		}
		if slotIndex >= len(slots) {
			slotIndex = len(slots) - 1
		}

		slots[slotIndex].TotalRequests++
		status.TotalRequests++
		status.LastRequestAt = maxInt64(status.LastRequestAt, row.CreatedAt)
		switch row.Type {
		case model.LogTypeConsume:
			slots[slotIndex].SuccessCount++
			status.SuccessCount++
			status.Quota += int64(row.Quota)
			status.PromptTokens += int64(row.PromptTokens)
			status.CompletionTokens += int64(row.CompletionTokens)
			useTimeTotal += int64(row.UseTime)
		case model.LogTypeError:
			slots[slotIndex].ErrorCount++
			status.ErrorCount++
		}
	}

	if status.TotalRequests > 0 {
		status.SuccessRate = roundedPercent(float64(status.SuccessCount) / float64(status.TotalRequests) * 100)
		status.ErrorRate = roundedPercent(float64(status.ErrorCount) / float64(status.TotalRequests))
	} else {
		status.SuccessRate = 100
	}
	if status.SuccessCount > 0 {
		status.AvgUseTime = roundedPercent(float64(useTimeTotal) / float64(status.SuccessCount))
	}
	status.CurrentStatus = modelStatusColor(status.SuccessRate, status.TotalRequests)
	status.Status = legacyModelStatus(status.CurrentStatus)
	status.Requests = status.SuccessCount

	for i := range slots {
		if slots[i].TotalRequests > 0 {
			slots[i].SuccessRate = roundedPercent(float64(slots[i].SuccessCount) / float64(slots[i].TotalRequests) * 100)
		} else {
			slots[i].SuccessRate = 100
		}
		slots[i].Status = modelStatusColor(slots[i].SuccessRate, slots[i].TotalRequests)
	}
	status.SlotData = slots

	return status, nil
}

func modelStatusIgnoredErrorKeywordsForMatch() []string {
	cfg := setting.GetEnhancementSetting()
	if !cfg.ModelStatusIgnoreErrorKeywordsEnabled || len(cfg.ModelStatusIgnoredErrorKeywords) == 0 {
		return nil
	}
	keywords, err := normalizeModelStatusIgnoredErrorKeywords(cfg.ModelStatusIgnoredErrorKeywords)
	if err != nil || len(keywords) == 0 {
		return nil
	}
	matchers := make([]string, 0, len(keywords))
	for _, keyword := range keywords {
		matchers = append(matchers, strings.ToLower(keyword))
	}
	return matchers
}

func shouldIgnoreModelStatusError(row model.Log, keywords []string) bool {
	if len(keywords) == 0 {
		return false
	}
	haystack := strings.ToLower(row.Content + "\n" + row.Other)
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(haystack, keyword) {
			return true
		}
	}
	return false
}

func ModelStatusFor(modelName string, minutes int, public bool) (ModelStatus, error) {
	return ModelStatusForWindow(modelName, ModelStatusWindowFromMinutes(minutes), public)
}

func ModelStatusesForWindow(modelNames []string, window string, public bool) ([]ModelStatus, error) {
	if public {
		if err := requirePublicEmbedEnabled(); err != nil {
			return nil, err
		}
	}
	targets, err := availableModelStatusTargets(public)
	if err != nil {
		return nil, err
	}
	if len(modelNames) > 0 {
		modelNames, err = validateModelList(modelNames, public)
		if err != nil {
			return nil, err
		}
		modelSet := make(map[string]struct{}, len(modelNames))
		for _, name := range modelNames {
			modelSet[name] = struct{}{}
		}
		filtered := make([]modelStatusTarget, 0, len(targets))
		for _, target := range targets {
			if _, ok := modelSet[target.Model]; ok {
				filtered = append(filtered, target)
			}
		}
		targets = filtered
	}

	out := make([]ModelStatus, 0, len(targets))
	for _, target := range targets {
		status, err := modelStatusForTarget(target, window, public)
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

func modelStatusMatchesQuery(item ModelStatus, query ListQuery) bool {
	if !matchesKeyword(query.Keyword,
		item.ModelName,
		item.Group,
		item.GroupName,
		item.DisplayName,
		item.TimeWindow,
		strconv.FormatInt(item.TotalRequests, 10),
		strconv.FormatInt(item.SuccessCount, 10),
		strconv.FormatInt(item.ErrorCount, 10),
		strconv.FormatFloat(item.SuccessRate, 'f', -1, 64),
		item.CurrentStatus,
		strconv.FormatInt(item.Quota, 10),
		strconv.FormatFloat(item.AvgUseTime, 'f', -1, 64),
		strconv.FormatInt(item.PromptTokens, 10),
		strconv.FormatInt(item.CompletionTokens, 10),
		strconv.FormatInt(item.LastRequestAt, 10),
	) {
		return false
	}
	return matchesFilters(query.Filters, map[string]func(string) bool{
		"model_name":          matchText(item.ModelName),
		"group":               matchText(item.Group),
		"group_name":          matchText(item.GroupName),
		"display_name":        matchText(item.DisplayName),
		"time_window":         matchText(item.TimeWindow),
		"total_requests":      matchInt(item.TotalRequests),
		"success_count":       matchInt(item.SuccessCount),
		"error_count":         matchInt(item.ErrorCount),
		"success_rate":        matchFloat(item.SuccessRate),
		"current_status":      matchText(item.CurrentStatus),
		"status":              matchText(item.Status),
		"requests":            matchInt(item.Requests),
		"error_rate":          matchFloat(item.ErrorRate),
		"quota":               matchInt(item.Quota),
		"avg_use_time":        matchFloat(item.AvgUseTime),
		"prompt_tokens":       matchInt(item.PromptTokens),
		"completion_tokens":   matchInt(item.CompletionTokens),
		"last_request_at":     matchInt(item.LastRequestAt),
		"time_window_minutes": matchInt(int64(item.TimeWindowMinutes)),
	})
}

func sortModelStatuses(items []ModelStatus, sortKey string, order string) {
	desc := sortDesc(order)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		result := 0
		switch sortKey {
		case "group":
			result = compareString(left.Group, right.Group, desc)
		case "group_name":
			result = compareString(left.GroupName, right.GroupName, desc)
		case "display_name":
			result = compareString(left.DisplayName, right.DisplayName, desc)
		case "time_window":
			result = compareString(left.TimeWindow, right.TimeWindow, desc)
		case "total_requests", "requests":
			result = compareInt(left.TotalRequests, right.TotalRequests, desc)
		case "success_count":
			result = compareInt(left.SuccessCount, right.SuccessCount, desc)
		case "error_count":
			result = compareInt(left.ErrorCount, right.ErrorCount, desc)
		case "success_rate":
			result = compareFloat(left.SuccessRate, right.SuccessRate, desc)
		case "current_status", "status":
			result = compareString(left.CurrentStatus, right.CurrentStatus, desc)
		case "quota":
			result = compareInt(left.Quota, right.Quota, desc)
		case "avg_use_time":
			result = compareFloat(left.AvgUseTime, right.AvgUseTime, desc)
		case "prompt_tokens":
			result = compareInt(left.PromptTokens, right.PromptTokens, desc)
		case "completion_tokens":
			result = compareInt(left.CompletionTokens, right.CompletionTokens, desc)
		case "last_request_at":
			result = compareInt(left.LastRequestAt, right.LastRequestAt, desc)
		case "time_window_minutes":
			result = compareInt(int64(left.TimeWindowMinutes), int64(right.TimeWindowMinutes), desc)
		case "model_name", "":
			result = compareString(left.ModelName, right.ModelName, desc)
		}
		if result != 0 {
			return result < 0
		}
		if left.Group != right.Group {
			return left.Group < right.Group
		}
		return left.ModelName < right.ModelName
	})
}

func ModelStatusesPageForWindow(window string, query ListQuery, public bool) (PageResult[ModelStatus], error) {
	query = normalizeListQuery(query)
	statuses, err := ModelStatusesForWindow(nil, window, public)
	if err != nil {
		return PageResult[ModelStatus]{}, err
	}
	filtered := make([]ModelStatus, 0, len(statuses))
	for _, status := range statuses {
		if modelStatusMatchesQuery(status, query) {
			filtered = append(filtered, status)
		}
	}
	sortModelStatuses(filtered, query.Sort, query.Order)
	return pageResult(filtered, query.Page, query.PageSize), nil
}

func ModelStatuses(modelNames []string, minutes int, public bool) ([]ModelStatus, error) {
	return ModelStatusesForWindow(modelNames, ModelStatusWindowFromMinutes(minutes), public)
}

func publicModelStatusCacheTTL() int64 {
	seconds := setting.GetEnhancementSetting().ModelStatusRefreshSeconds
	if seconds < 60 {
		seconds = 60
	}
	if seconds > 24*60*60 {
		seconds = 24 * 60 * 60
	}
	return int64(seconds)
}

func ClearModelStatusPublicCache() {
	modelStatusPublicCache.Lock()
	defer modelStatusPublicCache.Unlock()
	modelStatusPublicCache.key = ""
	modelStatusPublicCache.expiresAt = 0
	modelStatusPublicCache.data = nil
}

func modelStatusHasRequests(status ModelStatus) bool {
	if status.TotalRequests > 0 {
		return true
	}
	for _, slot := range status.SlotData {
		if slot.TotalRequests > 0 {
			return true
		}
	}
	return false
}

func filterZeroRequestModelStatuses(statuses []ModelStatus) []ModelStatus {
	filtered := make([]ModelStatus, 0, len(statuses))
	for _, status := range statuses {
		if modelStatusHasRequests(status) {
			filtered = append(filtered, status)
		}
	}
	return filtered
}

func ModelStatusesForPublicConfig() ([]ModelStatus, error) {
	if err := requirePublicEmbedEnabled(); err != nil {
		return nil, err
	}
	window := ModelStatusConfiguredWindow()
	greenThreshold, yellowThreshold := ModelStatusThresholds()
	showZeroRequests := setting.GetEnhancementSetting().ModelStatusShowZeroRequests
	key := "public:" + window + ":" +
		strconv.Itoa(ModelStatusSlotMinutes()) + ":" +
		strconv.FormatFloat(greenThreshold, 'f', -1, 64) + ":" +
		strconv.FormatFloat(yellowThreshold, 'f', -1, 64) + ":" +
		strconv.FormatBool(showZeroRequests) + ":" +
		ratio_setting.GroupDisplay2JSONString() + ":" +
		setting.UserUsableGroups2JSONString()
	now := common.GetTimestamp()

	modelStatusPublicCache.Lock()
	if modelStatusPublicCache.key == key && modelStatusPublicCache.expiresAt > now {
		cached := append([]ModelStatus(nil), modelStatusPublicCache.data...)
		modelStatusPublicCache.Unlock()
		return cached, nil
	}
	modelStatusPublicCache.Unlock()

	statuses, err := ModelStatusesForWindow(nil, window, true)
	if err != nil {
		return nil, err
	}
	if !showZeroRequests {
		statuses = filterZeroRequestModelStatuses(statuses)
	}

	modelStatusPublicCache.Lock()
	modelStatusPublicCache.key = key
	modelStatusPublicCache.expiresAt = now + publicModelStatusCacheTTL()
	modelStatusPublicCache.data = append([]ModelStatus(nil), statuses...)
	modelStatusPublicCache.Unlock()

	return statuses, nil
}

func ModelStatusConfig(public bool) map[string]interface{} {
	cfg := setting.GetEnhancementSetting()
	selected := append([]string{}, cfg.SelectedModels...)
	currentWindow := ModelStatusConfigWindowFromMinutes(cfg.ModelStatusTimeWindowMins)
	refreshMinutes := cfg.ModelStatusRefreshSeconds / 60
	if refreshMinutes < 1 {
		refreshMinutes = 1
	}
	greenThreshold, yellowThreshold := ModelStatusThresholds()
	base := map[string]interface{}{
		"site_title":               cfg.ModelStatusSiteTitle,
		"theme":                    cfg.ModelStatusTheme,
		"refresh_interval":         cfg.ModelStatusRefreshSeconds,
		"refresh_interval_minutes": refreshMinutes,
		"slot_minutes":             ModelStatusSlotMinutes(),
		"green_threshold":          greenThreshold,
		"yellow_threshold":         yellowThreshold,
		"show_zero_request_models": cfg.ModelStatusShowZeroRequests,
		"sort_mode":                cfg.ModelStatusSortMode,
		"selected_models":          selected,
		"time_window_minutes":      cfg.ModelStatusTimeWindowMins,
		"time_windows":             ModelStatusTimeWindows(),
		"default_window":           ModelStatusWindow24h,
		"current_window":           currentWindow,
		"public_embed_enabled":     cfg.PublicEmbedEnabled,
		"public_url_path":          "/model-status",
	}
	if public {
		base["public"] = true
	} else {
		base["model_status_ignore_error_keywords_enabled"] = cfg.ModelStatusIgnoreErrorKeywordsEnabled
		base["model_status_ignored_error_keywords"] = append([]string{}, cfg.ModelStatusIgnoredErrorKeywords...)
	}
	return base
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
