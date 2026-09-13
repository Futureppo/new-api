package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMistralChannelModelDiscoveryAndTestEndpoints(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeMistral}
	for name, endpoint := range map[string]string{"mistral-embed": string(constant.EndpointTypeEmbeddings), "codestral-embed": string(constant.EndpointTypeEmbeddings), "voxtral-mini-latest": string(constant.EndpointTypeAudioTranscription), "voxtral-mini-2602": string(constant.EndpointTypeAudioTranscription), "voxtral-small-latest": string(constant.EndpointTypeOpenAI), "ministral-3b-latest": string(constant.EndpointTypeOpenAI)} {
		require.Equal(t, endpoint, normalizeChannelTestEndpoint(channel, name, ""))
	}
	require.False(t, mistral.IsTranscriptionModel("voxtral-mini-tts-latest"))
	require.False(t, mistral.IsTranscriptionModel("voxtral-mini-realtime-latest"))
	for _, base := range []string{"https://api.mistral.ai", "https://api.mistral.ai/", "https://api.mistral.ai/v1/"} {
		require.Equal(t, "https://api.mistral.ai/v1/models", resolveFetchModelsURL(channel.Type, base, ""))
	}
	require.Equal(t, "https://example.com/list", resolveFetchModelsURL(channel.Type, "https://api.mistral.ai", "https://example.com/list"))
}

func TestMistralChannelTranscriptionTestBuildsRealMultipart(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeMistral}
	request := buildTestRequest("voxtral-mini-latest", string(constant.EndpointTypeAudioTranscription), channel, false).(*dto.AudioRequest)
	require.Empty(t, request.Metadata, "Mistral tests must not contain GCP metadata")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", nil)
	require.NoError(t, prepareMistralTranscriptionTest(c, request.Model))
	form, err := common.ParseMultipartFormReusable(c)
	require.NoError(t, err)
	defer form.RemoveAll()
	require.Len(t, form.File["file"], 1)
	file, err := form.File["file"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	duration, err := common.GetAudioDuration(c.Request.Context(), file, ".wav")
	require.NoError(t, err)
	require.Equal(t, float64(2), duration)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioTranscription}
	reader, err := (&mistral.Adaptor{}).ConvertAudioRequest(c, info, *request)
	require.NoError(t, err)
	require.NotNil(t, reader)
}
