package controller

import (
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

func TestOpenCodeFreeModelSelection(t *testing.T) {
	models := []string{"mimo-v2.5-free", "big-pickle", "muse-spark-1.3-contributor-free", "space-bunny-free",
		" mimo-v2.5-free ", "jev-1.13-free", "claude-free", "gemini-test-free", "qwen-free", "paid-model", ""}
	require.Equal(t, []string{"mimo-v2.5-free", "big-pickle", "muse-spark-1.3-contributor-free", "space-bunny-free"}, filterOpenCodeFreeChatModels(models))
	for _, enabled := range []bool{false, true} {
		settings := dto.ChannelOtherSettings{OpenCodeFreeModelSyncEnabled: enabled}
		for _, channelType := range []int{constant.ChannelTypeOpenCode, constant.ChannelTypeOpenCodeGo, constant.ChannelTypeOpenAI} {
			require.Equal(t, enabled && channelType == constant.ChannelTypeOpenCode,
				isChannelUpstreamModelUpdateEnabled(&model.Channel{Type: channelType}, settings))
		}
		encoded, err := common.Marshal(settings)
		require.NoError(t, err)
		var decoded dto.ChannelOtherSettings
		require.NoError(t, common.Unmarshal(encoded, &decoded))
		require.Equal(t, enabled, decoded.OpenCodeFreeModelSyncEnabled)
	}
}

func TestOpenCodeFreeModelFetchAPIs(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, []string{"/zen/v1/models", "/custom"}, r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"big-pickle"},{"id":"new-free"},{"id":"paid"},{"id":"jev-1.13-free"}]}`))
	}))
	defer upstream.Close()
	for _, enabled := range []bool{false, true} {
		status, response := runFetchModelsRequest(t, map[string]any{
			"type": constant.ChannelTypeOpenCode, "base_url": upstream.URL + "/zen",
			"key": "test-key", "opencode_free_only": enabled,
		})
		require.Equal(t, http.StatusOK, status)
		require.True(t, response.Success)
		want := []string{"big-pickle", "new-free"}
		if !enabled {
			want = append(want, "paid", "jev-1.13-free")
		}
		require.Equal(t, want, response.Data)
	}
	baseURL := upstream.URL + "/zen"
	ch := model.Channel{Type: constant.ChannelTypeOpenCode, Key: "test-key", BaseURL: &baseURL}
	ch.SetOtherSettings(dto.ChannelOtherSettings{OpenCodeFreeModelSyncEnabled: true, CustomModelListURL: upstream.URL + "/custom"})
	require.NoError(t, db.Create(&ch).Error)
	for _, query := range []string{"?opencode_free_only=false", "?opencode_free_only=true", "?opencode_free_only=invalid", ""} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/"+query, nil)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(ch.Id)}}
		FetchUpstreamModels(c)
		var result struct {
			Success bool
			Data    []string
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		if query == "?opencode_free_only=invalid" {
			require.False(t, result.Success)
			continue
		}
		require.True(t, result.Success)
		if query == "?opencode_free_only=false" {
			require.Len(t, result.Data, 4)
		} else {
			require.Len(t, result.Data, 2)
		}
	}
	var saved model.Channel
	require.NoError(t, db.First(&saved, ch.Id).Error)
	require.True(t, saved.GetOtherSettings().OpenCodeFreeModelSyncEnabled, "preview must not persist toggle changes")
}

func TestOpenCodeFreeSyncLifecycle(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	payload := `{"data":[{"id":"new-free"},{"id":"big-pickle"},{"id":"paid"},{"id":"ignored-free"},{"id":"blocked-free"},{"id":"jev-1.13-free"}]}`
	status := http.StatusOK
	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeOpenCode, Key: "test-key", BaseURL: &upstream.URL,
		Name: "opencode-sync", Status: common.ChannelStatusEnabled, Group: "default",
		Models: "paid/manual,old-free,manual-free,jev-1.13-free", ModelMapping: common.GetPointer(`{"manual-free":"custom/upstream"}`)}
	settings := dto.ChannelOtherSettings{OpenCodeFreeModelSyncEnabled: true, UpstreamModelUpdateIgnoredModels: []string{"ignored-free", "regex:^blocked-"}}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	changed, result, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.NoError(t, err)
	require.True(t, changed)
	require.ElementsMatch(t, []string{"new-free", "big-pickle"}, result.AddedModels)
	require.Equal(t, []string{"old-free"}, result.RemovedModels)
	want := []string{"paid/manual", "manual-free", "jev-1.13-free", "new-free", "big-pickle"}
	require.ElementsMatch(t, want, ch.GetModels())
	var saved model.Channel
	require.NoError(t, db.First(&saved, ch.Id).Error)
	require.ElementsMatch(t, want, saved.GetModels())
	var abilities []string
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", ch.Id).Pluck("model", &abilities).Error)
	require.ElementsMatch(t, want, abilities)
	require.Equal(t, map[string]string{"manual-free": "custom/upstream"}, normalizeChannelModelMapping(&saved))
	_, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, false, true)
	require.NoError(t, err)
	require.Equal(t, 1, requests, "respect the existing check interval")
	for _, invalid := range []string{`{"data":[]}`, `{"data":[{"id":"paid"}]}`, `{}`, "invalid"} {
		payload = invalid
		changed, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
		require.Error(t, err)
		require.False(t, changed)
		require.ElementsMatch(t, want, ch.GetModels())
	}
	status = http.StatusForbidden
	changed, _, err = checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, true)
	require.Error(t, err)
	require.False(t, changed)
	require.ElementsMatch(t, want, ch.GetModels())
}

func TestOpenCodeDetectionAndManualApplyProtectMappings(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"new-free"},{"id":"ignore-free"},{"id":"paid"}]}`))
	}))
	defer upstream.Close()
	ch := model.Channel{Type: constant.ChannelTypeOpenCode, Key: "test-key", BaseURL: &upstream.URL, Models: "old-free,paid/manual", Group: "default"}
	settings := dto.ChannelOtherSettings{OpenCodeFreeModelSyncEnabled: true}
	ch.SetOtherSettings(settings)
	require.NoError(t, db.Create(&ch).Error)
	changed, _, err := checkAndPersistChannelUpstreamModelUpdates(&ch, &settings, true, false)
	require.NoError(t, err)
	require.False(t, changed)
	require.ElementsMatch(t, []string{"new-free", "ignore-free"}, settings.UpstreamModelUpdateLastDetectedModels)
	require.Equal(t, []string{"old-free"}, settings.UpstreamModelUpdateLastRemovedModels)
	// Stale UI payloads may contain paid changes and newly mapped aliases.
	settings.UpstreamModelUpdateLastDetectedModels = append(settings.UpstreamModelUpdateLastDetectedModels, "paid")
	settings.UpstreamModelUpdateLastRemovedModels = append(settings.UpstreamModelUpdateLastRemovedModels, "paid/manual")
	ch.SetOtherSettings(settings)
	ch.ModelMapping = common.GetPointer(`{"old-free":"custom/target"}`)
	add, remove := collectPendingApplyUpstreamModelChanges(&ch, settings)
	require.ElementsMatch(t, []string{"new-free", "ignore-free"}, add)
	require.Empty(t, remove)
	_, removed, _, _, changed, err := applyChannelUpstreamModelUpdates(&ch, []string{"new-free", "paid"}, []string{"ignore-free"}, []string{"old-free", "paid/manual"})
	require.NoError(t, err)
	require.True(t, changed)
	require.Empty(t, removed)
	require.ElementsMatch(t, []string{"old-free", "paid/manual", "new-free"}, ch.GetModels())
	require.Contains(t, ch.GetOtherSettings().UpstreamModelUpdateIgnoredModels, "ignore-free")
}

func TestOpenCodeFreeSyncTogglePersistsAndClearsStaleDetection(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	ch := model.Channel{Type: constant.ChannelTypeOpenCode, Key: "test-key", Name: "opencode-toggle", Models: "big-pickle", Group: "default", Status: common.ChannelStatusEnabled}
	ch.SetOtherSettings(dto.ChannelOtherSettings{UpstreamModelUpdateLastDetectedModels: []string{"paid"}, UpstreamModelUpdateLastCheckTime: 123})
	require.NoError(t, db.Create(&ch).Error)
	for _, enabled := range []bool{true, false, true} {
		settings := ch.GetOtherSettings()
		settings.OpenCodeFreeModelSyncEnabled = enabled
		ch.SetOtherSettings(settings)
		callKiloChannelMutation(t, http.MethodPut, ch, UpdateChannel)
		var saved model.Channel
		require.NoError(t, db.First(&saved, ch.Id).Error)
		require.Equal(t, enabled, saved.GetOtherSettings().OpenCodeFreeModelSyncEnabled)
		require.Empty(t, saved.GetOtherSettings().UpstreamModelUpdateLastDetectedModels)
		require.Zero(t, saved.GetOtherSettings().UpstreamModelUpdateLastCheckTime)
		ch = saved
	}
}
