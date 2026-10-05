package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestSelfHostedModelDiscoveryAndCredentials(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeLiteLLM} {
		for _, key := range []string{"", "test-key"} {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/prefix/v1/models", r.URL.Path)
				want := ""
				if key != "" {
					want = "Bearer " + key
				}
				require.Equal(t, want, r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3.8-27b-fp8"}]}`))
			}))
			base := upstream.URL + "/prefix/v1/chat/completions"
			status, result := runFetchModelsRequest(t, map[string]any{"type": channelType, "base_url": base, "key": key})
			require.Equal(t, 200, status)
			require.True(t, result.Success)
			require.Equal(t, []string{"qwen3.8-27b-fp8"}, result.Data)
			ch := model.Channel{Type: channelType, Name: "selfhost", BaseURL: &base, Key: key, Models: "qwen3.8-27b-fp8", Group: "default"}
			require.NoError(t, validateChannel(&ch, true))
			callKiloChannelMutation(t, "POST", map[string]any{"mode": "single", "channel": ch}, AddChannel)
			upstream.Close()
		}
		ch := model.Channel{Type: channelType, Name: "clear", BaseURL: common.GetPointer("https://host.example/v1"), Key: "old-key", Models: "test", Group: "default"}
		require.NoError(t, db.Create(&ch).Error)
		callKiloChannelMutation(t, "PUT", map[string]any{"id": ch.Id, "type": channelType, "name": "keep", "models": "test", "group": "default"}, UpdateChannel)
		saved, err := model.GetChannelById(ch.Id, true)
		require.NoError(t, err)
		require.Equal(t, "old-key", saved.Key)
		callKiloChannelMutation(t, "PUT", map[string]any{"id": ch.Id, "type": channelType, "clear_key": true, "models": "test", "group": "default"}, UpdateChannel)
		saved, err = model.GetChannelById(ch.Id, true)
		require.NoError(t, err)
		require.Empty(t, saved.Key)
		key, _, apiErr := saved.GetNextEnabledKey()
		require.Nil(t, apiErr)
		require.Empty(t, key)
	}
	for _, base := range []string{"", "/v1", "ftp://invalid"} {
		require.Error(t, validateChannel(&model.Channel{Type: constant.ChannelTypeVLLM, BaseURL: &base, Models: "test"}, true))
	}
}

func TestSelfHostedBatchSkipsBlankKeys(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	ch := model.Channel{Type: constant.ChannelTypeVLLM, Name: "batch", BaseURL: common.GetPointer("https://host.example/v1"), Key: "key-a\n\n  \nkey-b", Models: "test", Group: "default"}
	callKiloChannelMutation(t, "POST", map[string]any{"mode": "batch", "channel": ch}, AddChannel)
	var saved []model.Channel
	require.NoError(t, db.Order("id").Find(&saved).Error)
	require.Len(t, saved, 2)
	require.Equal(t, "key-a", saved[0].Key)
	require.Equal(t, "key-b", saved[1].Key)
	ch.Name = "multi"
	callKiloChannelMutation(t, "POST", map[string]any{"mode": "multi_to_single", "channel": ch}, AddChannel)
	var multi model.Channel
	require.NoError(t, db.Where("name = ?", "multi").First(&multi).Error)
	require.Equal(t, []string{"key-a", "key-b"}, multi.GetKeys())
	require.Error(t, multi.UpdateWithKeyClear(true))
	require.Error(t, (&model.Channel{Type: constant.ChannelTypeOpenAI}).UpdateWithKeyClear(true))
}
