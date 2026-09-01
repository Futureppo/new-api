package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type registrationAPIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type inviteTestSession struct {
	values map[interface{}]interface{}
}

func newInviteTestSession(values map[interface{}]interface{}) *inviteTestSession {
	if values == nil {
		values = map[interface{}]interface{}{}
	}
	return &inviteTestSession{values: values}
}

func (s *inviteTestSession) ID() string {
	return "test"
}

func (s *inviteTestSession) Get(key interface{}) interface{} {
	return s.values[key]
}

func (s *inviteTestSession) Set(key interface{}, value interface{}) {
	s.values[key] = value
}

func (s *inviteTestSession) Delete(key interface{}) {
	delete(s.values, key)
}

func (s *inviteTestSession) Clear() {
	clear(s.values)
}

func (s *inviteTestSession) AddFlash(value interface{}, vars ...string) {}

func (s *inviteTestSession) Flashes(vars ...string) []interface{} {
	return nil
}

func (s *inviteTestSession) Options(options sessions.Options) {}

func (s *inviteTestSession) Save() error {
	return nil
}

func setupInviteRegistrationControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalRegisterEnabled := common.RegisterEnabled
	originalPasswordRegisterEnabled := common.PasswordRegisterEnabled
	originalEmailVerificationEnabled := common.EmailVerificationEnabled
	originalEmailCaseInsensitiveEnabled := common.EmailCaseInsensitiveEnabled
	originalDomainEmailRegistrationEnabled := common.DomainEmailRegistrationEnabled
	originalDomainEmailRegistrationWhitelist := append([]string(nil), common.DomainEmailRegistrationWhitelist...)
	originalEmailDomainBlacklist := append([]string(nil), common.EmailDomainBlacklist...)
	originalGenerateDefaultToken := constant.GenerateDefaultToken
	originalQuotaForNewUser := common.QuotaForNewUser
	originalQuotaForInviter := common.QuotaForInviter
	originalQuotaForInvitee := common.QuotaForInvitee
	cfg := setting.GetEnhancementSetting()
	originalInviteCodeRequired := cfg.InviteCodeRequired
	originalRegistrationCodeRequired := cfg.RegistrationCodeRequired

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false
	common.EmailCaseInsensitiveEnabled = true
	common.DomainEmailRegistrationEnabled = false
	common.DomainEmailRegistrationWhitelist = nil
	common.EmailDomainBlacklist = nil
	constant.GenerateDefaultToken = false
	common.QuotaForNewUser = 0
	common.QuotaForInviter = 0
	common.QuotaForInvitee = 0

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.RegistrationCode{}, &model.RegistrationCodeUsage{}, &model.Log{}))

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
		common.RegisterEnabled = originalRegisterEnabled
		common.PasswordRegisterEnabled = originalPasswordRegisterEnabled
		common.EmailVerificationEnabled = originalEmailVerificationEnabled
		common.EmailCaseInsensitiveEnabled = originalEmailCaseInsensitiveEnabled
		common.DomainEmailRegistrationEnabled = originalDomainEmailRegistrationEnabled
		common.DomainEmailRegistrationWhitelist = originalDomainEmailRegistrationWhitelist
		common.EmailDomainBlacklist = originalEmailDomainBlacklist
		constant.GenerateDefaultToken = originalGenerateDefaultToken
		common.QuotaForNewUser = originalQuotaForNewUser
		common.QuotaForInviter = originalQuotaForInviter
		common.QuotaForInvitee = originalQuotaForInvitee
		cfg.InviteCodeRequired = originalInviteCodeRequired
		cfg.RegistrationCodeRequired = originalRegistrationCodeRequired
	})

	return db
}

func registerTestUser(t *testing.T, payload gin.H) registrationAPIResponse {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	Register(ctx)

	var response registrationAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func requireRegistrationUserMissing(t *testing.T, db *gorm.DB, username string) {
	t.Helper()
	var count int64
	require.NoError(t, db.Unscoped().Model(&model.User{}).Where("username = ?", username).Count(&count).Error)
	require.Zero(t, count)
}

func TestPasswordRegistrationRequiresValidInviteCodeAndRollsBack(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = false

	inviter := model.User{
		Username:    "inviter",
		Password:    "password123",
		DisplayName: "inviter",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     "VALID-AFF",
	}
	require.NoError(t, db.Create(&inviter).Error)

	missingInvite := registerTestUser(t, gin.H{
		"username": "missing-invite",
		"password": "password123",
	})
	require.False(t, missingInvite.Success)
	require.Contains(t, missingInvite.Message, "请输入邀请码")
	requireRegistrationUserMissing(t, db, "missing-invite")

	invalidInvite := registerTestUser(t, gin.H{
		"username": "invalid-invite",
		"password": "password123",
		"aff_code": "UNKNOWN",
	})
	require.False(t, invalidInvite.Success)
	require.Contains(t, invalidInvite.Message, "邀请码无效")
	requireRegistrationUserMissing(t, db, "invalid-invite")

	require.NoError(t, db.Model(&inviter).Update("status", common.UserStatusDisabled).Error)
	disabledInviter := registerTestUser(t, gin.H{
		"username": "disabled-inviter",
		"password": "password123",
		"aff_code": inviter.AffCode,
	})
	require.False(t, disabledInviter.Success)
	require.Contains(t, disabledInviter.Message, "邀请码无效")
	requireRegistrationUserMissing(t, db, "disabled-inviter")
	require.NoError(t, db.Model(&inviter).Update("status", common.UserStatusEnabled).Error)

	validInvite := registerTestUser(t, gin.H{
		"username": "valid-invite",
		"password": "password123",
		"aff_code": inviter.AffCode,
	})
	require.True(t, validInvite.Success)
	var invitedUser model.User
	require.NoError(t, db.Where("username = ?", "valid-invite").First(&invitedUser).Error)
	require.Equal(t, inviter.Id, invitedUser.InviterId)

	cfg.RegistrationCodeRequired = true
	registrationCode := model.RegistrationCode{
		Code:        "VALID-REGISTRATION",
		Status:      common.RegistrationCodeStatusEnabled,
		Name:        "test",
		MaxUses:     1,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, db.Create(&registrationCode).Error)

	missingRegistrationCode := registerTestUser(t, gin.H{
		"username": "missing-reg-code",
		"password": "password123",
		"aff_code": inviter.AffCode,
	})
	require.False(t, missingRegistrationCode.Success)
	require.Contains(t, missingRegistrationCode.Message, "请输入注册码")
	requireRegistrationUserMissing(t, db, "missing-reg-code")

	validCodes := registerTestUser(t, gin.H{
		"username":          "valid-codes",
		"password":          "password123",
		"aff_code":          inviter.AffCode,
		"registration_code": registrationCode.Code,
	})
	require.True(t, validCodes.Success)
	var registeredUser model.User
	require.NoError(t, db.Where("username = ?", "valid-codes").First(&registeredUser).Error)
	require.Equal(t, inviter.Id, registeredUser.InviterId)
	require.NoError(t, db.First(&registrationCode, registrationCode.Id).Error)
	require.Equal(t, 1, registrationCode.UsedCount)
}

func TestDomainEmailRegistrationBypassesInviteAndRegistrationCodes(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = true
	common.EmailVerificationEnabled = true
	common.DomainEmailRegistrationEnabled = true
	common.DomainEmailRegistrationWhitelist = []string{"*.trusted.test"}
	common.EmailDomainBlacklist = []string{"*.blocked.trusted.test"}

	const verificationCode = "123456"
	domainEmail := "user@mail.trusted.test"
	common.RegisterVerificationCodeWithKey(
		common.NormalizeEmailIdentity(domainEmail),
		verificationCode,
		common.EmailVerificationPurpose,
	)
	t.Cleanup(func() {
		common.DeleteKey(common.NormalizeEmailIdentity(domainEmail), common.EmailVerificationPurpose)
	})

	response := registerTestUser(t, gin.H{
		"username":          "domain-user",
		"password":          "password123",
		"email":             domainEmail,
		"verification_code": verificationCode,
	})
	require.True(t, response.Success, response.Message)

	var user model.User
	require.NoError(t, db.Where("username = ?", "domain-user").First(&user).Error)
	require.Zero(t, user.InviterId)
	require.Equal(t, domainEmail, user.Email)

	var usages int64
	require.NoError(t, db.Model(&model.RegistrationCodeUsage{}).Count(&usages).Error)
	require.Zero(t, usages)
}

func TestDomainEmailRegistrationRejectsBlacklistedDomain(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = true
	common.EmailVerificationEnabled = true
	common.DomainEmailRegistrationEnabled = true
	common.DomainEmailRegistrationWhitelist = []string{"*.trusted.test"}
	common.EmailDomainBlacklist = []string{"*.blocked.trusted.test"}

	const verificationCode = "123456"
	blacklistedEmail := "user@mail.blocked.trusted.test"
	common.RegisterVerificationCodeWithKey(
		common.NormalizeEmailIdentity(blacklistedEmail),
		verificationCode,
		common.EmailVerificationPurpose,
	)
	t.Cleanup(func() {
		common.DeleteKey(common.NormalizeEmailIdentity(blacklistedEmail), common.EmailVerificationPurpose)
	})

	response := registerTestUser(t, gin.H{
		"username":          "blocked-user",
		"password":          "password123",
		"email":             blacklistedEmail,
		"verification_code": verificationCode,
	})
	require.False(t, response.Success)
	require.Equal(t, i18n.MsgUserEmailDomainBlacklisted, response.Message)
	requireRegistrationUserMissing(t, db, "blocked-user")
}

func TestUnconfiguredDomainEmailStillRequiresInviteCode(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = true
	common.EmailVerificationEnabled = true
	common.DomainEmailRegistrationEnabled = true
	common.DomainEmailRegistrationWhitelist = []string{"*.trusted.test"}

	const verificationCode = "123456"
	unconfiguredEmail := "user@example.com"
	common.RegisterVerificationCodeWithKey(
		common.NormalizeEmailIdentity(unconfiguredEmail),
		verificationCode,
		common.EmailVerificationPurpose,
	)
	t.Cleanup(func() {
		common.DeleteKey(common.NormalizeEmailIdentity(unconfiguredEmail), common.EmailVerificationPurpose)
	})

	response := registerTestUser(t, gin.H{
		"username":          "unconfigured-user",
		"password":          "password123",
		"email":             unconfiguredEmail,
		"verification_code": verificationCode,
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "请输入邀请码")
	requireRegistrationUserMissing(t, db, "unconfigured-user")
}

func TestUnverifiedDomainEmailCannotBypassRegistrationCodes(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = true
	common.EmailVerificationEnabled = false
	common.DomainEmailRegistrationEnabled = true
	common.DomainEmailRegistrationWhitelist = []string{"*.trusted.test"}

	response := registerTestUser(t, gin.H{
		"username": "unverified-user",
		"password": "password123",
		"email":    "user@mail.trusted.test",
	})
	require.False(t, response.Success)
	require.Contains(t, response.Message, "请输入邀请码")
	requireRegistrationUserMissing(t, db, "unverified-user")
}

func TestPasswordRegistrationTreatsEmailCaseVariantsAsOneIdentity(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = false
	cfg.RegistrationCodeRequired = false
	common.EmailVerificationEnabled = true
	common.EmailCaseInsensitiveEnabled = true

	const verificationCode = "123456"
	firstEmail := "Likwei@My.SWJTU.edu.cn"
	common.RegisterVerificationCodeWithKey(
		common.NormalizeEmailIdentity(firstEmail),
		verificationCode,
		common.EmailVerificationPurpose,
	)
	t.Cleanup(func() {
		common.DeleteKey(common.NormalizeEmailIdentity(firstEmail), common.EmailVerificationPurpose)
	})

	firstResponse := registerTestUser(t, gin.H{
		"username":          "email-case-first",
		"password":          "password123",
		"email":             firstEmail,
		"verification_code": verificationCode,
	})
	require.True(t, firstResponse.Success)

	var firstUser model.User
	require.NoError(t, db.Where("username = ?", "email-case-first").First(&firstUser).Error)
	require.Equal(t, firstEmail, firstUser.Email)

	caseVariant := "likwei@my.swjtu.EDU.CN"
	common.RegisterVerificationCodeWithKey(
		common.NormalizeEmailIdentity(caseVariant),
		verificationCode,
		common.EmailVerificationPurpose,
	)
	secondResponse := registerTestUser(t, gin.H{
		"username":          "email-case-second",
		"password":          "password123",
		"email":             caseVariant,
		"verification_code": verificationCode,
	})
	require.False(t, secondResponse.Success)
	requireRegistrationUserMissing(t, db, "email-case-second")
}

func TestOAuthRegistrationRequiresInviteCodeButExistingLoginDoesNot(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = false

	inviter := model.User{
		Username:    "oauth-inviter",
		Password:    "password123",
		DisplayName: "oauth-inviter",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     "OAUTH-AFF",
	}
	require.NoError(t, db.Create(&inviter).Error)

	provider := &oauth.DiscordProvider{}
	oauthUser := &oauth.OAuthUser{
		ProviderUserID: "discord-new-user",
		Username:       "oauth-new-user",
		DisplayName:    "OAuth New User",
	}

	_, err := findOrCreateOAuthUser(nil, provider, oauthUser, newInviteTestSession(nil))
	require.EqualError(t, err, "请输入邀请码")
	requireRegistrationUserMissing(t, db, oauthUser.Username)

	createdUser, err := findOrCreateOAuthUser(nil, provider, oauthUser, newInviteTestSession(map[interface{}]interface{}{
		"aff": inviter.AffCode,
	}))
	require.NoError(t, err)
	require.Equal(t, inviter.Id, createdUser.InviterId)
	require.Equal(t, oauthUser.ProviderUserID, createdUser.DiscordId)

	existingUser, err := findOrCreateOAuthUser(nil, provider, oauthUser, newInviteTestSession(nil))
	require.NoError(t, err)
	require.Equal(t, createdUser.Id, existingUser.Id)
}

func TestOAuthRegistrationRejectsEmailAlreadyUsedByCaseVariant(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = false
	cfg.RegistrationCodeRequired = false
	common.EmailCaseInsensitiveEnabled = true

	existing := model.User{
		Username:    "oauth-email-owner",
		Password:    "password123",
		DisplayName: "OAuth Email Owner",
		Email:       "Owner@Example.com",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     "OAUTH-EMAIL-OWNER",
	}
	require.NoError(t, db.Create(&existing).Error)

	provider := &oauth.DiscordProvider{}
	oauthUser := &oauth.OAuthUser{
		ProviderUserID: "discord-email-duplicate",
		Username:       "oauth-email-duplicate",
		DisplayName:    "OAuth Email Duplicate",
		Email:          "owner@example.COM",
	}

	_, err := findOrCreateOAuthUser(nil, provider, oauthUser, newInviteTestSession(nil))
	var emailUsedErr *OAuthEmailAlreadyUsedError
	require.ErrorAs(t, err, &emailUsedErr)
	requireRegistrationUserMissing(t, db, oauthUser.Username)

	require.NoError(t, db.Model(&model.User{}).Where("id = ?", existing.Id).Update("discord_id", oauthUser.ProviderUserID).Error)
	boundUser, err := findOrCreateOAuthUser(nil, provider, oauthUser, newInviteTestSession(nil))
	require.NoError(t, err)
	require.Equal(t, existing.Id, boundUser.Id)
}

func TestPasswordResetRejectsAmbiguousEmailIdentity(t *testing.T) {
	db := setupInviteRegistrationControllerTestDB(t)
	common.EmailCaseInsensitiveEnabled = true

	for index, email := range []string{"duplicate@example.com", "Duplicate@example.com"} {
		user := model.User{
			Username:    fmt.Sprintf("reset-duplicate-%d", index),
			Password:    "password123",
			DisplayName: fmt.Sprintf("Reset Duplicate %d", index),
			Email:       email,
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			AffCode:     fmt.Sprintf("RESET-DUP-%d", index),
		}
		require.NoError(t, db.Create(&user).Error)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/reset_password?email=DUPLICATE@example.com", nil)
	SendPasswordResetEmail(ctx)

	var response registrationAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.False(t, response.Success)
}

func TestGenerateOAuthCodeReplacesOrClearsInviteCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	session := newInviteTestSession(map[interface{}]interface{}{
		"aff": "STALE-AFF",
	})

	requestState := func(target string) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
		ctx.Set(sessions.DefaultKey, session)
		GenerateOAuthCode(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
	}

	requestState("/api/oauth/state")
	require.Nil(t, session.Get("aff"))

	requestState("/api/oauth/state?aff=NEW-AFF")
	require.Equal(t, "NEW-AFF", session.Get("aff"))
}

func TestWeChatRegistrationRequiresInviteCodeButExistingLoginDoesNot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupInviteRegistrationControllerTestDB(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = false

	inviter := model.User{
		Username:    "wechat-inviter",
		Password:    "password123",
		DisplayName: "wechat-inviter",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		AffCode:     "WECHAT-AFF",
	}
	require.NoError(t, db.Create(&inviter).Error)

	wechatPayload, err := common.Marshal(gin.H{
		"success": true,
		"message": "",
		"data":    "wechat-new-user",
	})
	require.NoError(t, err)
	wechatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(wechatPayload)
	}))
	defer wechatServer.Close()

	originalWeChatAuthEnabled := common.WeChatAuthEnabled
	originalWeChatServerAddress := common.WeChatServerAddress
	originalWeChatServerToken := common.WeChatServerToken
	common.WeChatAuthEnabled = true
	common.WeChatServerAddress = wechatServer.URL
	common.WeChatServerToken = "test-token"
	t.Cleanup(func() {
		common.WeChatAuthEnabled = originalWeChatAuthEnabled
		common.WeChatServerAddress = originalWeChatServerAddress
		common.WeChatServerToken = originalWeChatServerToken
	})

	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("invite-code-test-secret"))))
	router.GET("/api/oauth/wechat", WeChatAuth)

	requestWeChat := func(target string) registrationAPIResponse {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		router.ServeHTTP(recorder, request)
		var response registrationAPIResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response
	}

	missingInvite := requestWeChat("/api/oauth/wechat?code=valid")
	require.False(t, missingInvite.Success)
	require.Contains(t, missingInvite.Message, "请输入邀请码")
	var count int64
	require.NoError(t, db.Model(&model.User{}).Where("wechat_id = ?", "wechat-new-user").Count(&count).Error)
	require.Zero(t, count)

	validInvite := requestWeChat("/api/oauth/wechat?code=valid&aff=" + inviter.AffCode)
	require.True(t, validInvite.Success)
	var createdUser model.User
	require.NoError(t, db.Where("wechat_id = ?", "wechat-new-user").First(&createdUser).Error)
	require.Equal(t, inviter.Id, createdUser.InviterId)

	existingLogin := requestWeChat("/api/oauth/wechat?code=valid")
	require.True(t, existingLogin.Success)
}
