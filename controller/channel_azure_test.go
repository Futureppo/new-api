package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAzureDiscoveryDraftOverridesSavedChannelWithoutSaving(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	service.InitHttpClient()
	fetchSettings := system_setting.GetFetchSetting()
	previous := *fetchSettings
	fetchSettings.EnableSSRFProtection = false // Only the local httptest server is contacted.
	t.Cleanup(func() { *fetchSettings = previous })
	wantKey := "stored-first"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/openai/models", r.URL.Path)
		require.Equal(t, wantKey, r.Header.Get("api-key"))
		require.Equal(t, "draft-version", r.URL.Query().Get("api-version"))
		require.Equal(t, "draft-header", r.Header.Get("X-Test"))
		require.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("x-ms-region", "East US")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1"}]}`))
	}))
	defer upstream.Close()
	channel := model.Channel{Type: constant.ChannelTypeAzure, Name: "azure", Key: "stored-first\nstored-second", BaseURL: common.GetPointer("https://old.invalid"), Other: "old-version", Models: "saved-model", Group: "default"}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AzureRegion: "westus", AzureRegionSource: "resource", AzureResourceID: "saved-resource"})
	require.NoError(t, db.Create(&channel).Error)
	for _, key := range []string{"", " draft-first \n draft-second "} {
		if key != "" {
			wantKey = "draft-first"
		}
		body, err := common.Marshal(azureDiscoveryRequest{ChannelID: &channel.Id, BaseURL: &upstream.URL, Key: key, Other: common.GetPointer("draft-version"), HeaderOverride: common.GetPointer(`{"X-Test":"draft-header"}`)})
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/azure/discover", strings.NewReader(string(body)))
		ctx.Request.Header.Set("Content-Type", "application/json")
		DiscoverAzureChannel(ctx)
		require.Equal(t, 200, recorder.Code)
		var response struct {
			Success bool                         `json:"success"`
			Data    service.AzureDiscoveryResult `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		require.Equal(t, []string{"gpt-4.1"}, response.Data.Models)
		require.Equal(t, "East US", response.Data.Region)
		require.NotContains(t, recorder.Body.String(), wantKey)
	}
	var saved model.Channel
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.Equal(t, channel.Key, saved.Key)
	require.Equal(t, channel.GetBaseURL(), saved.GetBaseURL())
	require.Equal(t, "westus", saved.GetOtherSettings().AzureRegion)

	// A GET can only use persisted configuration; query-string credentials are ignored.
	require.NoError(t, db.Model(&channel).Updates(map[string]any{"base_url": upstream.URL, "other": "draft-version", "header_override": `{"X-Test":"draft-header"}`}).Error)
	wantKey = "stored-first"
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/azure/discover/1?key=ignored&management_token=ignored&target=region", nil)
	DiscoverSavedAzureChannel(ctx)
	require.Equal(t, 200, recorder.Code)
	require.Contains(t, recorder.Body.String(), "East US")
}

func TestAzureDiscoveryRegionSettingsRoundTrip(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	channel := model.Channel{Type: constant.ChannelTypeAzure, Name: "azure-settings", Key: "key", Models: "deployment", Group: "default", Status: common.ChannelStatusEnabled}
	settings := dto.ChannelOtherSettings{AzureResourceID: "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.CognitiveServices/accounts/resource", AzureRegion: "eastus", AzureRegionSource: "resource", AzureResponsesVersion: "preview"}
	channel.SetOtherSettings(settings)
	callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &channel}, AddChannel)
	var saved model.Channel
	require.NoError(t, db.Where("name = ?", channel.Name).First(&saved).Error)
	require.Equal(t, settings, saved.GetOtherSettings())
	settings.AzureRegion = "West Europe"
	settings.AzureRegionSource = "response_header"
	serialized, err := common.Marshal(settings)
	require.NoError(t, err)
	callKiloChannelMutation(t, http.MethodPut, map[string]any{"id": saved.Id, "settings": string(serialized)}, UpdateChannel)
	require.NoError(t, db.First(&saved, saved.Id).Error)
	require.Equal(t, settings, saved.GetOtherSettings())
	require.NotContains(t, saved.OtherSettings, "management_token")
}
