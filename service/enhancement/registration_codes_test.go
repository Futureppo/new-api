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

func setupRegistrationCodeServiceTestDB(t *testing.T) {
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

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.RegistrationCode{}, &model.RegistrationCodeUsage{}, &model.Log{}, &model.Option{}))

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

func TestGenerateRegistrationCodesAndStats(t *testing.T) {
	setupRegistrationCodeServiceTestDB(t)

	generated, err := GenerateRegistrationCodes(GenerateRegistrationCodesRequest{
		Name:    "launch",
		MaxUses: 3,
		EndTime: common.GetTimestamp() + 3600,
	}, 7)
	require.NoError(t, err)
	require.Len(t, generated, 1)
	require.NotEmpty(t, generated[0].Code)
	require.Equal(t, 3, generated[0].MaxUses)
	require.Greater(t, generated[0].EndTime, int64(0))

	stats, err := RegistrationCodeStats()
	require.NoError(t, err)
	require.Equal(t, int64(1), stats["total"])
	require.Equal(t, int64(1), stats["enabled"])
	require.Equal(t, int64(0), stats["disabled"])
	require.Equal(t, int64(0), stats["used_count"])

	_, err = DisableRegistrationCode(generated[0].Id, 7)
	require.NoError(t, err)
	stats, err = RegistrationCodeStats()
	require.NoError(t, err)
	require.Equal(t, int64(0), stats["enabled"])
	require.Equal(t, int64(1), stats["disabled"])
}

func TestGenerateRegistrationCodesSupportsBatchCount(t *testing.T) {
	setupRegistrationCodeServiceTestDB(t)

	generated, err := GenerateRegistrationCodes(GenerateRegistrationCodesRequest{
		Count:   2,
		Name:    "batch",
		MaxUses: 10,
	}, 7)
	require.NoError(t, err)
	require.Len(t, generated, 2)
	require.NotEqual(t, generated[0].Code, generated[1].Code)
	for _, code := range generated {
		require.NotEmpty(t, code.Code)
		require.Equal(t, "batch", code.Name)
		require.Equal(t, 10, code.MaxUses)
	}

	var count int64
	require.NoError(t, model.DB.Model(&model.RegistrationCode{}).Count(&count).Error)
	require.Equal(t, int64(2), count)
}

func TestGenerateRegistrationCodesRejectsCustomCodeForBatch(t *testing.T) {
	setupRegistrationCodeServiceTestDB(t)

	_, err := GenerateRegistrationCodes(GenerateRegistrationCodesRequest{
		Count:   2,
		Name:    "batch",
		MaxUses: 10,
		Code:    "CUSTOM",
	}, 7)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom code")
}

func TestSaveRegistrationCodeConfigIncludesInviteCodeRequirement(t *testing.T) {
	setupRegistrationCodeServiceTestDB(t)

	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	cfg := setting.GetEnhancementSetting()
	originalRegistrationCodeRequired := cfg.RegistrationCodeRequired
	originalInviteCodeRequired := cfg.InviteCodeRequired
	t.Cleanup(func() {
		common.OptionMap = originalOptionMap
		cfg.RegistrationCodeRequired = originalRegistrationCodeRequired
		cfg.InviteCodeRequired = originalInviteCodeRequired
	})

	inviteCodeRequired := true
	err := SaveRegistrationCodeConfig(RegistrationCodeConfigRequest{
		RegistrationCodeRequired: true,
		InviteCodeRequired:       &inviteCodeRequired,
	}, 7)
	require.NoError(t, err)
	require.True(t, setting.IsRegistrationCodeRequired())
	require.True(t, setting.IsInviteCodeRequired())

	config := RegistrationCodeConfig()
	require.Equal(t, true, config["registration_code_required"])
	require.Equal(t, true, config["invite_code_required"])

	var option model.Option
	require.NoError(t, model.DB.Where("key = ?", "enhancement_setting.invite_code_required").First(&option).Error)
	require.Equal(t, "true", option.Value)

	require.NoError(t, SaveRegistrationCodeConfig(RegistrationCodeConfigRequest{
		RegistrationCodeRequired: false,
	}, 7))
	require.True(t, setting.IsInviteCodeRequired())
}
