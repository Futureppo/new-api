package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/mimo"
	"github.com/stretchr/testify/require"
)

func TestMiMoModelDiscoveryAndSync(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "upstream-test-key", r.Header.Get("api-key"))
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, "override", r.Header.Get("X-Test"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/custom" {
			_, _ = w.Write([]byte(`{"data":[{"id":"custom-model"},{"id":"mimo-v2.5-tts"}]}`))
			return
		}
		require.Equal(t, "/v1/models", r.URL.Path)
		_, _ = w.Write([]byte(`{"data":[{"id":"mimo-v2.5"},{"id":"mimo-v2.5-pro"},{"id":"mimo-v2.5"},{"id":"mimo-v2.5-asr"},{"id":"mimo-v2.5-tts"},{"id":"mimo-v2.5-tts-voiceclone"},{"id":"mimo-v2.5-tts-voicedesign"}]}`))
	}))
	defer upstream.Close()
	ch := &model.Channel{Type: constant.ChannelTypeMiMo, Key: "upstream-test-key", BaseURL: common.GetPointer(upstream.URL + "/anthropic/"), Models: "mimo-v2.6-pro-ultraspeed", HeaderOverride: common.GetPointer(`{"X-Test":"override"}`)}
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), ch.Key, "")
	require.NoError(t, err)
	require.Equal(t, mimo.ModelList, names)
	add, remove, _, err := collectPendingUpstreamModelChanges(ch, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	require.Equal(t, []string{"mimo-v2.5", "mimo-v2.5-pro"}, add)
	require.Empty(t, remove)
	names, err = fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), ch.Key, upstream.URL+"/custom")
	require.NoError(t, err)
	require.Equal(t, []string{"custom-model", "mimo-v2.5-tts"}, names)
	require.Equal(t, mimo.ModelList, channelId2Models[constant.ChannelTypeMiMo])
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic}, common.GetEndpointTypesByChannelType(ch.Type, mimo.DefaultTestModel))
}

func TestMiMoDiscoveryFailureIsNotMasked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer upstream.Close()
	names, err := fetchChannelModelIDsWithKey(&model.Channel{Type: constant.ChannelTypeMiMo}, upstream.URL, "bad-key", "")
	require.Error(t, err)
	require.Empty(t, names)
}

func TestMiMoNewChannelDefaultsPreserveEdits(t *testing.T) {
	for _, tc := range []struct {
		add                  bool
		configured, expected string
	}{
		{true, "", mimo.DefaultTestModel}, {true, "mimo-v2.5", "mimo-v2.5"}, {false, "", ""}, {false, "mimo-v2.5", "mimo-v2.5"},
	} {
		ch := &model.Channel{Type: constant.ChannelTypeMiMo, Key: "test-key", TestModel: common.GetPointer(tc.configured)}
		require.NoError(t, validateChannel(ch, tc.add))
		require.Equal(t, tc.expected, *ch.TestModel)
	}
}
