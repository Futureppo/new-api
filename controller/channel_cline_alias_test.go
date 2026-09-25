package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClineAliasConflicts(t *testing.T) {
	for _, tc := range []struct {
		name, models, mapping string
		upstream, desired     []string
		aliases               map[string]string
	}{
		{name: "prefix only", upstream: []string{"cline-free/a", "stealth/b-alpha", "vendor/nested/model:free"}, desired: []string{"a", "b-alpha", "nested/model:free"}, aliases: map[string]string{"a": "cline-free/a", "b-alpha": "stealth/b-alpha", "nested/model:free": "vendor/nested/model:free"}},
		{name: "provider collision", upstream: []string{"one/a", "two/a"}, desired: []string{"one/a", "two/a"}},
		{name: "unqualified collision", upstream: []string{"a", "one/a"}, desired: []string{"a", "one/a"}},
		{name: "manual model", models: "a", upstream: []string{"one/a"}, desired: []string{"one/a"}},
		{name: "manual alias", mapping: `{"a":"manual/target"}`, upstream: []string{"one/a"}, desired: []string{"one/a"}},
		{name: "mapped original", models: "one/a", mapping: `{"one/a":"manual/target"}`, upstream: []string{"one/a"}, desired: []string{"one/a"}},
		{name: "reuse exact existing mapping", models: "a", mapping: `{"a":"one/a"}`, upstream: []string{"one/a"}, desired: []string{"a"}, aliases: map[string]string{"a": "one/a"}},
		{name: "short name and mapped ID are one model", models: "a,one/a", mapping: `{"a":"one/a"}`, upstream: []string{"a", "one/a"}, desired: []string{"a"}, aliases: map[string]string{"a": "one/a"}},
		{name: "nested mapped alias", models: "nested/a,vendor/nested/a", mapping: `{"nested/a":"vendor/nested/a"}`, upstream: []string{"nested/a", "vendor/nested/a"}, desired: []string{"nested/a"}, aliases: map[string]string{"nested/a": "vendor/nested/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := model.Channel{Models: tc.models, ModelMapping: &tc.mapping}
			desired, aliases := planClineModelAliases(&ch, tc.upstream, nil, nil)
			require.ElementsMatch(t, tc.desired, desired)
			require.True(t, modelMappingsEqual(tc.aliases, aliases))
		})
	}
}

func TestClineSyncRepairsDuplicatePrefixedModels(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy mapping", true: "generated mapping"}[owned], func(t *testing.T) {
			db := openChannelRetryControllerTestDB(t)
			payload := `{"free":[{"id":"stealth/space-bunny-alpha"},{"id":"cline-free/deepseek-v4.1-flash"}]}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
			defer upstream.Close()
			mapping := map[string]string{"space-bunny-alpha": "stealth/space-bunny-alpha", "deepseek-v4.1-flash": "cline-free/deepseek-v4.1-flash", "manual": "other/paid"}
			ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Group: "default", Models: "space-bunny-alpha,deepseek-v4.1-flash,stealth/space-bunny-alpha,cline-free/deepseek-v4.1-flash,manual"}
			_, err := setChannelModelMapping(&ch, mapping)
			require.NoError(t, err)
			settings := dto.ChannelOtherSettings{}
			if owned {
				settings.ClineModelGeneratedMappings = map[string]string{"space-bunny-alpha": "stealth/space-bunny-alpha", "deepseek-v4.1-flash": "cline-free/deepseek-v4.1-flash"}
				settings.ClineFreeModelManagedModels = []string{"space-bunny-alpha", "deepseek-v4.1-flash"}
			}
			ch.SetOtherSettings(settings)
			require.NoError(t, db.Create(&ch).Error)
			// Saving a list containing both spellings must collapse it immediately.
			copy := ch
			_, err = normalizeClineChannelModels(&copy)
			require.NoError(t, err)
			require.ElementsMatch(t, []string{"space-bunny-alpha", "deepseek-v4.1-flash", "manual"}, copy.GetModels())
			// The scheduled path must repair an already-persisted duplicate list.
			changed, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
			require.NoError(t, err)
			require.True(t, changed)
			require.ElementsMatch(t, []string{"space-bunny-alpha", "deepseek-v4.1-flash", "manual"}, ch.GetModels())
			require.Equal(t, mapping, normalizeChannelModelMapping(&ch))
			changed, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
			require.NoError(t, err)
			require.False(t, changed, "repeated sync must never re-add prefixed IDs")
			var abilities []model.Ability
			require.NoError(t, db.Where("channel_id = ?", ch.Id).Find(&abilities).Error)
			require.Len(t, abilities, 3)
			if !owned {
				require.Empty(t, settings.ClineModelGeneratedMappings, "reusing a manual mapping does not take ownership")
			}
		})
	}
}

func TestClineAutomaticSyncNeverFallsBackToPrefixedNames(t *testing.T) {
	for _, tc := range []struct {
		name, models, mapping string
		upstream              []string
	}{
		{name: "occupied short name", models: "a", upstream: []string{"one/a"}},
		{name: "manual mapping", models: "a", mapping: `{"a":"manual/target"}`, upstream: []string{"one/a"}},
		{name: "ambiguous provider", upstream: []string{"one/a", "two/a"}},
		{name: "manual full ID with collision", models: "a,one/a", mapping: `{"a":"manual/target"}`, upstream: []string{"one/a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := model.Channel{Type: constant.ChannelTypeCline, Models: tc.models, ModelMapping: &tc.mapping}
			add, remove, _, _ := buildClineManagedModelChanges(&ch, dto.ChannelOtherSettings{}, tc.upstream)
			require.Empty(t, add, "automatic sync must skip collisions instead of adding prefixed names")
			require.Empty(t, remove, "unrelated manual models must survive")
		})
	}
}

func TestClineLegacyPendingIDsCannotReintroducePrefixes(t *testing.T) {
	ch := model.Channel{Type: constant.ChannelTypeCline, Models: "a", ModelMapping: common.GetPointer(`{"a":"one/a"}`)}
	settings := dto.ChannelOtherSettings{UpstreamModelUpdateLastDetectedModels: []string{"one/a"}}
	added, _, _, _, changed, err := prepareClineModelUpdates(&ch, &settings, []string{"one/a"}, nil, nil)
	require.NoError(t, err)
	require.Empty(t, added)
	require.False(t, changed)
	require.Equal(t, "a", ch.Models)
}

func TestClineSyncReusesManualMappingWithoutTakingOwnership(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"free":[{"id":"one/a"},{"id":"two/a"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Group: "default", Models: "one/a,old,retired/old", ModelMapping: common.GetPointer(`{"a":"one/a","old":"retired/old"}`)}
	settings := dto.ChannelOtherSettings{}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	_, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "old"}, ch.GetModels())
	require.Equal(t, map[string]string{"a": "one/a", "old": "retired/old"}, normalizeChannelModelMapping(&ch))
	require.Empty(t, settings.ClineModelGeneratedMappings)
	require.Empty(t, settings.ClineFreeModelManagedModels)
	changed, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.False(t, changed)
	// Retiring the model must not delete an explicit manual mapping.
	payload = `{"free":[{"id":"three/b"}]}`
	_, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"a", "old", "b"}, ch.GetModels())
}

func TestClineAliasRetargetAndManualProtection(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"free":[{"id":"old/a"},{"id":"cline-free/stable"},{"id":"ignored/raw"},{"id":"one/short-ignored"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Group: "default", Models: "manual"}
	settings := dto.ChannelOtherSettings{UpstreamModelUpdateIgnoredModels: []string{"regex:^ignored/", "short-ignored"}}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	_, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"manual", "a", "stable"}, ch.GetModels())
	// Same alias, new upstream provider: mapping-only changes must persist.
	payload = `{"free":[{"id":"new/a"},{"id":"cline-free/stable"}]}`
	changed, result, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, result.AddedModels)
	var saved model.Channel
	require.NoError(t, db.First(&saved, ch.Id).Error)
	require.Equal(t, "new/a", normalizeChannelModelMapping(&saved)["a"])
	// A manual mapping edit relinquishes ownership, even after the model retires.
	_, err = setChannelModelMapping(&ch, map[string]string{"a": "manual/target", "stable": "cline-free/stable"})
	require.NoError(t, err)
	require.NoError(t, db.Model(&ch).Update("model_mapping", ch.ModelMapping).Error)
	payload = `{"free":[{"id":"cline-free/stable"}]}`
	_, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.Contains(t, ch.GetModels(), "a")
	require.Equal(t, "manual/target", normalizeChannelModelMapping(&ch)["a"])
	require.NotContains(t, settings.ClineFreeModelManagedModels, "a")
}

func TestClineAliasPendingApplyAndDeletedMapping(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"free":[{"id":"cline-free/a"},{"id":"stealth/b"}]}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(payload)) }))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Group: "default", Models: "manual"}
	settings := dto.ChannelOtherSettings{}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	_, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, false)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"a": "cline-free/a", "b": "stealth/b"}, settings.ClineFreeModelPendingMappings)
	_, _, _, _, changed, err := applyChannelUpstreamModelUpdates(&ch, []string{"a"}, []string{"b"}, nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "cline-free/a", normalizeChannelModelMapping(&ch)["a"])
	// Deleting a generated mapping and saving must protect the remaining model.
	ch.ModelMapping = common.GetPointer("{}")
	_, err = normalizeClineChannelModels(&ch)
	require.NoError(t, err)
	require.NotContains(t, ch.GetOtherSettings().ClineFreeModelManagedModels, "a")
	require.NoError(t, db.Model(&ch).Updates(map[string]any{"model_mapping": ch.ModelMapping, "settings": ch.OtherSettings}).Error)
	payload = `{"free":[{"id":"cline-free/c"}]}`
	settings = ch.GetOtherSettings()
	_, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"manual", "a", "c"}, ch.GetModels())
}

func TestClineSavedAliasRelaysFullID(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	t.Setenv("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", "false")
	service.InitHttpClient()
	captured := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request dto.GeneralOpenAIRequest
		require.NoError(t, common.DecodeJson(r.Body, &request))
		captured <- request.Model
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeCline, Key: "test-key", BaseURL: &upstream.URL, Name: "alias", Models: "cline-free/deepseek-v4.1-flash,stealth/space-bunny-alpha", Group: "default", Status: common.ChannelStatusEnabled}
	ch.SetOtherSettings(dto.ChannelOtherSettings{ClineFreeModelSyncEnabled: common.GetPointer(false)})
	callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &ch}, AddChannel)
	var saved model.Channel
	require.NoError(t, db.Where("name = ?", "alias").First(&saved).Error)
	require.ElementsMatch(t, []string{"deepseek-v4.1-flash", "space-bunny-alpha"}, saved.GetModels())
	// A partial edit must retain both auto mappings while simplifying additions.
	callKiloChannelMutation(t, http.MethodPut, map[string]any{"id": saved.Id, "models": saved.Models + ",cline-free/new-model"}, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.Contains(t, saved.GetModels(), "new-model")
	require.Equal(t, "cline-free/deepseek-v4.1-flash", normalizeChannelModelMapping(&saved)["deepseek-v4.1-flash"])
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("model_mapping", saved.GetModelMapping())
	info := &relaycommon.RelayInfo{OriginModelName: "deepseek-v4.1-flash", RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCline, ChannelBaseUrl: upstream.URL, ApiKey: "test-key"}}
	request := &dto.GeneralOpenAIRequest{Model: "deepseek-v4.1-flash"}
	require.NoError(t, helper.ModelMappedHelper(c, info, request))
	body, err := common.Marshal(request)
	require.NoError(t, err)
	response, err := (&openai.Adaptor{}).DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	response.(*http.Response).Body.Close()
	require.Equal(t, "cline-free/deepseek-v4.1-flash", <-captured)
}
