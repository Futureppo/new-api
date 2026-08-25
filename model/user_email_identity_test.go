package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createEmailIdentityTestUser(t *testing.T, username string, email string, password string) User {
	t.Helper()
	hashedPassword, err := common.Password2Hash(password)
	require.NoError(t, err)
	user := User{
		Username:    username,
		Password:    hashedPassword,
		DisplayName: username,
		Email:       email,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     username + "-aff",
	}
	require.NoError(t, DB.Create(&user).Error)
	return user
}

func TestEmailIdentityLookupHonorsCaseInsensitiveOption(t *testing.T) {
	truncateTables(t)
	originalSetting := common.EmailCaseInsensitiveEnabled
	t.Cleanup(func() { common.EmailCaseInsensitiveEnabled = originalSetting })

	common.EmailCaseInsensitiveEnabled = true
	stored := createEmailIdentityTestUser(t, "case-user", "Likwei@My.SWJTU.edu.cn", "password123")
	deleted := createEmailIdentityTestUser(t, "deleted-user", "Deleted@Example.com", "password123")
	require.NoError(t, DB.Delete(&deleted).Error)

	taken, err := EmailIdentityExists(DB, "likwei@my.swjtu.EDU.CN", 0, true)
	require.NoError(t, err)
	require.True(t, taken)

	matched, err := FindUniqueUserByEmail("LIKWEI@MY.SWJTU.EDU.CN")
	require.NoError(t, err)
	require.Equal(t, stored.Id, matched.Id)
	require.Equal(t, "Likwei@My.SWJTU.edu.cn", matched.Email)
	taken, err = EmailIdentityExists(DB, stored.Email, stored.Id, true)
	require.NoError(t, err)
	require.False(t, taken)
	require.True(t, IsEmailAlreadyTaken("deleted@example.com"))
	_, err = FindUniqueUserByEmail("deleted@example.com")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	taken, err = EmailIdentityExists(DB, "likwei@other.example", 0, true)
	require.NoError(t, err)
	require.False(t, taken)

	common.EmailCaseInsensitiveEnabled = false
	_, err = FindUniqueUserByEmail("likwei@my.swjtu.edu.cn")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	matched, err = FindUniqueUserByEmail("Likwei@My.SWJTU.edu.cn")
	require.NoError(t, err)
	require.Equal(t, stored.Id, matched.Id)

	taken, err = EmailIdentityExists(DB, "likwei@my.swjtu.edu.cn", 0, true)
	require.NoError(t, err)
	require.False(t, taken)
	exists, err := CheckUserExistOrDeleted("unused-username", "likwei@my.swjtu.edu.cn")
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = CheckUserExistOrDeleted("unused-username", stored.Email)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestEmailIdentityAmbiguityBlocksEmailLoginAndPasswordReset(t *testing.T) {
	truncateTables(t)
	originalSetting := common.EmailCaseInsensitiveEnabled
	common.EmailCaseInsensitiveEnabled = true
	t.Cleanup(func() { common.EmailCaseInsensitiveEnabled = originalSetting })

	first := createEmailIdentityTestUser(t, "first-user", "likwei@example.com", "password123")
	second := createEmailIdentityTestUser(t, "second-user", "Likwei@example.com", "password123")

	_, err := FindUniqueUserByEmail("LIKWEI@EXAMPLE.COM")
	require.ErrorIs(t, err, ErrEmailIdentityAmbiguous)

	emailLogin := User{Username: "LIKWEI@example.com", Password: "password123"}
	require.ErrorIs(t, emailLogin.ValidateAndFill(), ErrEmailIdentityAmbiguous)

	usernameLogin := User{Username: first.Username, Password: "password123"}
	require.NoError(t, usernameLogin.ValidateAndFill())
	require.Equal(t, first.Id, usernameLogin.Id)

	err = ResetUserPasswordByEmail("likwei@EXAMPLE.com", "new-password123")
	require.ErrorIs(t, err, ErrEmailIdentityAmbiguous)

	var reloaded []User
	require.NoError(t, DB.Where("id IN ?", []int{first.Id, second.Id}).Order("id ASC").Find(&reloaded).Error)
	require.Len(t, reloaded, 2)
	for _, user := range reloaded {
		require.True(t, common.ValidatePasswordAndHash("password123", user.Password))
		require.False(t, common.ValidatePasswordAndHash("new-password123", user.Password))
	}
}

func TestResetUserPasswordByEmailTargetsUniqueCaseInsensitiveMatch(t *testing.T) {
	truncateTables(t)
	originalSetting := common.EmailCaseInsensitiveEnabled
	common.EmailCaseInsensitiveEnabled = true
	t.Cleanup(func() { common.EmailCaseInsensitiveEnabled = originalSetting })

	user := createEmailIdentityTestUser(t, "reset-user", "Reset.Me@Example.com", "password123")
	require.NoError(t, ResetUserPasswordByEmail("reset.me@example.COM", "new-password123"))

	var reloaded User
	require.NoError(t, DB.First(&reloaded, user.Id).Error)
	require.True(t, common.ValidatePasswordAndHash("new-password123", reloaded.Password))
}
