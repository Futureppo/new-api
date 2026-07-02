package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMissingModelsTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	oldDB := DB
	oldLogDB := LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldUsingMySQL := common.UsingMySQL
	oldUsingPostgreSQL := common.UsingPostgreSQL

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	LOG_DB = db
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()

	require.NoError(t, db.AutoMigrate(&Ability{}, &Model{}))

	t.Cleanup(func() {
		DB = oldDB
		LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.UsingMySQL = oldUsingMySQL
		common.UsingPostgreSQL = oldUsingPostgreSQL
		initCol()
	})

	return db
}

func TestGetMissingModelsHonorsModelNameRules(t *testing.T) {
	db := setupMissingModelsTestDB(t)

	require.NoError(t, db.Create(&[]Model{
		{ModelName: "exact-model", NameRule: NameRuleExact},
		{ModelName: "qwen3-", NameRule: NameRulePrefix},
		{ModelName: "-latest", NameRule: NameRuleSuffix},
		{ModelName: "image", NameRule: NameRuleContains},
	}).Error)

	deletedRule := Model{ModelName: "deleted-", NameRule: NameRulePrefix}
	require.NoError(t, db.Create(&deletedRule).Error)
	require.NoError(t, db.Delete(&deletedRule).Error)

	require.NoError(t, db.Create(&[]Ability{
		{Group: "default", Model: "exact-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "qwen3-reranker-8b", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "custom-model-latest", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "custom-image-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "unmatched-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "deleted-rule-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "disabled-unmatched-model", ChannelId: 1, Enabled: false},
	}).Error)

	missing, err := GetMissingModels()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"unmatched-model", "deleted-rule-model"}, missing)
}
