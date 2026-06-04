package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUserListTestDB(t *testing.T) *gorm.DB {
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

	require.NoError(t, db.AutoMigrate(&User{}))

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

func createUserListTestUser(t *testing.T, db *gorm.DB, username string, status int, quota int, reason string) User {
	t.Helper()

	user := User{
		Username:      username,
		Password:      "password",
		DisplayName:   username,
		Status:        status,
		Quota:         quota,
		Group:         "default",
		DisableReason: reason,
		AffCode:       username + "-aff",
	}
	require.NoError(t, db.Create(&user).Error)
	return user
}

func TestSearchUsersFiltersDisabledAndDeletedUsers(t *testing.T) {
	db := setupUserListTestDB(t)

	createUserListTestUser(t, db, "active-user", common.UserStatusEnabled, 300, "")
	disabled := createUserListTestUser(t, db, "disabled-user", common.UserStatusDisabled, 100, "manual review")
	deleted := createUserListTestUser(t, db, "deleted-user", common.UserStatusEnabled, 200, "")
	require.NoError(t, db.Delete(&deleted).Error)

	users, total, err := SearchUsers(UserListQuery{Status: "disabled"}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, users, 1)
	require.Equal(t, disabled.Id, users[0].Id)
	require.Equal(t, "manual review", users[0].DisableReason)

	users, total, err = SearchUsers(UserListQuery{Status: "deleted"}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, users, 1)
	require.Equal(t, deleted.Id, users[0].Id)
	require.True(t, users[0].DeletedAt.Valid)
}

func TestSearchUsersOrdersByQuota(t *testing.T) {
	db := setupUserListTestDB(t)

	createUserListTestUser(t, db, "quota-high", common.UserStatusEnabled, 300, "")
	createUserListTestUser(t, db, "quota-low", common.UserStatusEnabled, 100, "")
	createUserListTestUser(t, db, "quota-mid", common.UserStatusEnabled, 200, "")

	users, total, err := SearchUsers(UserListQuery{QuotaOrder: "asc"}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Equal(t, []int{100, 200, 300}, []int{users[0].Quota, users[1].Quota, users[2].Quota})

	users, total, err = SearchUsers(UserListQuery{QuotaOrder: "desc"}, 0, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Equal(t, []int{300, 200, 100}, []int{users[0].Quota, users[1].Quota, users[2].Quota})
}
