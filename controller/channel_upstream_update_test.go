package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestNormalizeModelNames(t *testing.T) {
	result := normalizeModelNames([]string{
		" gpt-4o ",
		"",
		"gpt-4o",
		"gpt-4.1",
		"   ",
	})

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
}

func TestIsOpenRouterManagedFreeOrAlphaModel(t *testing.T) {
	testCases := []struct {
		name      string
		modelName string
		expected  bool
	}{
		{name: "free suffix", modelName: "google/gemma:free", expected: true},
		{name: "free router", modelName: "openrouter/free", expected: true},
		{name: "openrouter alpha", modelName: "openrouter/owl-alpha", expected: true},
		{name: "case and whitespace", modelName: " OpenRouter/Elephant-Alpha ", expected: true},
		{name: "other provider alpha", modelName: "vendor/owl-alpha", expected: true},
		{name: "other provider case and whitespace", modelName: " Vendor/OWL-ALPHA ", expected: true},
		{name: "nested openrouter alpha", modelName: "openrouter/vendor/owl-alpha", expected: true},
		{name: "nested provider alpha", modelName: "vendor/family/owl-alpha", expected: true},
		{name: "alpha without provider", modelName: "owl-alpha", expected: true},
		{name: "alpha without hyphen", modelName: "vendor/modelalpha", expected: false},
		{name: "alpha with underscore", modelName: "vendor/model_alpha", expected: false},
		{name: "alpha followed by version", modelName: "vendor/model-alpha-v2", expected: false},
		{name: "alpha prefix", modelName: "vendor/alpha-model", expected: false},
		{name: "alpha in provider only", modelName: "vendor-alpha/model", expected: false},
		{name: "empty alpha name", modelName: "openrouter/-alpha", expected: false},
		{name: "empty provider alpha name", modelName: "vendor/family/-alpha", expected: false},
		{name: "alpha suffix only", modelName: "-alpha", expected: false},
		{name: "blank alpha name", modelName: "vendor/ -alpha", expected: false},
		{name: "empty model", modelName: "", expected: false},
		{name: "blank model", modelName: " \t ", expected: false},
		{name: "paid model", modelName: "openrouter/auto", expected: false},
		{name: "zero price without marker", modelName: "google/lyria-preview", expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.expected, isOpenRouterManagedFreeOrAlphaModel(testCase.modelName))
		})
	}
}

func TestSimplifyOpenRouterFreeModelName(t *testing.T) {
	testCases := []struct {
		modelName   string
		expected    string
		canSimplify bool
	}{
		{modelName: "nvidia/nemotron-3-super:free", expected: "nemotron-3-super", canSimplify: true},
		{modelName: "openai/gpt-oss-20b:free", expected: "gpt-oss-20b", canSimplify: true},
		{modelName: "openrouter/free", canSimplify: false},
		{modelName: "openrouter/owl-alpha", canSimplify: false},
		{modelName: "vendor/owl-alpha", canSimplify: false},
		{modelName: "model-without-provider:free", canSimplify: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.modelName, func(t *testing.T) {
			actual, ok := simplifyOpenRouterFreeModelName(testCase.modelName)
			require.Equal(t, testCase.canSimplify, ok)
			require.Equal(t, testCase.expected, actual)
		})
	}
}

func TestBuildOpenRouterManagedModelPlanSimplifiesOnlyFreeModels(t *testing.T) {
	manualMapping := `{"manual-alias":"paid/upstream"}`
	channel := &model.Channel{
		Models:       "paid/local",
		ModelMapping: &manualMapping,
	}

	plan := buildOpenRouterManagedModelPlan(
		channel,
		[]string{
			"nvidia/nemotron-3-super:free",
			"openai/gpt-oss-20b:free",
			"openrouter/free",
			"openrouter/owl-alpha",
			"vendor/owl-alpha",
		},
		true,
		nil,
	)

	require.Equal(t, []string{
		"nemotron-3-super",
		"gpt-oss-20b",
		"openrouter/free",
		"openrouter/owl-alpha",
		"vendor/owl-alpha",
	}, plan.DesiredModels)
	require.Equal(t, map[string]string{
		"nemotron-3-super": "nvidia/nemotron-3-super:free",
		"gpt-oss-20b":      "openai/gpt-oss-20b:free",
	}, plan.DesiredMappings)
	require.Equal(t, map[string]string{"manual-alias": "paid/upstream"}, plan.PreservedModelMappings)
}

func TestBuildOpenRouterManagedModelPlanFallsBackOnAliasConflicts(t *testing.T) {
	t.Run("duplicate simplified names", func(t *testing.T) {
		plan := buildOpenRouterManagedModelPlan(
			&model.Channel{},
			[]string{"vendor-a/shared:free", "vendor-b/shared:free"},
			true,
			nil,
		)
		require.Equal(t, []string{"vendor-a/shared:free", "vendor-b/shared:free"}, plan.DesiredModels)
		require.Empty(t, plan.DesiredMappings)
	})

	t.Run("manual local model", func(t *testing.T) {
		plan := buildOpenRouterManagedModelPlan(
			&model.Channel{Models: "shared"},
			[]string{"vendor/shared:free"},
			true,
			nil,
		)
		require.Equal(t, []string{"vendor/shared:free"}, plan.DesiredModels)
		require.Empty(t, plan.DesiredMappings)
	})

	t.Run("manual mapping", func(t *testing.T) {
		manualMapping := `{"shared":"paid/upstream"}`
		plan := buildOpenRouterManagedModelPlan(
			&model.Channel{ModelMapping: &manualMapping},
			[]string{"vendor/shared:free"},
			true,
			nil,
		)
		require.Equal(t, []string{"vendor/shared:free"}, plan.DesiredModels)
		require.Empty(t, plan.DesiredMappings)
	})

	t.Run("manual mapping matching generated shape", func(t *testing.T) {
		manualMapping := `{"shared":"vendor/shared:free"}`
		plan := buildOpenRouterManagedModelPlan(
			&model.Channel{ModelMapping: &manualMapping},
			[]string{"vendor/shared:free"},
			true,
			nil,
		)
		require.Equal(t, []string{"vendor/shared:free"}, plan.DesiredModels)
		require.Empty(t, plan.DesiredMappings)
		require.Equal(t, map[string]string{"shared": "vendor/shared:free"}, plan.PreservedModelMappings)
	})
}

func TestMergeModelNames(t *testing.T) {
	result := mergeModelNames(
		[]string{"gpt-4o", "gpt-4.1"},
		[]string{"gpt-4.1", " gpt-4.1-mini ", "gpt-4o"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"}, result)
}

func TestSubtractModelNames(t *testing.T) {
	result := subtractModelNames(
		[]string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"},
		[]string{"gpt-4.1", "not-exists"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1-mini"}, result)
}

func TestIntersectModelNames(t *testing.T) {
	result := intersectModelNames(
		[]string{"gpt-4o", "gpt-4.1", "gpt-4.1", "not-exists"},
		[]string{"gpt-4.1", "gpt-4o-mini", "gpt-4o"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
}

func TestApplySelectedModelChanges(t *testing.T) {
	t.Run("add and remove together", func(t *testing.T) {
		result := applySelectedModelChanges(
			[]string{"gpt-4o", "gpt-4.1", "claude-3"},
			[]string{"gpt-4.1-mini"},
			[]string{"claude-3"},
		)

		require.Equal(t, []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"}, result)
	})

	t.Run("add wins when conflict with remove", func(t *testing.T) {
		result := applySelectedModelChanges(
			[]string{"gpt-4o"},
			[]string{"gpt-4.1"},
			[]string{"gpt-4.1"},
		)

		require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
	})
}

func TestCollectPendingApplyUpstreamModelChanges(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		UpstreamModelUpdateLastDetectedModels: []string{" gpt-4o ", "gpt-4o", "gpt-4.1"},
		UpstreamModelUpdateLastRemovedModels:  []string{" old-model ", "", "old-model"},
	}

	pendingAddModels, pendingRemoveModels := collectPendingApplyUpstreamModelChanges(&model.Channel{}, settings)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, pendingAddModels)
	require.Equal(t, []string{"old-model"}, pendingRemoveModels)
}

func TestNormalizeChannelModelMapping(t *testing.T) {
	modelMapping := `{
		" alias-model ": " upstream-model ",
		"": "invalid",
		"invalid-target": ""
	}`
	channel := &model.Channel{
		ModelMapping: &modelMapping,
	}

	result := normalizeChannelModelMapping(channel)
	require.Equal(t, map[string]string{
		"alias-model": "upstream-model",
	}, result)
}

func TestCollectPendingUpstreamModelChangesFromModels_WithModelMapping(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
		[]string{"alias-model", "gpt-4o", "stale-model"},
		[]string{"gpt-4o", "gpt-4.1", "mapped-target"},
		[]string{"gpt-4.1"},
		map[string]string{
			"alias-model": "mapped-target",
		},
	)

	require.Equal(t, []string{}, pendingAddModels)
	require.Equal(t, []string{"stale-model"}, pendingRemoveModels)
}

func TestCollectPendingUpstreamModelChangesFromModels_WithIgnoredRegexPatterns(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
		[]string{"gpt-4o"},
		[]string{"gpt-4o", "claude-3-5-sonnet", "sora-video", "gpt-4.1"},
		[]string{"regex:^sora-.*$", "gpt-4.1"},
		nil,
	)

	require.Equal(t, []string{"claude-3-5-sonnet"}, pendingAddModels)
	require.Equal(t, []string{}, pendingRemoveModels)
}

func TestCollectPendingOpenRouterManagedModelChangesFromModels(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingOpenRouterManagedModelChangesFromModels(
		[]string{
			"paid/model",
			"vendor/old:free",
			"openrouter/owl-alpha",
			"free-alias",
		},
		openRouterManagedModelPlan{
			DesiredModels: []string{
				"vendor/new:free",
				"openrouter/free",
				"openrouter/elephant-alpha",
				"vendor/mapped:free",
			},
			PreservedModelMappings: map[string]string{"free-alias": "vendor/mapped:free"},
		},
		[]string{"vendor/new:free"},
	)

	require.Equal(t, []string{"openrouter/free", "openrouter/elephant-alpha"}, pendingAddModels)
	require.Equal(t, []string{"vendor/old:free", "openrouter/owl-alpha"}, pendingRemoveModels)
}

func TestCollectPendingOpenRouterManagedModelChangesHonorsOriginalModelIgnoreRule(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingOpenRouterManagedModelChangesFromModels(
		nil,
		openRouterManagedModelPlan{
			DesiredModels:          []string{"shared"},
			DesiredMappings:        map[string]string{"shared": "vendor/shared:free"},
			PreservedModelMappings: map[string]string{},
		},
		[]string{"vendor/shared:free"},
	)

	require.Empty(t, pendingAddModels)
	require.Empty(t, pendingRemoveModels)
}

func TestFetchOpenRouterManagedFreeAndAlphaModelIDs(t *testing.T) {
	t.Run("uses authenticated user models endpoint and filters models", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v1/models/user", r.URL.Path)
			require.Equal(t, "all", r.URL.Query().Get("output_modalities"))
			require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"data":[{"id":"paid/model"},{"id":"vendor/model:free"},{"id":"openrouter/free"},{"id":"openrouter/owl-alpha"},{"id":"vendor/model:free"}]}`))
		}))
		defer upstream.Close()

		channel := &model.Channel{Type: constant.ChannelTypeOpenRouter}
		models, err := fetchOpenRouterManagedFreeAndAlphaModelIDs(channel, upstream.URL, "test-key", "")

		require.NoError(t, err)
		require.Equal(t, []string{"vendor/model:free", "openrouter/free", "openrouter/owl-alpha"}, models)
	})

	t.Run("accepts only other provider alpha models and preserves IDs", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":" Vendor/OWL-ALPHA "},{"id":"vendor/family/owl-alpha"},{"id":"owl-alpha"},{"id":"Vendor/OWL-ALPHA"},{"id":"vendor/modelalpha"},{"id":"vendor/model_alpha"},{"id":"vendor/model-alpha-v2"}]}`))
		}))
		defer upstream.Close()

		channel := &model.Channel{Type: constant.ChannelTypeOpenRouter}
		models, err := fetchOpenRouterManagedFreeAndAlphaModelIDs(channel, upstream.URL, "test-key", "")

		require.NoError(t, err)
		require.Equal(t, []string{"Vendor/OWL-ALPHA", "vendor/family/owl-alpha", "owl-alpha"}, models)
	})

	t.Run("uses custom model list URL", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/custom/models", r.URL.Path)
			require.Equal(t, "output_modalities=embeddings", r.URL.RawQuery)
			_, _ = w.Write([]byte(`{"data":[{"id":"custom/model:free"}]}`))
		}))
		defer upstream.Close()

		channel := &model.Channel{Type: constant.ChannelTypeOpenRouter}
		models, err := fetchOpenRouterManagedFreeAndAlphaModelIDs(
			channel,
			"https://unused.example.com",
			"test-key",
			upstream.URL+"/custom/models?output_modalities=embeddings",
		)

		require.NoError(t, err)
		require.Equal(t, []string{"custom/model:free"}, models)
	})

	t.Run("rejects empty managed model result", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"id":"paid/model"}]}`))
		}))
		defer upstream.Close()

		channel := &model.Channel{Type: constant.ChannelTypeOpenRouter}
		models, err := fetchOpenRouterManagedFreeAndAlphaModelIDs(channel, upstream.URL, "test-key", "")

		require.Error(t, err)
		require.Nil(t, models)
	})

	t.Run("returns HTTP errors", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
		}))
		defer upstream.Close()

		channel := &model.Channel{Type: constant.ChannelTypeOpenRouter}
		models, err := fetchOpenRouterManagedFreeAndAlphaModelIDs(channel, upstream.URL, "test-key", "")

		require.Error(t, err)
		require.Nil(t, models)
	})

	t.Run("returns response parsing errors", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"unexpected":true}`))
		}))
		defer upstream.Close()

		channel := &model.Channel{Type: constant.ChannelTypeOpenRouter}
		models, err := fetchOpenRouterManagedFreeAndAlphaModelIDs(channel, upstream.URL, "test-key", "")

		require.Error(t, err)
		require.Nil(t, models)
	})
}

func TestCheckAndPersistOpenRouterManagedModelUpdates(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"paid/upstream"},{"id":"vendor/new:free"},{"id":"openrouter/free"},{"id":"vendor/elephant-alpha"},{"id":"vendor/ignored-alpha"}]}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	manualMapping := `{"vendor/manual-alpha":"paid/upstream"}`
	channel := model.Channel{
		Type:         constant.ChannelTypeOpenRouter,
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		Name:         "openrouter-managed-models",
		BaseURL:      &baseURL,
		Models:       "paid/local,vendor/old:free,vendor/owl-alpha,vendor/manual-alpha",
		ModelMapping: &manualMapping,
		Group:        "default",
	}
	settings := dto.ChannelOtherSettings{
		OpenRouterFreeAlphaSyncEnabled:   true,
		UpstreamModelUpdateIgnoredModels: []string{"vendor/ignored-alpha"},
	}
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))

	modelsChanged, autoApplyResult, err := checkAndPersistChannelUpstreamModelUpdates(
		&channel,
		&settings,
		true,
		true,
	)

	require.NoError(t, err)
	require.True(t, modelsChanged)
	require.Equal(t, []string{"vendor/new:free", "openrouter/free", "vendor/elephant-alpha"}, autoApplyResult.AddedModels)
	require.Equal(t, []string{"vendor/old:free", "vendor/owl-alpha"}, autoApplyResult.RemovedModels)
	require.Equal(t, "paid/local,vendor/manual-alpha,vendor/new:free,openrouter/free,vendor/elephant-alpha", channel.Models)
	require.Empty(t, settings.UpstreamModelUpdateLastDetectedModels)
	require.Empty(t, settings.UpstreamModelUpdateLastRemovedModels)

	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, channel.Models, reloaded.Models)
	require.Equal(t, map[string]string{"vendor/manual-alpha": "paid/upstream"}, normalizeChannelModelMapping(&reloaded))
	var abilityModels []string
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Order("model asc").Pluck("model", &abilityModels).Error)
	require.Equal(t, []string{"openrouter/free", "paid/local", "vendor/elephant-alpha", "vendor/manual-alpha", "vendor/new:free"}, abilityModels)
}

func TestCheckAndPersistOpenRouterManagedEmbeddingModels(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	const liquid = "liquid/lfm-2.5-embedding-350m:free"
	const nvidia = "nvidia/nemotron-3-embed-1b:free"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/proxy/v1/models/user", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		// OpenRouter defaults to text output when no modality filter is supplied.
		models := []string{"vendor/chat:free", "vendor/test-alpha"}
		modalities := r.URL.Query().Get("output_modalities")
		if modalities == "all" || strings.Contains(modalities, "embeddings") {
			models = append(models, liquid, nvidia, "vendor/paid-embedding")
		}
		data := make([]map[string]string, 0, len(models))
		for _, id := range models {
			data = append(data, map[string]string{"id": id})
		}
		body, err := common.Marshal(map[string]any{"data": data})
		require.NoError(t, err)
		_, _ = w.Write(body)
	}))
	defer upstream.Close()

	baseURL := upstream.URL + "/proxy/v1/"
	channel := model.Channel{
		Type: constant.ChannelTypeOpenRouter, Key: "test-key",
		Status: common.ChannelStatusEnabled, Name: "openrouter-free-embeddings",
		BaseURL: &baseURL, Models: "paid/local," + liquid, Group: "default",
	}
	settings := dto.ChannelOtherSettings{OpenRouterFreeAlphaSyncEnabled: true}
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))

	changed, result, err := checkAndPersistChannelUpstreamModelUpdates(&channel, &settings, true, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"vendor/chat:free", "vendor/test-alpha", nvidia}, result.AddedModels)
	require.Empty(t, result.RemovedModels)

	expected := []string{"paid/local", liquid, "vendor/chat:free", "vendor/test-alpha", nvidia}
	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.ElementsMatch(t, expected, reloaded.GetModels())
	var abilityModels []string
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Pluck("model", &abilityModels).Error)
	require.ElementsMatch(t, expected, abilityModels)
}

func TestCheckAndPersistOpenRouterManagedModelUpdatesPreservesModelsOnEmptyResult(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"paid/upstream"}]}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := model.Channel{
		Type:    constant.ChannelTypeOpenRouter,
		Key:     "test-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "openrouter-empty-managed-models",
		BaseURL: &baseURL,
		Models:  "paid/local,vendor/existing:free",
		Group:   "default",
	}
	settings := dto.ChannelOtherSettings{OpenRouterFreeAlphaSyncEnabled: true}
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(&channel).Error)

	modelsChanged, autoApplyResult, err := checkAndPersistChannelUpstreamModelUpdates(
		&channel,
		&settings,
		true,
		true,
	)

	require.Error(t, err)
	require.False(t, modelsChanged)
	require.Empty(t, autoApplyResult.AddedModels)
	require.Empty(t, autoApplyResult.RemovedModels)
	require.Equal(t, "paid/local,vendor/existing:free", channel.Models)
	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, channel.Models, reloaded.Models)
}

func TestCheckAndPersistOpenRouterManagedModelUpdatesSimplifiesFreeModelNames(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"nvidia/nemotron-3-super:free"},{"id":"openai/gpt-oss-20b:free"},{"id":"openrouter/free"},{"id":"openrouter/elephant-alpha"}]}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	testModel := "openrouter/elephant-alpha"
	manualMapping := `{"manual-alias":"paid/upstream"}`
	channel := model.Channel{
		Type:         constant.ChannelTypeOpenRouter,
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		Name:         "openrouter-simplified-models",
		BaseURL:      &baseURL,
		Models:       "paid/local,nvidia/old-model:free",
		ModelMapping: &manualMapping,
		TestModel:    &testModel,
		Group:        "default",
	}
	settings := dto.ChannelOtherSettings{
		OpenRouterFreeAlphaSyncEnabled:               true,
		OpenRouterFreeModelNameSimplificationEnabled: true,
	}
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(&channel).Error)

	modelsChanged, autoApplyResult, err := checkAndPersistChannelUpstreamModelUpdates(
		&channel,
		&settings,
		true,
		true,
	)

	require.NoError(t, err)
	require.True(t, modelsChanged)
	require.Equal(t, []string{"nemotron-3-super", "gpt-oss-20b", "openrouter/free", "openrouter/elephant-alpha"}, autoApplyResult.AddedModels)
	require.Equal(t, []string{"nvidia/old-model:free"}, autoApplyResult.RemovedModels)
	require.Equal(t, "paid/local,nemotron-3-super,gpt-oss-20b,openrouter/free,openrouter/elephant-alpha", channel.Models)
	require.Equal(t, map[string]string{
		"manual-alias":     "paid/upstream",
		"nemotron-3-super": "nvidia/nemotron-3-super:free",
		"gpt-oss-20b":      "openai/gpt-oss-20b:free",
	}, normalizeChannelModelMapping(&channel))
	require.Equal(t, map[string]string{
		"nemotron-3-super": "nvidia/nemotron-3-super:free",
		"gpt-oss-20b":      "openai/gpt-oss-20b:free",
	}, settings.OpenRouterFreeModelGeneratedMappings)
	require.Nil(t, settings.OpenRouterFreeModelPendingMappings)

	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, channel.Models, reloaded.Models)
	require.Equal(t, normalizeChannelModelMapping(&channel), normalizeChannelModelMapping(&reloaded))
	require.NotNil(t, reloaded.TestModel)
	require.Equal(t, testModel, *reloaded.TestModel)

	modelsChanged, autoApplyResult, err = checkAndPersistChannelUpstreamModelUpdates(
		&channel,
		&settings,
		true,
		true,
	)
	require.NoError(t, err)
	require.False(t, modelsChanged)
	require.Empty(t, autoApplyResult.AddedModels)
	require.Empty(t, autoApplyResult.RemovedModels)
}

func TestCheckAndPersistOpenRouterManagedModelUpdatesRestoresFullNamesWhenSimplificationDisabled(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"nvidia/nemotron-3-super:free"},{"id":"openrouter/owl-alpha"}]}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	modelMapping := `{"nemotron-3-super":"nvidia/nemotron-3-super:free","manual-alias":"paid/upstream"}`
	channel := model.Channel{
		Type:         constant.ChannelTypeOpenRouter,
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		Name:         "openrouter-restored-models",
		BaseURL:      &baseURL,
		Models:       "paid/local,nemotron-3-super,openrouter/owl-alpha",
		ModelMapping: &modelMapping,
		Group:        "default",
	}
	settings := dto.ChannelOtherSettings{
		OpenRouterFreeAlphaSyncEnabled: true,
		OpenRouterFreeModelGeneratedMappings: map[string]string{
			"nemotron-3-super": "nvidia/nemotron-3-super:free",
		},
	}
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(&channel).Error)

	modelsChanged, _, err := checkAndPersistChannelUpstreamModelUpdates(
		&channel,
		&settings,
		true,
		true,
	)

	require.NoError(t, err)
	require.True(t, modelsChanged)
	require.Equal(t, "paid/local,openrouter/owl-alpha,nvidia/nemotron-3-super:free", channel.Models)
	require.Equal(t, map[string]string{"manual-alias": "paid/upstream"}, normalizeChannelModelMapping(&channel))
	require.Empty(t, settings.OpenRouterFreeModelGeneratedMappings)
}

func TestApplyChannelUpstreamModelUpdatesAppliesPendingSimplifiedMapping(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"nvidia/nemotron-3-super:free"},{"id":"vendor/owl-alpha"}]}`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := model.Channel{
		Type:    constant.ChannelTypeOpenRouter,
		Key:     "test-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "openrouter-manual-simplified-models",
		BaseURL: &baseURL,
		Models:  "paid/local,vendor/old-alpha",
		Group:   "default",
	}
	settings := dto.ChannelOtherSettings{
		OpenRouterFreeAlphaSyncEnabled:               true,
		OpenRouterFreeModelNameSimplificationEnabled: true,
	}
	channel.SetOtherSettings(settings)
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.UpdateAbilities(nil))

	_, _, err := checkAndPersistChannelUpstreamModelUpdates(&channel, &settings, true, false)
	require.NoError(t, err)
	require.Equal(t, []string{"nemotron-3-super", "vendor/owl-alpha"}, settings.UpstreamModelUpdateLastDetectedModels)
	require.Equal(t, []string{"vendor/old-alpha"}, settings.UpstreamModelUpdateLastRemovedModels)
	require.Equal(t, map[string]string{"nemotron-3-super": "nvidia/nemotron-3-super:free"}, settings.OpenRouterFreeModelPendingMappings)

	// Bulk apply must select the same models, including Alpha models from other providers.
	pendingAddModels, pendingRemoveModels := collectPendingApplyUpstreamModelChanges(&channel, channel.GetOtherSettings())
	require.Equal(t, settings.UpstreamModelUpdateLastDetectedModels, pendingAddModels)
	require.Equal(t, settings.UpstreamModelUpdateLastRemovedModels, pendingRemoveModels)

	addedModels, removedModels, remainingModels, remainingRemoveModels, modelsChanged, err := applyChannelUpstreamModelUpdates(
		&channel,
		pendingAddModels,
		nil,
		pendingRemoveModels,
	)

	require.NoError(t, err)
	require.True(t, modelsChanged)
	require.Equal(t, []string{"nemotron-3-super", "vendor/owl-alpha"}, addedModels)
	require.Equal(t, []string{"vendor/old-alpha"}, removedModels)
	require.Empty(t, remainingModels)
	require.Empty(t, remainingRemoveModels)
	require.Equal(t, "paid/local,nemotron-3-super,vendor/owl-alpha", channel.Models)
	require.Equal(t, map[string]string{"nemotron-3-super": "nvidia/nemotron-3-super:free"}, normalizeChannelModelMapping(&channel))
	require.Equal(t, map[string]string{"nemotron-3-super": "nvidia/nemotron-3-super:free"}, channel.GetOtherSettings().OpenRouterFreeModelGeneratedMappings)

	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, channel.Models, reloaded.Models)
	var abilityModels []string
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Order("model asc").Pluck("model", &abilityModels).Error)
	require.Equal(t, []string{"nemotron-3-super", "paid/local", "vendor/owl-alpha"}, abilityModels)
}

func TestBuildUpstreamModelUpdateTaskNotificationContent_OmitOverflowDetails(t *testing.T) {
	channelSummaries := make([]upstreamModelUpdateChannelSummary, 0, 12)
	for i := 0; i < 12; i++ {
		channelSummaries = append(channelSummaries, upstreamModelUpdateChannelSummary{
			ChannelName: "channel-" + string(rune('A'+i)),
			AddCount:    i + 1,
			RemoveCount: i,
		})
	}

	content := buildUpstreamModelUpdateTaskNotificationContent(
		24,
		12,
		56,
		21,
		9,
		4,
		[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		channelSummaries,
		[]string{
			"gpt-4.1", "gpt-4.1-mini", "o3", "o4-mini", "gemini-2.5-pro", "claude-3.7-sonnet",
			"qwen-max", "deepseek-r1", "llama-3.3-70b", "mistral-large", "command-r-plus", "doubao-pro-32k",
			"hunyuan-large",
		},
		[]string{
			"gpt-3.5-turbo", "claude-2.1", "gemini-1.5-pro", "mixtral-8x7b", "qwen-plus", "glm-4",
			"yi-large", "moonshot-v1", "doubao-lite",
		},
	)

	require.Contains(t, content, "其余 4 个渠道已省略")
	require.Contains(t, content, "其余 1 个已省略")
	require.Contains(t, content, "失败渠道 ID（展示 10/12）")
	require.Contains(t, content, "其余 2 个已省略")
}

func TestShouldSendUpstreamModelUpdateNotification(t *testing.T) {
	channelUpstreamModelUpdateNotifyState.Lock()
	channelUpstreamModelUpdateNotifyState.lastNotifiedAt = 0
	channelUpstreamModelUpdateNotifyState.lastChangedChannels = 0
	channelUpstreamModelUpdateNotifyState.lastFailedChannels = 0
	channelUpstreamModelUpdateNotifyState.Unlock()

	baseTime := int64(2000000)

	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime, 6, 0))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+3600, 6, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+3600, 7, 0))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+7200, 7, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+8000, 0, 3))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+9000, 0, 3))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+10000, 0, 4))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+90000, 7, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+90001, 0, 0))
}
