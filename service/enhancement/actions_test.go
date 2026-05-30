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
