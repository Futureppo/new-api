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
