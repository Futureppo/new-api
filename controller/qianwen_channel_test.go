package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relay/channel/qianwen"
	"github.com/stretchr/testify/require"
)

func TestQianwenRegistrationAndModelDiscovery(t *testing.T) {
	require.Len(t, constant.ChannelBaseURLs, constant.ChannelTypeDummy)
	apiType, ok := common.ChannelType2APIType(constant.ChannelTypeQianwenAI)
	require.True(t, ok)
	require.Equal(t, constant.APITypeQianwenAI, apiType)
	require.IsType(t, &qianwen.Adaptor{}, relay.GetAdaptor(apiType))
	require.Equal(t, qianwen.ModelList, channelId2Models[constant.ChannelTypeQianwenAI])
	for _, name := range []string{"qwen3.8-max", "kimi-k3", "glm-5.3", "future-model"} {
		require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAIResponse}, common.GetEndpointTypesByChannelType(constant.ChannelTypeQianwenAI, name))
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer upstream-test-key", r.Header.Get("Authorization"))
		require.Equal(t, "override", r.Header.Get("X-Test"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/custom" {
			_, _ = w.Write([]byte(`{"data":[{"id":"custom-model"}]}`))
			return
		}
		require.Equal(t, "/prefix/compatible-mode/v1/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3.7-plus"},{"id":"kimi-k3"},{"id":"qwen3.7-plus"},{"id":" "}]}`))
	}))
	defer upstream.Close()
	ch := &model.Channel{Type: constant.ChannelTypeQianwenAI, Key: "upstream-test-key", BaseURL: common.GetPointer(upstream.URL + "/prefix/apps/anthropic/"), Models: "qwen3.7-plus", HeaderOverride: common.GetPointer(`{"X-Test":"override"}`)}
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), ch.Key, "")
	require.NoError(t, err)
	require.Equal(t, []string{"qwen3.7-plus", "kimi-k3"}, names)
	add, remove, _, err := collectPendingUpstreamModelChanges(ch, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	require.Equal(t, []string{"kimi-k3"}, add)
	require.Empty(t, remove)
	names, err = fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), ch.Key, upstream.URL+"/custom")
	require.NoError(t, err)
	require.Equal(t, []string{"custom-model"}, names)
}

func TestQianwenModelDiscoveryLive(t *testing.T) {
	key := os.Getenv("QIANWEN_API_KEY")
	if key == "" {
		t.Skip("set QIANWEN_API_KEY to verify the real model list")
	}
	_, _, ch := setupQianwenGateway(t, "https://maas.qianwenaiapi.com", key)
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), key, "")
	require.NoError(t, err)
	require.NotEmpty(t, names)
}

func TestQianwenModelDiscoveryErrors(t *testing.T) {
	for _, status := range []int{401, 404, 429} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"unavailable"}}`))
		}))
		names, err := fetchChannelModelIDsWithKey(&model.Channel{Type: constant.ChannelTypeQianwenAI}, upstream.URL, "bad-key", "")
		require.Error(t, err)
		require.Empty(t, names)
		upstream.Close()
	}
}

func TestQianwenChannelDefaults(t *testing.T) {
	for _, add := range []bool{false, true} {
		for _, configured := range []string{"", "kimi-k3"} {
			ch := &model.Channel{Type: constant.ChannelTypeQianwenAI, Key: "key", TestModel: common.GetPointer(configured), BaseURL: common.GetPointer("https://proxy.example/prefix/compatible-mode/v1/responses/")}
			require.NoError(t, validateChannel(ch, add))
			require.Equal(t, "https://proxy.example/prefix", *ch.BaseURL)
			want := configured
			if add && want == "" {
				want = qianwen.DefaultTestModel
			}
			require.Equal(t, want, *ch.TestModel)
			require.Equal(t, "openai", normalizeChannelTestEndpoint(ch, "any-model", ""))
			require.Equal(t, "anthropic", normalizeChannelTestEndpoint(ch, "any-model", "anthropic"))
		}
	}
	for _, base := range []string{"relative", "ftp://host", "https://host?key=bad"} {
		require.Error(t, validateChannel(&model.Channel{Type: constant.ChannelTypeQianwenAI, BaseURL: &base}, true))
	}
}
