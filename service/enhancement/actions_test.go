package enhancement

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserPurgeTestDB(t *testing.T) {
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

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

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
}

func setupRiskActionTestDB(t *testing.T) {
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

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.IPBan{}, &model.Log{}))

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
		model.InitIPBanCache()
	})
}

func setupModelStatusOptionTestDB(t *testing.T) {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalOptionMap := common.OptionMap

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.OptionMap = map[string]string{}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.Option{}))

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
		common.OptionMap = originalOptionMap
	})
}

func seedPurgeUser(t *testing.T, id int, role int, status int, softDeleted bool) {
	t.Helper()

	user := model.User{
		Id:       id,
		Username: fmt.Sprintf("purge_user_%d", id),
		Password: "password",
		Role:     role,
		Status:   status,
		AffCode:  fmt.Sprintf("aff_%d", id),
	}
	require.NoError(t, model.DB.Create(&user).Error)
	if softDeleted {
		require.NoError(t, model.DB.Delete(&user).Error)
	}
}

func seedRiskUser(t *testing.T, username string) model.User {
	t.Helper()

	user := model.User{
		Username: username,
		Password: "password",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		AffCode:  "aff_" + username,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func seedRiskToken(t *testing.T, userId int, name string) model.Token {
	t.Helper()

	token := model.Token{
		UserId:      userId,
		Name:        name,
		Key:         "sk-" + name,
		Status:      common.TokenStatusEnabled,
		RemainQuota: 100,
	}
	require.NoError(t, model.DB.Create(&token).Error)
	return token
}

func requirePurgeUserExists(t *testing.T, id int, expected bool) {
	t.Helper()

	var count int64
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).Where("id = ?", id).Count(&count).Error)
	if expected {
		require.Equal(t, int64(1), count)
		return
	}
	require.Equal(t, int64(0), count)
}

func TestSaveModelStatusRequestCountHideThreshold(t *testing.T) {
	setupModelStatusOptionTestDB(t)

	cfg := setting.GetEnhancementSetting()
	originalThreshold := cfg.ModelStatusRequestCountHideThreshold
	t.Cleanup(func() {
		cfg.ModelStatusRequestCountHideThreshold = originalThreshold
		ClearModelStatusPublicCache()
	})

	require.NoError(t, SaveModelStatusOption("model_status_request_count_hide_threshold", "12", 1))
	require.Equal(t, 12, cfg.ModelStatusRequestCountHideThreshold)

	var option model.Option
	require.NoError(t, model.DB.Where("key = ?", "enhancement_setting.model_status_request_count_hide_threshold").First(&option).Error)
	require.Equal(t, "12", option.Value)
}

func TestSaveModelStatusRequestCountHideThresholdRejectsInvalidValues(t *testing.T) {
	setupModelStatusOptionTestDB(t)

	for _, value := range []string{"-1", "1000001", "1.5", "true", "abc"} {
		err := SaveModelStatusOption("model_status_request_count_hide_threshold", value, 1)
		require.Error(t, err, "value %q should be rejected", value)
	}
}

func TestPurgeSoftDeletedUsersAdminDeletesOnlyCommonUsers(t *testing.T) {
	setupUserPurgeTestDB(t)
	seedPurgeUser(t, 101, common.RoleCommonUser, common.UserStatusEnabled, true)
	seedPurgeUser(t, 102, common.RoleAdminUser, common.UserStatusEnabled, true)
	seedPurgeUser(t, 103, common.RoleRootUser, common.UserStatusEnabled, true)
	seedPurgeUser(t, 104, common.RoleCommonUser, common.UserStatusDisabled, false)

	deleted, err := PurgeSoftDeletedUsers(900, common.RoleAdminUser)

	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	requirePurgeUserExists(t, 101, false)
	requirePurgeUserExists(t, 102, true)
	requirePurgeUserExists(t, 103, true)
	requirePurgeUserExists(t, 104, true)
}

func TestPurgeSoftDeletedUsersRootDeletesCommonAndAdminUsers(t *testing.T) {
	setupUserPurgeTestDB(t)
	seedPurgeUser(t, 201, common.RoleCommonUser, common.UserStatusEnabled, true)
	seedPurgeUser(t, 202, common.RoleAdminUser, common.UserStatusEnabled, true)
	seedPurgeUser(t, 203, common.RoleRootUser, common.UserStatusEnabled, true)
	seedPurgeUser(t, 204, common.RoleCommonUser, common.UserStatusDisabled, false)

	deleted, err := PurgeSoftDeletedUsers(900, common.RoleRootUser)

	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	requirePurgeUserExists(t, 201, false)
	requirePurgeUserExists(t, 202, false)
	requirePurgeUserExists(t, 203, true)
	requirePurgeUserExists(t, 204, true)
}

func TestCreateRiskIPBansCreatesPermanentAutoBanAndSkipsExisting(t *testing.T) {
	setupRiskActionTestDB(t)
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target: "203.0.113.20",
		Reason: "existing",
	}))

	result, err := CreateRiskIPBans(RiskIPBanRequest{
		Targets: []string{"203.0.113.10", "203.0.113.10", "203.0.113.20"},
		Reason:  "risk reason",
	}, 900)

	require.NoError(t, err)
	require.Equal(t, 1, result["created"])
	require.Equal(t, 1, result["skipped"])

	var ban model.IPBan
	require.NoError(t, model.DB.First(&ban, "target = ?", "203.0.113.10").Error)
	require.Equal(t, "risk reason", ban.Reason)
	require.Zero(t, ban.ExpiresAt)
	require.True(t, ban.AutoBanUser)
	require.Equal(t, 900, ban.CreatedBy)

	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&logCount).Error)
	require.GreaterOrEqual(t, logCount, int64(1))
}

func TestCreateRiskIPBansRejectsEmptyAndTooManyTargets(t *testing.T) {
	setupRiskActionTestDB(t)

	_, err := CreateRiskIPBans(RiskIPBanRequest{}, 900)
	require.Error(t, err)

	targets := make([]string, 0, MaxBatchOperation+1)
	for i := 1; i <= MaxBatchOperation+1; i++ {
		targets = append(targets, fmt.Sprintf("203.0.113.%d", i))
	}
	_, err = CreateRiskIPBans(RiskIPBanRequest{Targets: targets, Reason: "risk"}, 900)
	require.Error(t, err)
	require.Contains(t, err.Error(), "limit")
}

func TestBanSharedTokenIPUsersLimitsToSelectedIntersectedUsers(t *testing.T) {
	setupRiskActionTestDB(t)
	first := seedRiskUser(t, "risk-first")
	second := seedRiskUser(t, "risk-second")
	third := seedRiskUser(t, "risk-third")
	firstToken := seedRiskToken(t, first.Id, "first-token")
	secondToken := seedRiskToken(t, second.Id, "second-token")
	thirdToken := seedRiskToken(t, third.Id, "third-token")
	now := common.GetTimestamp()
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{
		{UserId: first.Id, Username: first.Username, TokenId: firstToken.Id, TokenName: firstToken.Name, Type: model.LogTypeConsume, CreatedAt: now - 30, Ip: "203.0.113.7"},
		{UserId: second.Id, Username: second.Username, TokenId: secondToken.Id, TokenName: secondToken.Name, Type: model.LogTypeConsume, CreatedAt: now - 20, Ip: "203.0.113.7"},
		{UserId: third.Id, Username: third.Username, TokenId: thirdToken.Id, TokenName: thirdToken.Name, Type: model.LogTypeConsume, CreatedAt: now - 10, Ip: "203.0.113.8"},
	}).Error)
	selected := []int{second.Id, third.Id, 99999}

	result, err := BanSharedTokenIPUsers("203.0.113.7", IPRiskQuery{Start: now - 60, End: now + 1}, 900, common.RoleRootUser, "selected risk", &selected)

	require.NoError(t, err)
	require.Equal(t, 1, result["success"])
	require.Equal(t, 1, result["total_users"])

	var users []model.User
	require.NoError(t, model.DB.Order("id asc").Find(&users, "id IN ?", []int{first.Id, second.Id, third.Id}).Error)
	require.Equal(t, common.UserStatusEnabled, users[0].Status)
	require.Equal(t, common.UserStatusDisabled, users[1].Status)
	require.Equal(t, "selected risk", users[1].DisableReason)
	require.Equal(t, common.UserStatusEnabled, users[2].Status)
}

func TestBanSharedTokenIPUsersWithoutSelectionKeepsExistingAllUsersBehavior(t *testing.T) {
	setupRiskActionTestDB(t)
	first := seedRiskUser(t, "risk-all-first")
	second := seedRiskUser(t, "risk-all-second")
	firstToken := seedRiskToken(t, first.Id, "all-first-token")
	secondToken := seedRiskToken(t, second.Id, "all-second-token")
	now := common.GetTimestamp()
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{
		{UserId: first.Id, Username: first.Username, TokenId: firstToken.Id, TokenName: firstToken.Name, Type: model.LogTypeConsume, CreatedAt: now - 30, Ip: "203.0.113.9"},
		{UserId: second.Id, Username: second.Username, TokenId: secondToken.Id, TokenName: secondToken.Name, Type: model.LogTypeConsume, CreatedAt: now - 20, Ip: "203.0.113.9"},
	}).Error)

	result, err := BanSharedTokenIPUsers("203.0.113.9", IPRiskQuery{Start: now - 60, End: now + 1}, 900, common.RoleRootUser, "all risk", nil)

	require.NoError(t, err)
	require.Equal(t, 2, result["success"])
	require.Equal(t, 2, result["total_users"])

	var disabled int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("id IN ? AND status = ?", []int{first.Id, second.Id}, common.UserStatusDisabled).Count(&disabled).Error)
	require.Equal(t, int64(2), disabled)
}
