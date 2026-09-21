package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nodeLocControllerTransport func(*http.Request) (*http.Response, error)

func (f nodeLocControllerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setupNodeLocController(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	db := setupInviteRegistrationControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	oldSettings := *system_setting.GetNodeLocSettings()
	oldAddress := system_setting.ServerAddress
	oldOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	*system_setting.GetNodeLocSettings() = system_setting.NodeLocSettings{Enabled: true, ClientId: "client", ClientSecret: "SECRET-MUST-NOT-LEAK"}
	system_setting.ServerAddress = "https://gateway.example/"
	setting.GetEnhancementSetting().InviteCodeRequired = false
	setting.GetEnhancementSetting().RegistrationCodeRequired = false
	t.Cleanup(func() {
		*system_setting.GetNodeLocSettings() = oldSettings
		system_setting.ServerAddress = oldAddress
		common.OptionMap = oldOptions
	})
}

func nodeLocCallback(t *testing.T, query string, values map[interface{}]interface{}) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/oauth/nodeloc?"+query, nil)
	c.Params = gin.Params{{Key: "provider", Value: "nodeloc"}}
	c.Set(sessions.DefaultKey, newInviteTestSession(values))
	HandleOAuth(c)
	var result map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	return w.Code, result
}

func TestNodeLocAccountLifecycle(t *testing.T) {
	setupNodeLocController(t)
	var remoteID, calls atomic.Int64
	remoteID.Store(123)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/oauth-provider/token" {
			fmt.Fprint(w, `{"access_token":"token"}`)
			return
		}
		fmt.Fprintf(w, `{"id":%d,"username":"existing-name","name":"","email":"ignored@example.com","trust_level":0}`, remoteID.Load())
	}))
	t.Cleanup(server.Close)
	originalTransport := http.DefaultTransport
	transport := server.Client().Transport
	http.DefaultTransport = nodeLocControllerTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "www.nodeloc.com", r.URL.Host)
		r = r.Clone(r.Context())
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return transport.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	occupied := model.User{Username: "existing-name", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&occupied).Error)
	freshSession := func() map[interface{}]interface{} { return map[interface{}]interface{}{"oauth_state": "valid"} }

	_, res := nodeLocCallback(t, "state=valid&code=code", freshSession())
	require.Equal(t, true, res["success"], res)
	var user model.User
	require.NoError(t, model.DB.Where("nodeloc_id = ?", "123").First(&user).Error)
	require.NotEqual(t, occupied.Id, user.Id)
	require.True(t, strings.HasPrefix(user.Username, "nodeloc_"))
	require.Equal(t, "existing-name", user.DisplayName)
	require.Empty(t, user.Email)
	_, res = nodeLocCallback(t, "state=valid&code=another-code", freshSession())
	require.Equal(t, true, res["success"])
	require.Equal(t, float64(user.Id), res["data"].(map[string]any)["id"])
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("nodeloc_id = ?", "123").Count(&count).Error)
	require.EqualValues(t, 1, count)

	// Registration switches do not prevent an existing identity from logging in.
	common.RegisterEnabled = false
	_, res = nodeLocCallback(t, "state=valid&code=code", freshSession())
	require.Equal(t, true, res["success"])
	remoteID.Store(124)
	_, res = nodeLocCallback(t, "state=valid&code=code", freshSession())
	require.Equal(t, false, res["success"])
	common.RegisterEnabled = true

	// Bind an existing local account, then refuse use of that identity by another account.
	bindSession := freshSession()
	bindSession["id"] = occupied.Id
	bindSession["username"] = occupied.Username
	_, res = nodeLocCallback(t, "state=valid&code=code", bindSession)
	require.Equal(t, true, res["success"], res)
	require.Equal(t, "bind", res["data"].(map[string]any)["action"])
	require.NoError(t, model.DB.First(&occupied, occupied.Id).Error)
	require.Equal(t, "124", occupied.NodeLocId)
	conflict := freshSession()
	conflict["id"] = user.Id
	conflict["username"] = user.Username
	_, res = nodeLocCallback(t, "state=valid&code=code", conflict)
	require.Equal(t, false, res["success"])

	// Denial must not exchange tokens in either login or binding flows.
	for _, session := range []map[interface{}]interface{}{freshSession(), bindSession} {
		before := calls.Load()
		_, res = nodeLocCallback(t, "state=valid&error=access_denied&error_description=denied", session)
		require.Equal(t, false, res["success"])
		require.Equal(t, before, calls.Load())
		status, _ := nodeLocCallback(t, "state=invalid&error=access_denied", session)
		require.Equal(t, http.StatusForbidden, status)
		require.Equal(t, before, calls.Load())
	}
	before := calls.Load()
	system_setting.GetNodeLocSettings().Enabled = false
	_, res = nodeLocCallback(t, "state=valid&code=code", freshSession())
	require.Equal(t, false, res["success"])
	require.Equal(t, before, calls.Load())
	system_setting.GetNodeLocSettings().Enabled = true

	// Admin unbinding uses the existing permission and audit path.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/user/binding", nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(occupied.Id)}, {Key: "binding_type", Value: "nodeloc"}}
	c.Set("role", common.RoleRootUser)
	AdminClearUserBinding(c)
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &res))
	require.Equal(t, true, res["success"])
	var unbound model.User
	require.NoError(t, model.DB.First(&unbound, occupied.Id).Error)
	require.Empty(t, unbound.NodeLocId)

	remoteID.Store(123)
	require.NoError(t, model.DB.Model(&user).Update("status", common.UserStatusDisabled).Error)
	_, res = nodeLocCallback(t, "state=valid&code=code", freshSession())
	require.Equal(t, false, res["success"])
	require.NoError(t, model.DB.Delete(&user).Error)
	_, res = nodeLocCallback(t, "state=valid&code=code", freshSession())
	require.Equal(t, false, res["success"])
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).Where("nodeloc_id = ?", "123").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestNodeLocRegistrationCodes(t *testing.T) {
	setupNodeLocController(t)
	cfg := setting.GetEnhancementSetting()
	cfg.InviteCodeRequired = true
	cfg.RegistrationCodeRequired = true
	inviter := model.User{Username: "inviter", Status: common.UserStatusEnabled, AffCode: inviteRegistrationTestCode(t)}
	require.NoError(t, model.DB.Create(&inviter).Error)
	code := model.RegistrationCode{Code: "NODELOC-REG", Status: common.RegistrationCodeStatusEnabled, Name: "NodeLoc test", MaxUses: 1}
	require.NoError(t, model.DB.Create(&code).Error)
	provider := &oauth.NodeLocProvider{}
	identity := &oauth.OAuthUser{ProviderUserID: "200", Username: "node-new"}
	_, err := findOrCreateOAuthUser(nil, provider, identity, newInviteTestSession(nil))
	require.Error(t, err)
	_, err = findOrCreateOAuthUser(nil, provider, identity, newInviteTestSession(map[interface{}]interface{}{"aff": inviter.AffCode}))
	require.Error(t, err)
	requireRegistrationUserMissing(t, model.DB, identity.Username)
	user, err := findOrCreateOAuthUser(nil, provider, identity, newInviteTestSession(map[interface{}]interface{}{"aff": inviter.AffCode, "registration_code": code.Code}))
	require.NoError(t, err)
	require.Equal(t, inviter.Id, user.InviterId)
	require.NoError(t, model.DB.First(&code, code.Id).Error)
	require.Equal(t, 1, code.UsedCount)
	existing, err := findOrCreateOAuthUser(nil, provider, identity, newInviteTestSession(nil))
	require.NoError(t, err)
	require.Equal(t, user.Id, existing.Id)
}

func TestNodeLocOptionsAndStatus(t *testing.T) {
	setupNodeLocController(t)
	settings := system_setting.GetNodeLocSettings()
	*settings = system_setting.NodeLocSettings{}
	require.False(t, settings.Enabled)
	put := func(key string, value any) bool {
		body, err := common.Marshal(gin.H{"key": key, "value": value})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))
		UpdateOption(c)
		var res map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &res))
		return res["success"] == true
	}
	require.False(t, put("nodeloc.enabled", true))
	require.True(t, put("nodeloc.client_id", "client"))
	require.False(t, put("nodeloc.enabled", true))
	require.True(t, put("nodeloc.client_secret", "SECRET-MUST-NOT-LEAK"))
	system_setting.ServerAddress = "invalid"
	require.False(t, put("nodeloc.enabled", true))
	require.True(t, put("ServerAddress", "https://gateway.example/"))
	require.True(t, put("nodeloc.enabled", true))
	require.True(t, settings.Enabled)
	require.False(t, put("nodeloc.enabled", "anything"))
	require.False(t, put("nodeloc.client_id", ""))
	require.False(t, put("ServerAddress", "https://gateway.example/path"))
	require.True(t, put("nodeloc.client_secret", ""))
	require.Equal(t, "SECRET-MUST-NOT-LEAK", settings.ClientSecret)
	var stored model.Option
	require.NoError(t, model.DB.Where("key = ?", "nodeloc.client_secret").First(&stored).Error)
	require.Equal(t, settings.ClientSecret, stored.Value)
	for _, handler := range []gin.HandlerFunc{GetOptions, GetStatus} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		handler(c)
		require.NotContains(t, w.Body.String(), "SECRET-MUST-NOT-LEAK")
		require.NotContains(t, w.Body.String(), "nodeloc.client_secret")
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	GetStatus(c)
	var res struct {
		Data struct {
			Enabled     bool   `json:"nodeloc_oauth"`
			ClientID    string `json:"nodeloc_client_id"`
			RedirectURI string `json:"nodeloc_redirect_uri"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &res))
	require.True(t, res.Data.Enabled)
	require.Equal(t, "client", res.Data.ClientID)
	require.Equal(t, "https://gateway.example/oauth/nodeloc", res.Data.RedirectURI)
	// Simulate configuration reload using values actually persisted to the database.
	options, err := model.AllOption()
	require.NoError(t, err)
	values := make(map[string]string)
	for _, option := range options {
		values[option.Key] = option.Value
	}
	*settings = system_setting.NodeLocSettings{}
	require.NoError(t, config.GlobalConfig.LoadFromDB(values))
	require.True(t, settings.Enabled)
	require.Equal(t, "client", settings.ClientId)
	require.Equal(t, "SECRET-MUST-NOT-LEAK", settings.ClientSecret)
}

func TestNodeLocSlugReservedForBuiltInProvider(t *testing.T) {
	setupNodeLocController(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CustomOAuthProvider{}))
	provider := oauth.GetProvider("nodeloc")
	require.IsType(t, &oauth.NodeLocProvider{}, provider)
	body, err := common.Marshal(gin.H{
		"name": "Custom NodeLoc", "slug": "nodeloc", "client_id": "client", "client_secret": "secret",
		"authorization_endpoint": "https://www.nodeloc.com/oauth-provider/authorize",
		"token_endpoint":         "https://www.nodeloc.com/oauth-provider/token",
		"user_info_endpoint":     "https://www.nodeloc.com/oauth-provider/userinfo",
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/custom-oauth-provider/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	CreateCustomOAuthProvider(c)
	var res map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &res))
	require.Equal(t, false, res["success"])
	require.Same(t, provider, oauth.GetProvider("nodeloc"))
}

func TestOAuthOtherProvidersLeaveNodeLocUnbound(t *testing.T) {
	setupNodeLocController(t)
	for i := 1; i <= 2; i++ {
		user, err := findOrCreateOAuthUser(nil, &oauth.DiscordProvider{}, &oauth.OAuthUser{
			ProviderUserID: fmt.Sprint(i), Username: fmt.Sprintf("discord-%d", i),
		}, newInviteTestSession(nil))
		require.NoError(t, err)
		require.Empty(t, user.NodeLocId)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.User{}).Where("nodeloc_id IS NULL").Count(&count).Error)
	require.EqualValues(t, 2, count)
}
