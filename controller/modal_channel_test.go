package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/modal"
	"github.com/stretchr/testify/require"
)

func TestModalDefaultModelListURLAcceptsChatEndpoint(t *testing.T) {
	require.Equal(
		t,
		"https://example--app.modal.direct/v1/models",
		resolveFetchModelsURL(
			constant.ChannelTypeModal,
			"https://example--app.modal.direct/v1/chat/completions",
			"",
		),
	)
}

func TestFetchModalModelsUsesBearerAuth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer token-id.token-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"orcarouter/Qwen3.8-27B-Uncensored-FP8"}]}`))
	}))
	defer upstream.Close()

	channel := &model.Channel{Type: constant.ChannelTypeModal}
	models, err := fetchChannelModelIDsWithKey(
		channel,
		upstream.URL+"/v1/chat/completions",
		"token-id.token-secret",
		"",
	)
	require.NoError(t, err)
	require.Equal(t, []string{"orcarouter/Qwen3.8-27B-Uncensored-FP8"}, models)
}

func TestValidateModalChannelNormalizesBaseURL(t *testing.T) {
	baseURL := " https://example--app.modal.direct/v1/chat/completions/ "
	channel := &model.Channel{
		Type:    constant.ChannelTypeModal,
		Key:     "token-id.token-secret",
		BaseURL: &baseURL,
	}

	require.NoError(t, validateChannel(channel, true))
	require.Equal(t, "https://example--app.modal.direct", channel.GetBaseURL())
}

func TestValidateModalChannelRequiresBaseURL(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeModal, Key: "token-id.token-secret"}
	require.ErrorContains(t, validateChannel(channel, true), "Modal")
}

func TestValidateModalChannelDefaultsEnabledKeepaliveInterval(t *testing.T) {
	baseURL := "https://example--app.modal.direct"
	channel := &model.Channel{Type: constant.ChannelTypeModal, Key: "token-id.token-secret", BaseURL: &baseURL}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ModalKeepaliveEnabled: true})

	require.NoError(t, validateChannel(channel, true))
	require.Equal(t, dto.ModalKeepaliveDefaultIntervalSeconds, channel.GetOtherSettings().ModalKeepaliveIntervalSeconds)
}

func TestValidateModalChannelRejectsNegativeKeepaliveInterval(t *testing.T) {
	baseURL := "https://example--app.modal.direct"
	channel := &model.Channel{Type: constant.ChannelTypeModal, Key: "token-id.token-secret", BaseURL: &baseURL}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ModalKeepaliveIntervalSeconds: -1})

	require.ErrorContains(t, validateChannel(channel, true), "测活间隔")
}

func TestModalKeepaliveScheduleHonorsEligibilityIntervalAndOverlap(t *testing.T) {
	keepalive := &model.Channel{Id: 1, Type: constant.ChannelTypeModal, Status: common.ChannelStatusEnabled}
	keepalive.SetOtherSettings(dto.ChannelOtherSettings{ModalKeepaliveEnabled: true})

	disabledChannel := &model.Channel{Id: 2, Type: constant.ChannelTypeModal, Status: common.ChannelStatusManuallyDisabled}
	disabledChannel.SetOtherSettings(dto.ChannelOtherSettings{ModalKeepaliveEnabled: true})
	off := &model.Channel{Id: 3, Type: constant.ChannelTypeModal, Status: common.ChannelStatusEnabled}
	nonModal := &model.Channel{Id: 4, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	nonModal.SetOtherSettings(dto.ChannelOtherSettings{ModalKeepaliveEnabled: true})

	state := newModalKeepaliveScheduleState()
	now := time.Unix(1_700_000_000, 0)
	channels := []*model.Channel{keepalive, disabledChannel, off, nonModal}
	require.Equal(t, []*model.Channel{keepalive}, state.startDue(channels, now))
	require.Empty(t, state.startDue(channels, now.Add(30*time.Second)), "an in-flight request must not overlap")

	state.finish(keepalive.Id)
	require.Empty(t, state.startDue(channels, now.Add(29*time.Second)))
	require.Equal(t, []*model.Channel{keepalive}, state.startDue(channels, now.Add(30*time.Second)))
}

func TestKeepModalChannelAliveCallsEveryConfiguredModel(t *testing.T) {
	received := make(chan modalKeepaliveChatRequest, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer token-id.token-secret", r.Header.Get("Authorization"))
		require.Equal(t, "modal-keepalive", r.Header.Get("X-Request-Source"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var payload modalKeepaliveChatRequest
		require.NoError(t, common.DecodeJson(r.Body, &payload))
		require.Equal(t, 1, payload.MaxTokens)
		require.False(t, payload.Stream)
		require.Equal(t, []modalKeepaliveMessage{{Role: "user", Content: "ping"}}, payload.Messages)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	headerOverride := `{"X-Request-Source":"modal-keepalive"}`
	modelMapping := `{"alias-model":"mapped-model"}`
	channel := &model.Channel{
		Id:             1,
		Type:           constant.ChannelTypeModal,
		Status:         common.ChannelStatusEnabled,
		Key:            "token-id.token-secret",
		BaseURL:        common.GetPointer(upstream.URL + "/v1/chat/completions"),
		Models:         "alias-model,direct-model,alias-model",
		ModelMapping:   &modelMapping,
		HeaderOverride: &headerOverride,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, keepModalChannelAlive(ctx, channel))
	require.Equal(t, "mapped-model", (<-received).Model)
	require.Equal(t, "direct-model", (<-received).Model)
}

func TestModalDashboardStartsWithoutAssumedModels(t *testing.T) {
	require.Equal(t, modal.ModelList, channelId2Models[constant.ChannelTypeModal])
	require.Empty(t, channelId2Models[constant.ChannelTypeModal])
}
