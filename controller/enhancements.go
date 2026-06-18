package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/enhancement"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type modelStatusRequest struct {
	Models            []string `json:"models"`
	Window            string   `json:"window"`
	TimeWindowMinutes int      `json:"time_window_minutes"`
}

func RegisterEnhancementRoutes(r *gin.RouterGroup) {
	dashboard := r.Group("/dashboard")
	{
		dashboard.GET("/overview", enhancementDashboardOverview)
		dashboard.GET("/usage", enhancementDashboardUsage)
		dashboard.GET("/models", enhancementDashboardModels)
		dashboard.GET("/trends/daily", enhancementDashboardDailyTrend)
		dashboard.GET("/trends/hourly", enhancementDashboardHourlyTrend)
		dashboard.GET("/top-users", enhancementDashboardTopUsers)
		dashboard.GET("/channels", enhancementDashboardChannels)
		dashboard.POST("/cache/invalidate", enhancementClearCache)
		dashboard.GET("/refresh-estimate", enhancementRefreshEstimate)
		dashboard.GET("/system-info", enhancementSystemInfo)
	}

	redemptions := r.Group("/redemptions")
	{
		redemptions.POST("/generate", enhancementGenerateRedemptions)
		redemptions.GET("", enhancementListRedemptions)
		redemptions.GET("/", enhancementListRedemptions)
		redemptions.GET("/statistics", enhancementRedemptionStats)
		redemptions.POST("/batch-delete", enhancementBatchDeleteRedemptions)
		redemptions.POST("/batch", enhancementBatchDeleteRedemptions)
		redemptions.DELETE("/batch", enhancementBatchDeleteRedemptions)
		redemptions.POST("/:id/disable", enhancementDisableRedemption)
		redemptions.POST("/:id/enable", enhancementEnableRedemption)
		redemptions.DELETE("/:id", enhancementDeleteRedemption)
	}

	users := r.Group("/users")
	{
		users.GET("/activity-stats", enhancementUserActivityStats)
		users.GET("/stats", enhancementUserActivityStats)
		users.GET("/banned", enhancementBannedUsers)
		users.GET("", enhancementListUsers)
		users.GET("/", enhancementListUsers)
		users.GET("/soft-deleted/count", enhancementSoftDeletedUserCount)
		users.POST("/tokens/:token_id/disable", enhancementDisableToken)
		users.GET("/:user_id/invited", enhancementInvitedUsers)
		users.POST("/:user_id/ban", enhancementBanUser)
		users.POST("/:user_id/unban", enhancementUnbanUser)
	}

	tokens := r.Group("/tokens")
	{
		tokens.GET("", enhancementListTokens)
		tokens.GET("/", enhancementListTokens)
		tokens.GET("/statistics", enhancementTokenStats)
		tokens.GET("/groups", enhancementTokenGroups)
		tokens.PUT("/:token_id", enhancementUpdateToken)
	}

	risk := r.Group("/risk")
	{
		risk.GET("/ip-log-coverage", enhancementIPLogCoverage)
		risk.POST("/ip-log/enable-all", enhancementEnableAllRecordIPLog)
		risk.GET("/shared-token-ips", enhancementSharedTokenIPs)
		risk.GET("/token-multi-ips", enhancementTokenMultiIPs)
		risk.GET("/leaderboards", enhancementRiskLeaderboards)
		risk.GET("/users/:user_id/analysis", enhancementUserRiskAnalysis)
		risk.GET("/ban-records", enhancementBanRecords)
		risk.GET("/token-rotation", enhancementTokenRotation)
		risk.GET("/affiliated-accounts", enhancementAffiliatedAccounts)
	}

	modelStatus := r.Group("/model-status")
	{
		modelStatus.GET("/time-windows", enhancementModelStatusTimeWindows)
		modelStatus.GET("/models", enhancementModelStatusModels)
		modelStatus.POST("/status/multiple", enhancementModelStatusMultiple)
		modelStatus.POST("/status/batch", enhancementModelStatusMultiple)
		modelStatus.GET("/status/all", enhancementModelStatusAll)
		modelStatus.GET("/status/:model_name", enhancementModelStatusOne)
		modelStatus.GET("/selected", enhancementModelStatusSelected)
		modelStatus.GET("/config/selected", enhancementModelStatusSelected)
		modelStatus.GET("/config/time-window", enhancementModelStatusConfig)
		modelStatus.GET("/config/theme", enhancementModelStatusConfig)
		modelStatus.GET("/config/refresh-interval", enhancementModelStatusConfig)
		modelStatus.GET("/config/sort-mode", enhancementModelStatusConfig)
		modelStatus.GET("/config/show-zero-request-models", enhancementModelStatusConfig)
		modelStatus.GET("/config/ignore-error-keywords-enabled", enhancementModelStatusConfig)
		modelStatus.GET("/config/ignored-error-keywords", enhancementModelStatusConfig)
		modelStatus.GET("/config/groups", enhancementTokenGroups)
		modelStatus.GET("/config/site-title", enhancementModelStatusConfig)
		modelStatus.GET("/token-groups", enhancementTokenGroups)
	}

	autoGroup := r.Group("/auto-group")
	{
		autoGroup.GET("/config", enhancementAutoGroupConfig)
		autoGroup.POST("/config", enhancementReadOnlyPlaceholder("auto group config is managed by existing system settings"))
		autoGroup.GET("/stats", enhancementAutoGroupStats)
		autoGroup.GET("/groups", enhancementAutoGroupConfig)
		autoGroup.GET("/preview", enhancementAutoGroupPreview)
		autoGroup.GET("/users", enhancementAutoGroupPreview)
		autoGroup.POST("/scan", enhancementAutoGroupScan)
		autoGroup.GET("/logs", enhancementAutoGroupLogs)
	}

	aiBan := r.Group("/ai-ban")
	{
		aiBan.GET("/config", enhancementAIBanConfig)
		aiBan.POST("/reset-api-health", enhancementReadOnlyPlaceholder("AI health state reset is not needed until AI ban is enabled"))
		aiBan.GET("/audit-logs", enhancementAIBanAuditLogs)
		aiBan.GET("/groups", enhancementAutoGroupConfig)
		aiBan.GET("/available-groups", enhancementAutoGroupConfig)
		aiBan.GET("/models", enhancementModelStatusModels)
		aiBan.GET("/available-models-for-exclude", enhancementModelStatusModels)
		aiBan.GET("/suspicious", enhancementAIBanSuspicious)
		aiBan.GET("/suspicious-users", enhancementAIBanSuspicious)
		aiBan.POST("/assess", enhancementAIBanDryRun)
		aiBan.POST("/scan", enhancementAIBanDryRun)
		aiBan.POST("/test-connection", enhancementReadOnlyPlaceholder("AI connection tests require root configuration"))
		aiBan.GET("/whitelist", enhancementEmptyList)
		aiBan.GET("/whitelist/search", enhancementEmptyList)
	}

	system := r.Group("/system")
	{
		system.GET("/info", enhancementSystemInfo)
		system.GET("/scale", enhancementSystemScale)
		system.GET("/indexes", enhancementIndexStatus)
		system.GET("/cache", enhancementSystemCache)
		system.GET("/warmup-status", enhancementWarmupStatus)
	}

	r.GET("/linuxdo/lookup/:id", enhancementLinuxDOLookup)
}

func RegisterEnhancementRootRoutes(r *gin.RouterGroup) {
	r.POST("/users/batch-delete", enhancementBatchDeleteUsers)
	r.DELETE("/users/:user_id", enhancementDeleteUser)
	r.POST("/users/soft-deleted/purge", enhancementPurgeSoftDeletedUsers)
	r.POST("/system/indexes/ensure", enhancementEnsureIndexes)
	r.PUT("/model-status/selected", enhancementModelStatusSaveSelected)
	r.POST("/model-status/config/selected", enhancementModelStatusSaveSelected)
	r.PUT("/model-status/config/time-window", enhancementModelStatusSaveOption("model_status_time_window_mins"))
	r.PUT("/model-status/config/theme", enhancementModelStatusSaveOption("model_status_theme"))
	r.PUT("/model-status/config/refresh-interval", enhancementModelStatusSaveOption("model_status_refresh_seconds"))
	r.PUT("/model-status/config/slot-granularity", enhancementModelStatusSaveOption("model_status_slot_minutes"))
	r.PUT("/model-status/config/threshold-green", enhancementModelStatusSaveOption("model_status_green_threshold"))
	r.PUT("/model-status/config/threshold-yellow", enhancementModelStatusSaveOption("model_status_yellow_threshold"))
	r.PUT("/model-status/config/show-zero-request-models", enhancementModelStatusSaveOption("model_status_show_zero_requests"))
	r.PUT("/model-status/config/ignore-error-keywords-enabled", enhancementModelStatusSaveOption("model_status_ignore_error_keywords_enabled"))
	r.PUT("/model-status/config/ignored-error-keywords", enhancementModelStatusSaveOption("model_status_ignored_error_keywords"))
	r.PUT("/model-status/config/sort-mode", enhancementModelStatusSaveOption("model_status_sort_mode"))
	r.PUT("/model-status/config/custom-order", enhancementModelStatusSaveSelected)
	r.PUT("/model-status/config/groups", enhancementReadOnlyPlaceholder("token groups are derived from tokens"))
	r.PUT("/model-status/config/site-title", enhancementModelStatusSaveOption("model_status_site_title"))
	r.PUT("/model-status/config/public-embed", enhancementModelStatusSaveOption("public_embed_enabled"))
	r.POST("/auto-group/batch-move", enhancementRootDryRunPlaceholder("auto-group batch move"))
	r.POST("/auto-group/revert", enhancementRootDryRunPlaceholder("auto-group revert"))
	r.POST("/ai-ban/config", enhancementSaveAIBanConfig)
	r.DELETE("/ai-ban/audit-logs", enhancementRootDryRunPlaceholder("ai-ban audit cleanup"))
	r.POST("/ai-ban/fetch-models", enhancementModelStatusModels)
	r.POST("/ai-ban/models", enhancementModelStatusModels)
	r.POST("/ai-ban/test-model", enhancementRootDryRunPlaceholder("ai-ban model test"))
	r.POST("/ai-ban/whitelist/add", enhancementRootDryRunPlaceholder("ai-ban whitelist add"))
	r.POST("/ai-ban/whitelist/remove", enhancementRootDryRunPlaceholder("ai-ban whitelist remove"))
}

func RegisterEnhancementModelStatusEmbedRoutes(r *gin.RouterGroup) {
	r.GET("/time-windows", enhancementEmbedTimeWindows)
	r.GET("/models", enhancementEmbedModels)
	r.POST("/status/multiple", enhancementEmbedStatusMultiple)
	r.POST("/status/batch", enhancementEmbedStatusMultiple)
	r.GET("/status/all", enhancementEmbedStatusAll)
	r.GET("/status/:model_name", enhancementEmbedStatusOne)
	r.GET("/config", enhancementEmbedConfig)
	r.GET("/config/selected", enhancementEmbedSelected)
	r.GET("/token-groups", enhancementEmbedTokenGroups)
}

func queryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(c.Query(key))
	if err != nil {
		return fallback
	}
	return value
}

func queryInt64(c *gin.Context, key string, fallback int64) int64 {
	value, err := strconv.ParseInt(c.Query(key), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func pathInt(c *gin.Context, key string) (int, error) {
	return strconv.Atoi(c.Param(key))
}

func operator(c *gin.Context) (int, int) {
	return c.GetInt("id"), c.GetInt("role")
}

func respondPublic(c *gin.Context, data interface{}, err error) {
	if err == nil {
		common.ApiSuccess(c, data)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "not found"})
		return
	}
	common.ApiError(c, err)
}

func enhancementDashboardOverview(c *gin.Context) {
	data, err := enhancement.DashboardOverview()
	respondPublic(c, data, err)
}

func enhancementDashboardUsage(c *gin.Context) {
	start := queryInt64(c, "start", 0)
	end := queryInt64(c, "end", 0)
	data, err := enhancement.UsageSummary(start, end)
	respondPublic(c, data, err)
}

func enhancementDashboardModels(c *gin.Context) {
	start := queryInt64(c, "start", 0)
	end := queryInt64(c, "end", 0)
	limit := queryInt(c, "limit", 20)
	data, err := enhancement.ModelUsageList(start, end, limit)
	respondPublic(c, data, err)
}

func enhancementDashboardDailyTrend(c *gin.Context) {
	start := queryInt64(c, "start", 0)
	end := queryInt64(c, "end", 0)
	data, err := enhancement.UsageTrend(start, end, "daily")
	respondPublic(c, data, err)
}

func enhancementDashboardHourlyTrend(c *gin.Context) {
	start := queryInt64(c, "start", 0)
	end := queryInt64(c, "end", 0)
	data, err := enhancement.UsageTrend(start, end, "hourly")
	respondPublic(c, data, err)
}

func enhancementDashboardTopUsers(c *gin.Context) {
	start := queryInt64(c, "start", 0)
	end := queryInt64(c, "end", 0)
	limit := queryInt(c, "limit", 20)
	data, err := enhancement.TopUsers(start, end, limit)
	respondPublic(c, data, err)
}

func enhancementDashboardChannels(c *gin.Context) {
	data, err := enhancement.ChannelSummaries(queryInt(c, "limit", 20))
	respondPublic(c, data, err)
}

func enhancementClearCache(c *gin.Context) {
	operatorId, _ := operator(c)
	common.ApiSuccess(c, enhancement.ClearEnhancementCache(operatorId))
}

func enhancementRefreshEstimate(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"estimated_seconds": 1, "cache_scope": "enhancements"})
}

func enhancementSystemInfo(c *gin.Context) {
	common.ApiSuccess(c, enhancement.SystemInfo())
}

func enhancementListRedemptions(c *gin.Context) {
	keyword := c.Query("keyword")
	if keyword == "" {
		keyword = c.Query("search")
	}
	data, err := enhancement.ListRedemptions(queryInt(c, "p", 1), queryInt(c, "page_size", 20), queryInt(c, "status", 0), keyword)
	respondPublic(c, data, err)
}

func enhancementRedemptionStats(c *gin.Context) {
	data, err := enhancement.RedemptionStats()
	respondPublic(c, data, err)
}

func enhancementGenerateRedemptions(c *gin.Context) {
	var req enhancement.GenerateRedemptionsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	data, err := enhancement.GenerateRedemptions(req, operatorId)
	respondPublic(c, data, err)
}

func enhancementDeleteRedemption(c *gin.Context) {
	id, err := pathInt(c, "id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, role := operator(c)
	err = enhancement.DeleteRedemption(id, operatorId, role >= common.RoleRootUser)
	respondPublic(c, gin.H{"deleted": true}, err)
}

func enhancementDisableRedemption(c *gin.Context) {
	id, err := pathInt(c, "id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	data, err := enhancement.DisableRedemption(id, operatorId)
	respondPublic(c, data, err)
}

func enhancementEnableRedemption(c *gin.Context) {
	id, err := pathInt(c, "id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	data, err := enhancement.EnableRedemption(id, operatorId)
	respondPublic(c, data, err)
}

func enhancementBatchDeleteRedemptions(c *gin.Context) {
	var req enhancement.BatchIDsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, role := operator(c)
	data, err := enhancement.BatchDeleteRedemptions(req.Ids, operatorId, role >= common.RoleRootUser)
	respondPublic(c, data, err)
}

func enhancementUserActivityStats(c *gin.Context) {
	data, err := enhancement.UserActivityStats(queryInt64(c, "start", 0), queryInt64(c, "end", 0))
	respondPublic(c, data, err)
}

func enhancementBannedUsers(c *gin.Context) {
	data, err := enhancement.ListUsers(queryInt(c, "p", 1), queryInt(c, "page_size", 20), common.UserStatusDisabled, c.Query("group"))
	respondPublic(c, data, err)
}

func enhancementListUsers(c *gin.Context) {
	data, err := enhancement.ListUsers(queryInt(c, "p", 1), queryInt(c, "page_size", 20), queryInt(c, "status", 0), c.Query("group"))
	respondPublic(c, data, err)
}

func enhancementSoftDeletedUserCount(c *gin.Context) {
	data, err := enhancement.SoftDeletedUserCount()
	respondPublic(c, gin.H{"count": data}, err)
}

func enhancementInvitedUsers(c *gin.Context) {
	userId, err := pathInt(c, "user_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := enhancement.InvitedUsers(userId, queryInt(c, "p", 1), queryInt(c, "page_size", 20))
	respondPublic(c, data, err)
}

func enhancementBanUser(c *gin.Context) {
	userId, err := pathInt(c, "user_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req enhancement.BanUserRequest
	_ = common.DecodeJson(c.Request.Body, &req)
	operatorId, role := operator(c)
	err = enhancement.BanUser(userId, operatorId, role, req.Reason)
	respondPublic(c, gin.H{"banned": true}, err)
}

func enhancementUnbanUser(c *gin.Context) {
	userId, err := pathInt(c, "user_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, role := operator(c)
	err = enhancement.UnbanUser(userId, operatorId, role)
	respondPublic(c, gin.H{"unbanned": true}, err)
}

func enhancementDisableToken(c *gin.Context) {
	tokenId, err := pathInt(c, "token_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	err = enhancement.DisableToken(tokenId, operatorId)
	respondPublic(c, gin.H{"disabled": true}, err)
}

func enhancementListTokens(c *gin.Context) {
	data, err := enhancement.ListTokens(queryInt(c, "p", 1), queryInt(c, "page_size", 20), queryInt(c, "status", 0), c.Query("group"), c.Query("key"))
	respondPublic(c, data, err)
}

func enhancementUpdateToken(c *gin.Context) {
	tokenId, err := pathInt(c, "token_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var req enhancement.UpdateTokenRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	data, err := enhancement.UpdateToken(tokenId, req, operatorId)
	respondPublic(c, data, err)
}

func enhancementTokenStats(c *gin.Context) {
	data, err := enhancement.TokenStats()
	respondPublic(c, data, err)
}

func enhancementTokenGroups(c *gin.Context) {
	data, err := enhancement.TokenGroups()
	respondPublic(c, data, err)
}

func enhancementRiskLeaderboards(c *gin.Context) {
	data, err := enhancement.RiskLeaderboards(queryInt64(c, "start", 0), queryInt64(c, "end", 0), queryInt(c, "limit", 20))
	respondPublic(c, data, err)
}

func enhancementIPLogCoverage(c *gin.Context) {
	data, err := enhancement.IPLogCoverageStats()
	respondPublic(c, data, err)
}

func enhancementEnableAllRecordIPLog(c *gin.Context) {
	operatorId, _ := operator(c)
	data, err := enhancement.EnableAllRecordIPLog(operatorId)
	respondPublic(c, data, err)
}

func ipRiskQuery(c *gin.Context) enhancement.IPRiskQuery {
	return enhancement.IPRiskQuery{
		Page:     queryInt(c, "p", 1),
		PageSize: queryInt(c, "page_size", 20),
		Start:    queryInt64(c, "start", 0),
		End:      queryInt64(c, "end", 0),
		Sort:     c.Query("sort"),
		Order:    c.Query("order"),
		Keyword:  c.Query("keyword"),
	}
}

func enhancementSharedTokenIPs(c *gin.Context) {
	data, err := enhancement.SharedTokenIPs(ipRiskQuery(c))
	respondPublic(c, data, err)
}

func enhancementTokenMultiIPs(c *gin.Context) {
	data, err := enhancement.TokenMultiIPs(ipRiskQuery(c))
	respondPublic(c, data, err)
}

func enhancementUserRiskAnalysis(c *gin.Context) {
	userId, err := pathInt(c, "user_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := enhancement.UserRiskAnalysis(userId, queryInt64(c, "start", 0), queryInt64(c, "end", 0))
	respondPublic(c, data, err)
}

func enhancementBanRecords(c *gin.Context) {
	data, err := enhancement.ListUsers(queryInt(c, "p", 1), queryInt(c, "page_size", 20), common.UserStatusDisabled, "")
	respondPublic(c, data, err)
}

func enhancementTokenRotation(c *gin.Context) {
	data, err := enhancement.ListTokens(queryInt(c, "p", 1), queryInt(c, "page_size", 20), 0, "", "")
	respondPublic(c, data, err)
}

func enhancementAffiliatedAccounts(c *gin.Context) {
	data, err := enhancement.RiskLeaderboards(queryInt64(c, "start", 0), queryInt64(c, "end", 0), queryInt(c, "limit", 20))
	respondPublic(c, data, err)
}

func enhancementIndexStatus(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"status": "managed_by_gorm_migrations", "ensure_requires_root": true})
}

func modelStatusWindowFromQuery(c *gin.Context) string {
	if window := c.Query("window"); window != "" {
		return window
	}
	if minutes := queryInt(c, "time_window_minutes", 0); minutes > 0 {
		return enhancement.ModelStatusWindowFromMinutes(minutes)
	}
	return ""
}

func modelStatusWindowFromRequest(c *gin.Context, req modelStatusRequest) string {
	if window := modelStatusWindowFromQuery(c); window != "" {
		return window
	}
	if req.Window != "" {
		return req.Window
	}
	if req.TimeWindowMinutes > 0 {
		return enhancement.ModelStatusWindowFromMinutes(req.TimeWindowMinutes)
	}
	return ""
}

func enhancementModelStatusTimeWindows(c *gin.Context) {
	common.ApiSuccess(c, enhancement.ModelStatusTimeWindows())
}

func enhancementModelStatusModels(c *gin.Context) {
	data, err := enhancement.AvailableModels(false)
	respondPublic(c, data, err)
}

func enhancementModelStatusOne(c *gin.Context) {
	data, err := enhancement.ModelStatusForGroupWindow(c.Query("group"), c.Param("model_name"), modelStatusWindowFromQuery(c), false)
	respondPublic(c, data, err)
}

func enhancementModelStatusMultiple(c *gin.Context) {
	var req modelStatusRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := enhancement.ModelStatusesForWindow(req.Models, modelStatusWindowFromRequest(c, req), false)
	respondPublic(c, data, err)
}

func enhancementModelStatusAll(c *gin.Context) {
	data, err := enhancement.ModelStatusesForWindow(nil, modelStatusWindowFromQuery(c), false)
	respondPublic(c, data, err)
}

func enhancementModelStatusSelected(c *gin.Context) {
	common.ApiSuccess(c, enhancement.ModelStatusConfig(false)["selected_models"])
}

func enhancementModelStatusConfig(c *gin.Context) {
	common.ApiSuccess(c, enhancement.ModelStatusConfig(false))
}

func enhancementModelStatusSaveSelected(c *gin.Context) {
	var req modelStatusRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	err := enhancement.SaveSelectedModels(req.Models, operatorId)
	respondPublic(c, gin.H{"saved": true}, err)
}

func enhancementModelStatusSaveOption(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req map[string]interface{}
		if err := common.DecodeJson(c.Request.Body, &req); err != nil {
			common.ApiError(c, err)
			return
		}
		value := ""
		if raw, ok := req["value"]; ok {
			value = common.Interface2String(raw)
		}
		operatorId, _ := operator(c)
		err := enhancement.SaveModelStatusOption(key, value, operatorId)
		respondPublic(c, gin.H{"saved": true}, err)
	}
}

func enhancementAutoGroupConfig(c *gin.Context) {
	common.ApiSuccess(c, enhancement.AutoGroupConfig())
}

func enhancementAutoGroupStats(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"enabled": false, "dry_run_default": true})
}

func enhancementAutoGroupPreview(c *gin.Context) {
	data, err := enhancement.AutoGroupPreview(queryInt(c, "limit", 20))
	respondPublic(c, data, err)
}

func enhancementAutoGroupScan(c *gin.Context) {
	data, err := enhancement.AutoGroupPreview(queryInt(c, "limit", 20))
	respondPublic(c, gin.H{"dry_run": true, "candidates": data}, err)
}

func enhancementAutoGroupLogs(c *gin.Context) {
	common.ApiSuccess(c, []gin.H{})
}

func enhancementAIBanConfig(c *gin.Context) {
	common.ApiSuccess(c, enhancement.AIBanConfig())
}

func enhancementAIBanAuditLogs(c *gin.Context) {
	common.ApiSuccess(c, []gin.H{})
}

func enhancementAIBanSuspicious(c *gin.Context) {
	data, err := enhancement.RiskLeaderboards(queryInt64(c, "start", 0), queryInt64(c, "end", 0), queryInt(c, "limit", 20))
	respondPublic(c, data, err)
}

func enhancementAIBanDryRun(c *gin.Context) {
	data, err := enhancement.RiskLeaderboards(queryInt64(c, "start", 0), queryInt64(c, "end", 0), queryInt(c, "limit", 20))
	respondPublic(c, gin.H{"dry_run": true, "candidates": data}, err)
}

func enhancementSystemScale(c *gin.Context) {
	data, err := enhancement.DashboardOverview()
	respondPublic(c, data, err)
}

func enhancementSystemCache(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"redis_enabled": common.RedisEnabled, "memory_cache_enabled": common.MemoryCacheEnabled})
}

func enhancementWarmupStatus(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"status": "ready"})
}

func enhancementLinuxDOLookup(c *gin.Context) {
	id := c.Param("id")
	if _, err := strconv.Atoi(id); err != nil || len(id) > 32 {
		common.ApiErrorMsg(c, "invalid LinuxDO id")
		return
	}
	data, err := enhancement.LinuxDOLookup(id)
	respondPublic(c, data, err)
}

func enhancementEmptyList(c *gin.Context) {
	common.ApiSuccess(c, []gin.H{})
}

func enhancementReadOnlyPlaceholder(message string) gin.HandlerFunc {
	return func(c *gin.Context) {
		common.ApiSuccess(c, gin.H{"applied": false, "message": message})
	}
}

func enhancementRootDryRunPlaceholder(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		common.ApiSuccess(c, gin.H{"dry_run": true, "action": action, "applied": false})
	}
}

func enhancementBatchDeleteUsers(c *gin.Context) {
	var req enhancement.BatchIDsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, role := operator(c)
	data, err := enhancement.BatchDeleteUsers(req.Ids, operatorId, role)
	respondPublic(c, data, err)
}

func enhancementDeleteUser(c *gin.Context) {
	userId, err := pathInt(c, "user_id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, role := operator(c)
	err = enhancement.DeleteUser(userId, operatorId, role)
	respondPublic(c, gin.H{"deleted": true}, err)
}

func enhancementPurgeSoftDeletedUsers(c *gin.Context) {
	operatorId, role := operator(c)
	count, err := enhancement.PurgeSoftDeletedUsers(operatorId, role)
	respondPublic(c, gin.H{"deleted": count}, err)
}

func enhancementEnsureIndexes(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"ensured": false, "message": "indexes are maintained by migrations and model tags"})
}

func enhancementSaveAIBanConfig(c *gin.Context) {
	var req map[string]interface{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorId, _ := operator(c)
	err := enhancement.SaveAIBanConfig(req, operatorId)
	respondPublic(c, gin.H{"saved": true}, err)
}

func enhancementEmbedTimeWindows(c *gin.Context) {
	if err := enhancement.RequirePublicEmbedEnabled(); err != nil {
		respondPublic(c, nil, err)
		return
	}
	enhancementModelStatusTimeWindows(c)
}

func enhancementEmbedModels(c *gin.Context) {
	if err := enhancement.RequirePublicEmbedEnabled(); err != nil {
		respondPublic(c, nil, err)
		return
	}
	data, err := enhancement.AvailableModels(true)
	respondPublic(c, data, err)
}

func enhancementEmbedStatusOne(c *gin.Context) {
	data, err := enhancement.ModelStatusForGroupWindow(c.Query("group"), c.Param("model_name"), enhancement.ModelStatusConfiguredWindow(), true)
	respondPublic(c, data, err)
}

func enhancementEmbedStatusMultiple(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var req modelStatusRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := enhancement.ModelStatusesForWindow(req.Models, enhancement.ModelStatusConfiguredWindow(), true)
	respondPublic(c, data, err)
}

func enhancementEmbedStatusAll(c *gin.Context) {
	data, err := enhancement.ModelStatusesForPublicConfig()
	respondPublic(c, data, err)
}

func enhancementEmbedConfig(c *gin.Context) {
	if err := enhancement.RequirePublicEmbedEnabled(); err != nil {
		respondPublic(c, nil, err)
		return
	}
	common.ApiSuccess(c, enhancement.ModelStatusConfig(true))
}

func enhancementEmbedSelected(c *gin.Context) {
	if err := enhancement.RequirePublicEmbedEnabled(); err != nil {
		respondPublic(c, nil, err)
		return
	}
	data, err := enhancement.AvailableModels(true)
	respondPublic(c, data, err)
}

func enhancementEmbedTokenGroups(c *gin.Context) {
	if err := enhancement.RequirePublicEmbedEnabled(); err != nil {
		respondPublic(c, nil, err)
		return
	}
	common.ApiSuccess(c, []gin.H{})
}
