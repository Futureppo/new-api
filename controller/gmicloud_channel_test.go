package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	"github.com/stretchr/testify/require"
)

func TestGMICloudDefaultModelListURL(t *testing.T) {
	require.Equal(
		t,
		"https://api.gmi-serving.com/v1/models",
		resolveFetchModelsURL(constant.ChannelTypeGMICloud, constant.ChannelBaseURLs[constant.ChannelTypeGMICloud], ""),
	)
}

func TestFetchGMICloudModelsDeduplicatesUpstreamList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"MiniMaxAI/MiniMax-M3"},{"id":"MiniMaxAI/MiniMax-M2.7"},{"id":"MiniMaxAI/MiniMax-M3"}]}`))
	}))
	defer upstream.Close()

	channel := &model.Channel{Type: constant.ChannelTypeGMICloud}
	models, err := fetchChannelModelIDsWithKey(channel, upstream.URL, "test-key", "")
	require.NoError(t, err)
	require.Equal(t, []string{"MiniMaxAI/MiniMax-M3", "MiniMaxAI/MiniMax-M2.7"}, models)
}

func TestGMICloudDashboardDefaultsToFreeModels(t *testing.T) {
	require.Equal(t, gmicloud.ModelList, channelId2Models[constant.ChannelTypeGMICloud])
}
