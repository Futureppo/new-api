package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/cline"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClineModelFetch(t *testing.T) {
	payload := `{"free":[{"id":"cline-free/a"},{"id":"stealth/b-alpha"},{"id":" cline-free/a "}],"recommended":[{"id":"paid"}],"clinePass":[{"id":"subscription"}],"data":[{"id":"elsewhere:free"}]}`
	statusCode := http.StatusOK
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer overridden", r.Header.Get("Authorization"))
		require.Equal(t, "test", r.Header.Get("X-Catalog"))
		require.Equal(t, "Cline", r.Header.Get("X-Title"))
		require.Equal(t, []string{cline.UserAgent}, r.Header.Values("User-Agent"))
		require.Equal(t, "cline-cli", r.Header.Get("X-CLIENT-TYPE"))
		require.NotEmpty(t, r.Header.Get("X-CLIENT-VERSION"))
		require.Empty(t, r.Header.Get("X-Task-ID"), "catalog requests have no task")
		if r.URL.Path == "/api/v1/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"paid"}]}`))
			return
		}
		require.Contains(t, []string{"/api/v1/ai/cline/recommended-models", "/custom"}, r.URL.Path)
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()
	ch := &model.Channel{Type: constant.ChannelTypeCline, HeaderOverride: common.GetPointer(`{"Authorization":"Bearer overridden","X-Catalog":"test","uSeR-aGeNt":"OtherClient/1.0"}`)}
	for _, base := range []string{upstream.URL + "/api", upstream.URL + "/api/", upstream.URL + "/api/v1/"} {
		ids, err := fetchChannelModelIDsWithKey(ch, base, "test-key", "")
		require.NoError(t, err)
		require.Equal(t, []string{"cline-free/a", "stealth/b-alpha"}, ids)
	}
	ch.SetOtherSettings(dto.ChannelOtherSettings{ClineFreeModelSyncEnabled: common.GetPointer(false)})
	ids, err := fetchChannelModelIDsWithKey(ch, upstream.URL+"/api/v1", "test-key", "")
	require.NoError(t, err)
	require.Equal(t, []string{"paid"}, ids)
	ch.SetOtherSettings(dto.ChannelOtherSettings{})
	ids, err = fetchChannelModelIDsWithKey(ch, "https://unused.invalid", "test-key", upstream.URL+"/custom")
	require.NoError(t, err)
	require.Len(t, ids, 2)
	for _, invalid := range []string{`{}`, `{"free":null}`, `{"free":[]}`, `{"free":[{}]}`, `{"free":[{"id":""}]}`, `{"free":[{"id":"valid"},{"id":"a,b"}]}`, `{"free":[{"id":42}]}`, `{"data":[{"id":"x:free"}]}`, `{"success":false,"free":[{"id":"x"}]}`, `not json`} {
		payload = invalid
		_, err := fetchChannelModelIDsWithKey(ch, upstream.URL+"/api", "test-key", "")
		require.Error(t, err, invalid)
	}
	statusCode = http.StatusUnauthorized
	_, err = fetchChannelModelIDsWithKey(ch, upstream.URL+"/api", "test-key", "")
	require.ErrorContains(t, err, "401")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fetchClineModelIDs(ctx, ch, upstream.URL, "test-key", "", true)
	require.Error(t, err)
}

func TestClineFetchModelsAPIOverride(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"free":[{"id":"cline-free/a"}],"data":[{"id":"paid"}]}`))
	}))
	defer upstream.Close()
	request := map[string]any{"type": constant.ChannelTypeCline, "base_url": upstream.URL, "key": "test-key"}
	for _, free := range []bool{true, false} {
		if !free {
			request["cline_free_only"] = false
		}
		status, response := runFetchModelsRequest(t, request)
		require.Equal(t, http.StatusOK, status)
		require.True(t, response.Success)
		require.Equal(t, []string{map[bool]string{true: "cline-free/a", false: "paid"}[free]}, response.Data)
	}
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL}
	require.NoError(t, db.Create(&ch).Error)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?cline_free_only=false", nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(ch.Id)}}
	FetchUpstreamModels(c)
	require.Contains(t, w.Body.String(), `"paid"`)
	require.NoError(t, db.First(&ch, ch.Id).Error)
	require.True(t, ch.GetOtherSettings().ShouldSyncClineFreeModels(), "preview must not save settings")
}

func TestClineSyncLifecycle(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"free":[{"id":"cline-free/a"},{"id":"stealth/new-alpha"},{"id":"ignored"},{"id":"skip/regex"},{"id":"manual"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Models: "paid,manual,cline-free/a,old", Group: "default", Status: common.ChannelStatusEnabled, ModelMapping: common.GetPointer(`{"manual":"paid/target"}`)}
	settings := dto.ChannelOtherSettings{ClineFreeModelManagedModels: []string{"old", "manual"}, UpstreamModelUpdateIgnoredModels: []string{"ignored", "regex:^skip/"}, UpstreamModelUpdateAutoSyncEnabled: true}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	require.True(t, isChannelUpstreamModelUpdateEnabled(&ch, settings))
	changed, result, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"a", "new-alpha"}, result.AddedModels)
	require.ElementsMatch(t, []string{"old", "cline-free/a"}, result.RemovedModels)
	require.ElementsMatch(t, []string{"paid", "manual", "a", "new-alpha"}, ch.GetModels())
	require.ElementsMatch(t, []string{"a", "new-alpha"}, settings.ClineFreeModelManagedModels)
	require.Equal(t, map[string]string{"a": "cline-free/a", "new-alpha": "stealth/new-alpha", "manual": "paid/target"}, normalizeChannelModelMapping(&ch))
	var abilities []model.Ability
	require.NoError(t, db.Where("channel_id = ?", ch.Id).Find(&abilities).Error)
	require.Len(t, abilities, 4)
	changed, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.False(t, changed)
	payload = `{"free":[{"id":"cline-free/a"}]}`
	_, result, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.Equal(t, []string{"new-alpha"}, result.RemovedModels)
	require.Equal(t, map[string]string{"a": "cline-free/a", "manual": "paid/target"}, normalizeChannelModelMapping(&ch))
	before := ch.Models
	payload = `{"free":[]}`
	changed, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.Error(t, err)
	require.False(t, changed)
	require.Equal(t, before, ch.Models)
	settings.ClineFreeModelSyncEnabled = common.GetPointer(false)
	require.False(t, isChannelUpstreamModelUpdateEnabled(&ch, settings))
	require.False(t, isClineManagedModelSyncEnabled(&model.Channel{Type: constant.ChannelTypeOpenAI}, dto.ChannelOtherSettings{}))
}

func TestClineManualApplyAndConcurrentEdit(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"free":[{"id":"new"},{"id":"ignored"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Models: "old,manual", Group: "default"}
	settings := dto.ChannelOtherSettings{ClineFreeModelManagedModels: []string{"old"}}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	changed, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, false)
	require.NoError(t, err)
	require.False(t, changed)
	stale := ch
	_, _, remaining, remainingRemove, changed, err := applyChannelUpstreamModelUpdates(&ch, []string{"new", "unapproved"}, []string{"ignored"}, []string{"old", "manual"})
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, remaining)
	require.Empty(t, remainingRemove)
	require.ElementsMatch(t, []string{"new", "manual"}, ch.GetModels())
	// A stale detection/apply snapshot cannot restore models or settings.
	_, _, _, _, changed, err = applyChannelUpstreamModelUpdates(&stale, []string{"ignored"}, nil, nil)
	require.Error(t, err)
	require.False(t, changed)
	var saved model.Channel
	require.NoError(t, db.First(&saved, ch.Id).Error)
	require.Equal(t, ch.Models, saved.Models)
	require.Equal(t, []string{"ignored"}, saved.GetOtherSettings().UpstreamModelUpdateIgnoredModels)
}

func TestClineCreateAndEditSync(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	previousMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemoryCache })
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	t.Setenv("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", "true")
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"free":[{"id":"cline-free/current"}]}`))
	}))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Name: "cline", Models: "manual", Group: "default", Status: common.ChannelStatusEnabled}
	callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &ch}, AddChannel)
	var saved model.Channel
	require.NoError(t, db.Where("name = ?", "cline").First(&saved).Error)
	require.Equal(t, 1, requests)
	require.Contains(t, saved.GetModels(), "current")
	require.Equal(t, "cline-free/current", normalizeChannelModelMapping(&saved)["current"])
	cached, err := model.GetRandomSatisfiedChannel("default", "current", 0)
	require.NoError(t, err)
	require.NotNil(t, cached)
	require.Equal(t, saved.Id, cached.Id)
	lastCheck := saved.GetOtherSettings().UpstreamModelUpdateLastCheckTime
	// Simulate a form opened before the background sync updated ownership.
	saved.SetOtherSettings(dto.ChannelOtherSettings{})
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.Equal(t, []string{"current"}, saved.GetOtherSettings().ClineFreeModelManagedModels)
	require.Equal(t, map[string]string{"current": "cline-free/current"}, saved.GetOtherSettings().ClineModelGeneratedMappings)
	require.Equal(t, lastCheck, saved.GetOtherSettings().UpstreamModelUpdateLastCheckTime)
	require.Equal(t, 1, requests)
	// Settings-only edits from an older form omit unchanged models/mappings.
	callKiloChannelMutation(t, http.MethodPut, map[string]any{
		"id": saved.Id, "type": saved.Type, "name": saved.Name,
		"base_url": saved.GetBaseURL(), "status": saved.Status,
		"settings": "{}", "group": "default",
	}, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.Contains(t, saved.GetModels(), "current")
	require.Equal(t, 1, requests)
	saved.SetOtherSettings(dto.ChannelOtherSettings{ClineFreeModelSyncEnabled: common.GetPointer(false)})
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.False(t, saved.GetOtherSettings().ShouldSyncClineFreeModels())
	require.Contains(t, saved.GetModels(), "current")
	require.Equal(t, 1, requests)
	callKiloChannelMutation(t, http.MethodPut, map[string]any{
		"id": saved.Id, "type": saved.Type, "name": "renamed-cline",
	}, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.False(t, saved.GetOtherSettings().ShouldSyncClineFreeModels())
	require.Equal(t, upstream.URL, saved.GetBaseURL())
	require.Equal(t, 1, requests)
	saved.SetOtherSettings(dto.ChannelOtherSettings{})
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.Equal(t, 2, requests)
	saved.Key = "new-test-key"
	callKiloChannelMutation(t, http.MethodPut, saved, UpdateChannel)
	require.Equal(t, 3, requests)
	t.Setenv("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", "false")
	syncClineModelsAfterSave(&saved)
	require.Equal(t, 3, requests)
}

func TestClineCopyPreservesSyncChoice(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })
	t.Setenv("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", "true")
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"free":[{"id":"cline-free/current"}]}`))
	}))
	defer upstream.Close()
	for _, enabled := range []bool{true, false} {
		ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Name: fmt.Sprintf("copy-%t", enabled), Models: "manual,cline-free/legacy", Group: "default", Status: common.ChannelStatusEnabled}
		ch.SetOtherSettings(dto.ChannelOtherSettings{ClineFreeModelSyncEnabled: common.GetPointer(enabled)})
		require.NoError(t, db.Create(&ch).Error)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy/", nil)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(ch.Id)}}
		CopyChannel(c)
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				ID int `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.True(t, response.Success)
		require.Positive(t, response.Data.ID)
		require.NotEqual(t, ch.Id, response.Data.ID)
		var clone model.Channel
		require.NoError(t, db.First(&clone, response.Data.ID).Error)
		require.Equal(t, enabled, clone.GetOtherSettings().ShouldSyncClineFreeModels())
		require.Contains(t, clone.GetModels(), "legacy")
		require.Equal(t, "cline-free/legacy", normalizeChannelModelMapping(&clone)["legacy"])
	}
	require.Equal(t, 1, requests)
}
