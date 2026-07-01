package enhancement

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelStatusTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.Log{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})

	return db
}

func setupEnhancementListTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Redemption{}, &model.Channel{}, &model.Ability{}, &model.Log{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
	})

	return db
}

func seedEnhancementUser(t *testing.T, username string, quota int, usedQuota int, group string) model.User {
	t.Helper()
	user := model.User{
		Username:     username,
		Password:     "password",
		DisplayName:  username + "-display",
		Status:       common.UserStatusEnabled,
		Email:        username + "@example.com",
		Quota:        quota,
		UsedQuota:    usedQuota,
		RequestCount: quota / 10,
		Group:        group,
		AffCode:      "aff_" + username,
		LinuxDOId:    "linux_" + username,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func TestEnhancementSummariesRevealFieldsInsideEnhancementAPI(t *testing.T) {
	setupEnhancementListTestDB(t)
	user := seedEnhancementUser(t, "reveal", 100, 10, "default")
	token := model.Token{
		UserId:      user.Id,
		Name:        "reveal-token",
		Key:         "sk-reveal-full-key",
		Status:      common.TokenStatusEnabled,
		Group:       "default",
		RemainQuota: 100,
	}
	require.NoError(t, model.DB.Create(&token).Error)

	cfg := setting.GetEnhancementSetting()
	originalBaseURL := cfg.AIBanBaseURL
	originalAPIKey := cfg.AIBanAPIKey
	t.Cleanup(func() {
		cfg.AIBanBaseURL = originalBaseURL
		cfg.AIBanAPIKey = originalAPIKey
	})
	cfg.AIBanBaseURL = "https://ai-ban.example.com/v1"
	cfg.AIBanAPIKey = "secret-key"

	users, err := ListUsers(ListQuery{Page: 1, PageSize: 20, Keyword: "reveal"})
	require.NoError(t, err)
	require.Len(t, users.Items, 1)
	require.Equal(t, "reveal@example.com", users.Items[0].Email)
	require.Equal(t, "aff_reveal", users.Items[0].AffCode)
	require.Equal(t, "linux_reveal", users.Items[0].LinuxDOId)
	require.NotContains(t, users.Items[0].Email, "***masked***")
	require.NotContains(t, users.Items[0].LinuxDOId, "***masked***")

	tokens, err := ListTokens(ListQuery{Page: 1, PageSize: 20, Keyword: "reveal-token"})
	require.NoError(t, err)
	require.Len(t, tokens.Items, 1)
	require.Equal(t, "sk-reveal-full-key", tokens.Items[0].Key)

	config := AIBanConfig()
	require.Equal(t, "https://ai-ban.example.com/v1", config["base_url"])
	require.Equal(t, true, config["api_key_set"])
	_, hasAPIKey := config["api_key"]
	require.False(t, hasAPIKey)
}

func TestEnhancementListsFilterAndSortBeforePagination(t *testing.T) {
	setupEnhancementListTestDB(t)
	alpha := seedEnhancementUser(t, "alpha", 100, 10, "default")
	beta := seedEnhancementUser(t, "beta", 300, 30, "vip")

	require.NoError(t, model.DB.Create(&[]model.Token{
		{UserId: alpha.Id, Name: "alpha-token", Key: "sk-alpha-key", Status: common.TokenStatusEnabled, Group: "default", UsedQuota: 10},
		{UserId: beta.Id, Name: "beta-token", Key: "sk-beta-key", Status: common.TokenStatusEnabled, Group: "vip", UsedQuota: 90},
	}).Error)
	require.NoError(t, model.DB.Create(&[]model.Redemption{
		{UserId: alpha.Id, Key: "alpha-redemption", Status: common.RedemptionCodeStatusUsed, Name: "promo-alpha", Quota: 100, UsedUserId: alpha.Id},
		{UserId: beta.Id, Key: "beta-redemption", Status: common.RedemptionCodeStatusUsed, Name: "promo-beta", Quota: 300, UsedUserId: beta.Id},
	}).Error)

	users, err := ListUsers(ListQuery{
		Page:     1,
		PageSize: 20,
		Sort:     "quota",
		Order:    "desc",
		Filters:  map[string]string{"group": "vip"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), users.Total)
	require.Equal(t, "beta", users.Items[0].Username)

	users, err = ListUsers(ListQuery{
		Page:     1,
		PageSize: 20,
		Keyword:  "alpha-redemption",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), users.Total)
	require.Equal(t, "alpha", users.Items[0].Username)
	require.Equal(t, 1, users.Items[0].RedemptionCount)
	require.Contains(t, users.Items[0].RedemptionCodes, "alpha-redemption")

	tokens, err := ListTokens(ListQuery{
		Page:     1,
		PageSize: 20,
		Sort:     "used_quota",
		Order:    "desc",
		Filters:  map[string]string{"key": "sk-"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"beta-token", "alpha-token"}, []string{tokens.Items[0].Name, tokens.Items[1].Name})

	redemptions, err := ListRedemptions(ListQuery{
		Page:     1,
		PageSize: 1,
		Keyword:  "promo",
		Sort:     "quota",
		Order:    "asc",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), redemptions.Total)
	require.Len(t, redemptions.Items, 1)
	require.Equal(t, "promo-alpha", redemptions.Items[0].Name)
}

func TestDeleteTokenRemovesToken(t *testing.T) {
	setupEnhancementListTestDB(t)
	user := seedEnhancementUser(t, "delete-token-user", 100, 10, "default")
	token := model.Token{
		UserId:      user.Id,
		Name:        "delete-token",
		Key:         "sk-delete-token-key",
		Status:      common.TokenStatusEnabled,
		Group:       "default",
		RemainQuota: 100,
	}
	require.NoError(t, model.DB.Create(&token).Error)

	require.NoError(t, DeleteToken(token.Id, 999))

	var fetched model.Token
	err := model.DB.First(&fetched, "id = ?", token.Id).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestDeleteTokenRejectsInvalidID(t *testing.T) {
	setupEnhancementListTestDB(t)

	require.Error(t, DeleteToken(0, 999))
	require.Error(t, DeleteToken(-1, 999))
}

func TestDeleteTokenReturnsErrorForMissingToken(t *testing.T) {
	setupEnhancementListTestDB(t)

	err := DeleteToken(12345, 999)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestEnhancementAggregateListsFilterBeforePagination(t *testing.T) {
	db := setupEnhancementListTestDB(t)
	alpha := seedEnhancementUser(t, "risk-alpha", 100, 10, "default")
	beta := seedEnhancementUser(t, "risk-beta", 500, 80, "vip")
	now := common.GetTimestamp()
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{
		{UserId: alpha.Id, Username: alpha.Username, Type: model.LogTypeConsume, CreatedAt: now - 60, Ip: "203.0.113.1", Quota: 10},
		{UserId: beta.Id, Username: beta.Username, Type: model.LogTypeConsume, CreatedAt: now - 50, Ip: "203.0.113.2", Quota: 50},
		{UserId: beta.Id, Username: beta.Username, Type: model.LogTypeConsume, CreatedAt: now - 40, Ip: "203.0.113.3", Quota: 60},
	}).Error)

	riskPage, err := RiskLeaderboardsPage(0, 0, ListQuery{
		Page:     1,
		PageSize: 20,
		Filters:  map[string]string{"username": "risk-beta"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), riskPage.Total)
	require.Equal(t, beta.Id, riskPage.Items[0].UserId)

	channel := model.Channel{Name: "status-channel", Key: "status-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "status-alpha", ChannelId: channel.Id, Enabled: true},
		{Group: "vip", Model: "status-beta", ChannelId: channel.Id, Enabled: true},
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.Log{Type: model.LogTypeConsume, CreatedAt: now - 30, Group: "vip", ModelName: "status-beta"}).Error)

	statusPage, err := ModelStatusesPageForWindow(ModelStatusWindow24h, ListQuery{
		Page:     1,
		PageSize: 20,
		Filters:  map[string]string{"model_name": "status-beta"},
	}, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), statusPage.Total)
	require.Equal(t, "status-beta", statusPage.Items[0].ModelName)

	originalAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
	})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
	autoPage, err := AutoGroupPreview(ListQuery{
		Page:     1,
		PageSize: 20,
		Sort:     "used_quota",
		Order:    "desc",
		Filters:  map[string]string{"username": "risk-beta"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), autoPage.Total)
	require.Equal(t, "risk-beta", autoPage.Items[0].Username)
}

func configurePublicModelStatusGroups(t *testing.T, groupDisplay string, usableGroups string) {
	t.Helper()

	originalPublicEmbedEnabled := setting.GetEnhancementSetting().PublicEmbedEnabled
	originalGroupDisplay := ratio_setting.GroupDisplay2JSONString()
	originalUserUsableGroups := setting.UserUsableGroups2JSONString()

	t.Cleanup(func() {
		setting.GetEnhancementSetting().PublicEmbedEnabled = originalPublicEmbedEnabled
		require.NoError(t, ratio_setting.UpdateGroupDisplayByJSONString(originalGroupDisplay))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUserUsableGroups))
		ClearModelStatusPublicCache()
	})

	setting.GetEnhancementSetting().PublicEmbedEnabled = true
	require.NoError(t, ratio_setting.UpdateGroupDisplayByJSONString(groupDisplay))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(usableGroups))
	ClearModelStatusPublicCache()
}

func statusKeys(statuses []ModelStatus) []string {
	keys := make([]string, 0, len(statuses))
	for _, status := range statuses {
		keys = append(keys, status.Group+":"+status.ModelName)
	}
	return keys
}

func seedModelStatusTargets(t *testing.T, db *gorm.DB) {
	t.Helper()

	visibleChannel := model.Channel{Name: "visible", Key: "visible-key", Status: common.ChannelStatusEnabled}
	hiddenChannel := model.Channel{Name: "hidden", Key: "hidden-key", Status: common.ChannelStatusEnabled}
	missingChannel := model.Channel{Name: "missing", Key: "missing-key", Status: common.ChannelStatusEnabled}
	unlistedChannel := model.Channel{Name: "unlisted", Key: "unlisted-key", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&visibleChannel).Error)
	require.NoError(t, db.Create(&hiddenChannel).Error)
	require.NoError(t, db.Create(&missingChannel).Error)
	require.NoError(t, db.Create(&unlistedChannel).Error)

	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "visible", Model: "zz-visible-model", ChannelId: visibleChannel.Id, Enabled: true},
		{Group: "visible", Model: "zz-shared-model", ChannelId: visibleChannel.Id, Enabled: true},
		{Group: "hidden", Model: "zz-hidden-model", ChannelId: hiddenChannel.Id, Enabled: true},
		{Group: "hidden", Model: "zz-shared-model", ChannelId: hiddenChannel.Id, Enabled: true},
		{Group: "missing", Model: "zz-missing-model", ChannelId: missingChannel.Id, Enabled: true},
		{Group: "unlisted", Model: "zz-unlisted-model", ChannelId: unlistedChannel.Id, Enabled: true},
	}).Error)
}

func configureModelStatusIgnoredErrorKeywords(t *testing.T, enabled bool, keywords []string) {
	t.Helper()

	cfg := setting.GetEnhancementSetting()
	originalEnabled := cfg.ModelStatusIgnoreErrorKeywordsEnabled
	originalKeywords := append([]string{}, cfg.ModelStatusIgnoredErrorKeywords...)

	t.Cleanup(func() {
		cfg.ModelStatusIgnoreErrorKeywordsEnabled = originalEnabled
		cfg.ModelStatusIgnoredErrorKeywords = originalKeywords
	})

	cfg.ModelStatusIgnoreErrorKeywordsEnabled = enabled
	cfg.ModelStatusIgnoredErrorKeywords = append([]string{}, keywords...)
}

func seedModelStatusLogs(t *testing.T, db *gorm.DB, logs ...model.Log) {
	t.Helper()
	require.NoError(t, db.Create(&logs).Error)
}

func TestModelStatusIgnoredErrorKeywordsDisabledCountsErrors(t *testing.T) {
	db := setupModelStatusTestDB(t)
	configureModelStatusIgnoredErrorKeywords(t, false, []string{"unsupported_feature_for_model"})
	now := common.GetTimestamp()
	modelName := "zz-ignore-disabled-model"

	seedModelStatusLogs(t, db,
		model.Log{CreatedAt: now - 60, Type: model.LogTypeConsume, ModelName: modelName, Group: "default", UseTime: 2},
		model.Log{CreatedAt: now - 30, Type: model.LogTypeError, ModelName: modelName, Group: "default", Content: "unsupported_feature_for_model"},
	)

	status, err := ModelStatusForGroupWindow("default", modelName, ModelStatusWindow24h, false)
	require.NoError(t, err)
	require.Equal(t, int64(2), status.TotalRequests)
	require.Equal(t, int64(1), status.SuccessCount)
	require.Equal(t, int64(1), status.ErrorCount)
	require.Equal(t, 50.0, status.SuccessRate)
}

func TestModelStatusIgnoredErrorKeywordsMatchContentCaseInsensitive(t *testing.T) {
	db := setupModelStatusTestDB(t)
	configureModelStatusIgnoredErrorKeywords(t, true, []string{"UNSUPPORTED_FEATURE"})
	now := common.GetTimestamp()
	modelName := "zz-ignore-content-model"

	seedModelStatusLogs(t, db,
		model.Log{CreatedAt: now - 60, Type: model.LogTypeConsume, ModelName: modelName, Group: "default", UseTime: 2},
		model.Log{CreatedAt: now - 30, Type: model.LogTypeError, ModelName: modelName, Group: "default", Content: "unsupported_feature_for_model"},
	)

	status, err := ModelStatusForGroupWindow("default", modelName, ModelStatusWindow24h, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), status.TotalRequests)
	require.Equal(t, int64(1), status.SuccessCount)
	require.Equal(t, int64(0), status.ErrorCount)
	require.Equal(t, 100.0, status.SuccessRate)
}

func TestModelStatusIgnoredErrorKeywordsMatchOtherJSONText(t *testing.T) {
	db := setupModelStatusTestDB(t)
	configureModelStatusIgnoredErrorKeywords(t, true, []string{"content_policy_violation", `"status_code":400`})
	now := common.GetTimestamp()
	modelName := "zz-ignore-other-model"

	seedModelStatusLogs(t, db,
		model.Log{CreatedAt: now - 60, Type: model.LogTypeConsume, ModelName: modelName, Group: "default", UseTime: 2},
		model.Log{
			CreatedAt: now - 30,
			Type:      model.LogTypeError,
			ModelName: modelName,
			Group:     "default",
			Content:   "request rejected",
			Other:     `{"error_code":"content_policy_violation","status_code":400}`,
		},
	)

	status, err := ModelStatusForGroupWindow("default", modelName, ModelStatusWindow24h, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), status.TotalRequests)
	require.Equal(t, int64(1), status.SuccessCount)
	require.Equal(t, int64(0), status.ErrorCount)
	require.Equal(t, 100.0, status.SuccessRate)
}

func TestModelStatusIgnoredErrorKeywordsOnlyIgnoredErrorsLooksEmpty(t *testing.T) {
	db := setupModelStatusTestDB(t)
	configureModelStatusIgnoredErrorKeywords(t, true, []string{"invalid_parameter"})
	now := common.GetTimestamp()
	modelName := "zz-ignore-only-model"

	seedModelStatusLogs(t, db,
		model.Log{CreatedAt: now - 60, Type: model.LogTypeError, ModelName: modelName, Group: "default", Content: "invalid_parameter: bad value"},
		model.Log{CreatedAt: now - 30, Type: model.LogTypeError, ModelName: modelName, Group: "default", Other: `{"error_code":"invalid_parameter","status_code":400}`},
	)

	status, err := ModelStatusForGroupWindow("default", modelName, ModelStatusWindow24h, false)
	require.NoError(t, err)
	require.Equal(t, int64(0), status.TotalRequests)
	require.Equal(t, int64(0), status.SuccessCount)
	require.Equal(t, int64(0), status.ErrorCount)
	require.Equal(t, 100.0, status.SuccessRate)
	require.Equal(t, "green", status.CurrentStatus)
}

func TestNormalizeModelStatusIgnoredErrorKeywordsTrimsAndDeduplicates(t *testing.T) {
	keywords, err := parseModelStatusIgnoredErrorKeywords(" Foo \nfoo\n\nBAR\r\nbar ")
	require.NoError(t, err)
	require.Equal(t, []string{"Foo", "BAR"}, keywords)
}

func TestPublicModelStatusesFilterGroupsByMarketplaceDisplay(t *testing.T) {
	configurePublicModelStatusGroups(
		t,
		`{"visible":true,"hidden":false,"unlisted":true}`,
		`{"visible":"Visible group","hidden":"Hidden group"}`,
	)
	db := setupModelStatusTestDB(t)
	seedModelStatusTargets(t, db)

	statuses, err := ModelStatusesForWindow(nil, ModelStatusWindow24h, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"visible:zz-shared-model",
		"visible:zz-visible-model",
	}, statusKeys(statuses))

	models, err := AvailableModels(true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"zz-shared-model", "zz-visible-model"}, models)

	_, err = ModelStatusForGroupWindow("hidden", "zz-hidden-model", ModelStatusWindow24h, true)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	_, err = ModelStatusForGroupWindow("unlisted", "zz-unlisted-model", ModelStatusWindow24h, true)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestPublicModelStatusCacheVariesByGroupDisplay(t *testing.T) {
	configurePublicModelStatusGroups(
		t,
		`{"visible":true,"hidden":false}`,
		`{"visible":"Visible group","hidden":"Hidden group"}`,
	)
	db := setupModelStatusTestDB(t)
	seedModelStatusTargets(t, db)

	statuses, err := ModelStatusesForPublicConfig()
	require.NoError(t, err)
	require.NotContains(t, statusKeys(statuses), "hidden:zz-hidden-model")

	require.NoError(t, ratio_setting.UpdateGroupDisplayByJSONString(`{"visible":true,"hidden":true}`))

	statuses, err = ModelStatusesForPublicConfig()
	require.NoError(t, err)
	require.Contains(t, statusKeys(statuses), "hidden:zz-hidden-model")
}
