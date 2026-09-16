package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKiloModelListFiltering(t *testing.T) {
	payload := `{"data":[{"id":"paid-zero","isFree":false,"pricing":{"prompt":"0","completion":"0"}},{"id":"paid-alpha","isFree":false},{"id":"kilo-auto/free","isFree":true},{"id":"openrouter/free","isFree":true},{"id":"vendor/test:free","isFree":true},{"id":"opaque-free","isFree":true},{"id":" vendor/test:free ","isFree":true}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/gateway/models", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()
	ch := &model.Channel{Type: constant.ChannelTypeKilo, HeaderOverride: common.GetPointer(`{"Authorization":"Bearer stale"}`)}
	ch.SetOtherSettings(dto.ChannelOtherSettings{KiloAnonymousEnabled: true, KiloFreeModelSyncEnabled: true})
	models, err := fetchChannelModelIDsWithKey(ch, upstream.URL+"/gateway/", "old-key", "")
	require.NoError(t, err)
	require.Equal(t, []string{"kilo-auto/free", "openrouter/free", "vendor/test:free", "opaque-free"}, models)
	ch.SetOtherSettings(dto.ChannelOtherSettings{KiloAnonymousEnabled: true})
	models, err = fetchChannelModelIDsWithKey(ch, "https://unused.invalid", "", upstream.URL+"/gateway/models")
	require.NoError(t, err)
	require.Len(t, models, 6)
	for _, invalid := range []string{`{}`, `{"data":null}`, `{"data":[]}`, `{"data":[{"id":"v/free:free"}]}`, `{"data":[{"id":"paid","isFree":false}]}`, `{"data":[{"id":"bad","isFree":"true"}]}`, `not json`} {
		payload = invalid
		_, err := fetchKiloModelIDs(ch, upstream.URL+"/gateway/models", "", true)
		require.Error(t, err, invalid)
	}
}

func TestKiloFetchModelsAPIs(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[{"id":"free","isFree":true},{"id":"paid","isFree":false}]}`))
	}))
	defer upstream.Close()
	status, response := runFetchModelsRequest(t, map[string]any{"type": constant.ChannelTypeKilo, "base_url": upstream.URL, "kilo_free_only": true})
	require.Equal(t, http.StatusOK, status)
	require.True(t, response.Success)
	require.Equal(t, []string{"free"}, response.Data)
	ch := model.Channel{Type: constant.ChannelTypeKilo, BaseURL: &upstream.URL, Models: "free"}
	ch.SetOtherSettings(dto.ChannelOtherSettings{KiloAnonymousEnabled: true, KiloFreeModelSyncEnabled: true})
	require.NoError(t, db.Create(&ch).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?kilo_free_only=false", nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(ch.Id)}}
	FetchUpstreamModels(c)
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, []string{"free", "paid"}, response.Data)
	var saved model.Channel
	require.NoError(t, db.First(&saved, ch.Id).Error)
	require.True(t, saved.GetOtherSettings().KiloFreeModelSyncEnabled, "query override must not change saved settings")
}

func callKiloChannelMutation(t *testing.T, method string, body any, handler gin.HandlerFunc) {
	t.Helper()
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/channel/", bytes.NewReader(encoded))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleRootUser)
	handler(c)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.True(t, response.Success, response.Message)
}

func TestKiloAnonymousChannelCreateAndEdit(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	ch := model.Channel{Type: constant.ChannelTypeKilo, Name: "anonymous-kilo", Models: "openrouter/free", Group: "default", Status: common.ChannelStatusEnabled}
	callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &ch}, AddChannel)
	var saved model.Channel
	require.NoError(t, db.Where("name = ?", ch.Name).First(&saved).Error)
	require.True(t, saved.GetOtherSettings().KiloAnonymousEnabled)
	require.Empty(t, saved.Key)
	var count int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", saved.Id).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Changing to key mode, then to anonymous mode, must preserve the stored key.
	saved.Key = "saved-key"
	saved.SetOtherSettings(dto.ChannelOtherSettings{})
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.False(t, saved.GetOtherSettings().KiloAnonymousEnabled)
	saved.Key = ""
	saved.SetOtherSettings(dto.ChannelOtherSettings{KiloAnonymousEnabled: true})
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.Equal(t, "saved-key", saved.Key)
	key, _, apiErr := saved.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Empty(t, key)
	saved.Key = ""
	saved.SetOtherSettings(dto.ChannelOtherSettings{})
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	key, _, apiErr = saved.GetNextEnabledKey()
	require.Nil(t, apiErr)
	require.Equal(t, "saved-key", key)
}

func TestKiloCreateRespectsExplicitAuthMode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		settings  string
		key       string
		success   bool
		anonymous bool
	}{
		{"explicit key mode without key", `{"kilo_anonymous_enabled":false}`, "", false, false},
		{"explicit key mode with key", `{"kilo_anonymous_enabled":false}`, "saved-key", true, false},
		{"explicit anonymous mode", `{"kilo_anonymous_enabled":true}`, "", true, true},
		{"anonymous preserves key", `{"kilo_anonymous_enabled":true}`, "saved-key", true, true},
		{"legacy empty key", "", "", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openChannelRetryControllerTestDB(t)
			ch := model.Channel{Type: constant.ChannelTypeKilo, Name: tc.name, Key: tc.key, OtherSettings: tc.settings, Models: "openrouter/free", Group: "default", Status: common.ChannelStatusEnabled}
			body, err := common.Marshal(AddChannelRequest{Mode: "single", Channel: &ch})
			require.NoError(t, err)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("role", common.RoleRootUser)
			AddChannel(c)
			var response struct {
				Success bool `json:"success"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, tc.success, response.Success, w.Body.String())
			var channels []model.Channel
			require.NoError(t, db.Where("name = ?", tc.name).Find(&channels).Error)
			if !tc.success {
				require.Empty(t, channels, "key mode must not silently create an anonymous channel")
				return
			}
			require.Len(t, channels, 1)
			require.Equal(t, tc.anonymous, channels[0].GetOtherSettings().KiloAnonymousEnabled)
			require.Equal(t, tc.key, channels[0].Key)
		})
	}
}

func TestKiloFetchModelsRespectsExplicitAuthMode(t *testing.T) {
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"free","isFree":true}]}`))
	}))
	defer upstream.Close()
	req := map[string]any{"type": constant.ChannelTypeKilo, "base_url": upstream.URL, "kilo_anonymous_enabled": false}
	status, response := runFetchModelsRequest(t, req)
	require.Equal(t, http.StatusBadRequest, status)
	require.False(t, response.Success)
	require.Zero(t, requests, "explicit key mode must not make an anonymous upstream request")
	req["kilo_anonymous_enabled"] = true
	status, response = runFetchModelsRequest(t, req)
	require.Equal(t, http.StatusOK, status)
	require.True(t, response.Success)
	require.Equal(t, 1, requests)
}

func TestKiloSyncLifecycle(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"data":[{"id":"vendor/a:free","isFree":true},{"id":"opaque","isFree":true},{"id":"kilo-auto/free","isFree":true},{"id":"openrouter/free","isFree":true},{"id":"ignored","isFree":true}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeKilo, BaseURL: &upstream.URL, Name: "kilo-sync", Status: common.ChannelStatusEnabled, Group: "default", Models: "paid/manual,vendor/a:free,old-free,manual", ModelMapping: common.GetPointer(`{"manual":"paid/upstream"}`)}
	settings := dto.ChannelOtherSettings{KiloAnonymousEnabled: true, KiloFreeModelSyncEnabled: true, KiloFreeModelNameSimplificationEnabled: true, KiloFreeModelManagedModels: []string{"old-free"}, UpstreamModelUpdateIgnoredModels: []string{"ignored"}}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	changed, result, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"vendor/a:free", "old-free"}, result.RemovedModels)
	require.ElementsMatch(t, []string{"paid/manual", "manual", "a", "opaque", "kilo-auto/free", "openrouter/free"}, ch.GetModels())
	require.Equal(t, map[string]string{"manual": "paid/upstream", "a": "vendor/a:free"}, normalizeChannelModelMapping(&ch))
	var abilities []string
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", ch.Id).Pluck("model", &abilities).Error)
	require.ElementsMatch(t, ch.GetModels(), abilities)
	// Explicitly becoming paid and disappearing are both removals, even without :free.
	payload = `{"data":[{"id":"vendor/a:free","isFree":true},{"id":"opaque","isFree":false}]}`
	_, result, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"opaque", "kilo-auto/free", "openrouter/free"}, result.RemovedModels)
	// Disabling simplification restores the complete upstream ID.
	settings.KiloFreeModelNameSimplificationEnabled = false
	ch.SetOtherSettings(settings)
	_, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"paid/manual", "manual", "vendor/a:free"}, ch.GetModels())
	require.Equal(t, map[string]string{"manual": "paid/upstream"}, normalizeChannelModelMapping(&ch))
	for _, invalid := range []string{`{"data":[]}`, `{"data":[{"id":"bad"}]}`} {
		payload = invalid
		before := ch.Models
		changed, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, before, ch.Models)
	}
}

func TestKiloManualApplyAndMappingConflicts(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"data":[{"id":"one/shared:free","isFree":true},{"id":"two/shared:free","isFree":true},{"id":"vendor/manual:free","isFree":true},{"id":"vendor/new:free","isFree":true}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeKilo, BaseURL: &upstream.URL, Models: "manual", Group: "default", ModelMapping: common.GetPointer(`{"manual":"custom/target"}`)}
	settings := dto.ChannelOtherSettings{KiloAnonymousEnabled: true, KiloFreeModelSyncEnabled: true, KiloFreeModelNameSimplificationEnabled: true}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	changed, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.ElementsMatch(t, []string{"one/shared:free", "two/shared:free", "vendor/manual:free", "new"}, settings.UpstreamModelUpdateLastDetectedModels)
	_, _, remaining, _, _, err := applyChannelUpstreamModelUpdates(&ch, []string{"new"}, []string{"one/shared:free"}, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"two/shared:free", "vendor/manual:free"}, remaining)
	require.Equal(t, map[string]string{"manual": "custom/target", "new": "vendor/new:free"}, normalizeChannelModelMapping(&ch))
	// A subsequent administrator edit relinquishes ownership of the generated mapping.
	ch.ModelMapping = common.GetPointer(`{"manual":"custom/target","new":"custom/new"}`)
	settings = ch.GetOtherSettings()
	payload = `{"data":[{"id":"unrelated","isFree":true}]}`
	_, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.Contains(t, ch.GetModels(), "new")
	require.Equal(t, "custom/new", normalizeChannelModelMapping(&ch)["new"])
}

func TestKiloSyncRespectsDeletedMappingsAndNewManualAliases(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	ch := model.Channel{Type: constant.ChannelTypeKilo, Models: "old,added-by-admin", Group: "default"}
	settings := dto.ChannelOtherSettings{
		KiloFreeModelSyncEnabled: true, KiloFreeModelNameSimplificationEnabled: true,
		KiloFreeModelManagedModels:            []string{"old"},
		KiloFreeModelGeneratedMappings:        map[string]string{"old": "vendor/old:free"},
		KiloFreeModelPendingMappings:          map[string]string{"added-by-admin": "vendor/added-by-admin:free"},
		UpstreamModelUpdateLastDetectedModels: []string{"added-by-admin"},
		UpstreamModelUpdateLastRemovedModels:  []string{"old"},
	}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	// The old mapping was deleted and a new plain model was added after detection.
	_, _, _, _, changed, err := applyChannelUpstreamModelUpdates(&ch, []string{"added-by-admin"}, nil, []string{"old"})
	require.NoError(t, err)
	require.False(t, changed)
	require.ElementsMatch(t, []string{"old", "added-by-admin"}, ch.GetModels())
	require.Empty(t, normalizeChannelModelMapping(&ch))
	_, remove, _, _ := buildKiloManagedModelChanges(&ch, ch.GetOtherSettings(), []string{"unrelated"})
	require.Empty(t, remove)
}
