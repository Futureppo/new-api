package enhancement

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

	require.NoError(t, db.AutoMigrate(&model.RegistrationCode{}, &model.RegistrationCodeUsage{}, &model.Log{}))

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
		Count:   2,
		Name:    "launch",
		MaxUses: 3,
	}, 7)
	require.NoError(t, err)
	require.Len(t, generated, 2)
	require.NotEmpty(t, generated[0].Code)
	require.Equal(t, 3, generated[0].MaxUses)

	stats, err := RegistrationCodeStats()
	require.NoError(t, err)
	require.Equal(t, int64(2), stats["total"])
	require.Equal(t, int64(2), stats["enabled"])
	require.Equal(t, int64(0), stats["disabled"])
	require.Equal(t, int64(0), stats["used_count"])

	_, err = DisableRegistrationCode(generated[0].Id, 7)
	require.NoError(t, err)
	stats, err = RegistrationCodeStats()
	require.NoError(t, err)
	require.Equal(t, int64(1), stats["enabled"])
	require.Equal(t, int64(1), stats["disabled"])
}
