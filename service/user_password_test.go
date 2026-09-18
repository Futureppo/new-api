package service

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestConcurrentFirstPasswordUpdate(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)
	user := model.User{Username: "concurrent-password", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	start := make(chan struct{})
	results := make(chan bool, 2)
	errors := make(chan error, 2)
	var wg sync.WaitGroup
	for _, candidate := range []string{"first-hash", "second-hash"} {
		wg.Add(1)
		go func(hash string) {
			defer wg.Done()
			<-start
			updated, err := model.UpdateUserPasswordIfUnchanged(user.Id, "", hash)
			results <- updated
			errors <- err
		}(candidate)
	}
	close(start)
	wg.Wait()
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	require.NotEqual(t, <-results, <-results, "exactly one concurrent first-time update must succeed")
}

func TestSetUserLoginPassword(t *testing.T) {
	db := setupUserInviteDisableTestDB(t)
	user := model.User{Username: "password-user", DisplayName: "unchanged", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	has, err := model.HasUserPassword(user.Id)
	require.NoError(t, err)
	require.False(t, has)
	require.ErrorIs(t, SetUserLoginPassword(user.Id, "", "password123"), common.ErrPasswordPolicy)
	require.NoError(t, SetUserLoginPassword(user.Id, "", "Password123!"))
	has, err = model.HasUserPassword(user.Id)
	require.NoError(t, err)
	require.True(t, has)
	require.ErrorIs(t, SetUserLoginPassword(user.Id, "", "Another123!"), ErrOriginalPassword)
	require.ErrorIs(t, SetUserLoginPassword(user.Id, "wrong", "Another123!"), ErrOriginalPassword)
	require.ErrorIs(t, SetUserLoginPassword(user.Id, "Password123!", "Password123!"), ErrPasswordUnchanged)
	require.NoError(t, SetUserLoginPassword(user.Id, "Password123!", "Another123!"))
	stored, err := model.GetUserById(user.Id, true)
	require.NoError(t, err)
	require.True(t, common.ValidatePasswordAndHash("Another123!", stored.Password))
	require.Equal(t, "unchanged", stored.DisplayName)
	// An update using the empty password observed by another setup request fails.
	updated, err := model.UpdateUserPasswordIfUnchanged(user.Id, "", "obsolete")
	require.NoError(t, err)
	require.False(t, updated)
	// Old weak passwords remain valid credentials when choosing a strong one.
	weakHash, err := common.Password2Hash("weak")
	require.NoError(t, err)
	require.NoError(t, db.Model(&user).Update("password", weakHash).Error)
	require.NoError(t, SetUserLoginPassword(user.Id, "weak", "Strong123!"))
}
