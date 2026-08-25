package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/vercel"
	"github.com/stretchr/testify/require"
)

func TestVercelDefaultModelListURL(t *testing.T) {
	require.Equal(
		t,
		"https://ai-gateway.vercel.sh/v1/models",
		resolveFetchModelsURL(constant.ChannelTypeVercel, constant.ChannelBaseURLs[constant.ChannelTypeVercel], ""),
	)
}

func TestVercelModelListResponseWithMetadata(t *testing.T) {
	body := []byte(`{
		"object":"list",
		"data":[
			{"id":"minimax/minimax-m2.7-free","object":"model","type":"language","tags":["free"],"pricing":{"input":"0","output":"0"}},
			{"id":"openai/text-embedding-3-small","object":"model","type":"embedding","pricing":{"input":"0.00000002"}}
		]
	}`)

	ids, parsed := parseModelIDsFromResponseBody(body)
	require.True(t, parsed)
	require.Equal(t, []string{"minimax/minimax-m2.7-free", "openai/text-embedding-3-small"}, ids)
}

func TestFetchVercelModelsReturnsCompleteUpstreamList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"minimax/minimax-m2.7-free","tags":["free"]},{"id":"openai/gpt-5.4"}]}`))
	}))
	defer upstream.Close()

	channel := &model.Channel{Type: constant.ChannelTypeVercel}
	models, err := fetchChannelModelIDsWithKey(channel, upstream.URL, "test-key", "")
	require.NoError(t, err)
	require.Equal(t, []string{"minimax/minimax-m2.7-free", "openai/gpt-5.4"}, models)
}

func TestVercelDashboardDefaultsToFreeModels(t *testing.T) {
	require.Equal(t, vercel.ModelList, channelId2Models[constant.ChannelTypeVercel])
}
