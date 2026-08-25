package vyceai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageRequestModelRatios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model string
		ratio string
	}{
		{model: "你妈-1x1", ratio: "1:1"},
		{model: "你妈-16x9", ratio: "16:9"},
		{model: "你妈-9x16", ratio: "9:16"},
		{model: "你妈-2x3", ratio: "2:3"},
		{model: "你妈-3x2", ratio: "3:2"},
		{model: "你妈-4x3", ratio: "4:3"},
	}

	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			t.Parallel()
			requestedN := uint(99)
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeImagesGenerations,
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: test.model,
				},
			}
			converted, err := (&Adaptor{}).ConvertImageRequest(nil, info, dto.ImageRequest{
				Model:          test.model,
				Prompt:         "draw a banana",
				N:              &requestedN,
				Size:           "1x2",
				ResponseFormat: "url",
			})
			require.NoError(t, err)
			require.Equal(t, imageRequest{
				Model:       UpstreamModel,
				Prompt:      "draw a banana",
				AspectRatio: test.ratio,
				EnableNSFW:  true,
				Size:        UpstreamSize,
			}, converted)
			require.Equal(t, 1.0, info.PriceData.OtherRatios["n"])
		})
	}
}

func TestConvertImageRequestRejectsUnsupportedModel(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "你妈-21x9",
		},
	}
	_, err := (&Adaptor{}).ConvertImageRequest(nil, info, dto.ImageRequest{Model: "你妈-21x9", Prompt: "test"})
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func TestGetRequestURL(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	url, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
	require.NoError(t, err)
	require.Equal(t, "https://vyceai.com/v1/images/stream", url)

	url, err = adaptor.GetRequestURL(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com/custom/"}})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/custom/v1/images/stream", url)
}

func TestAdaptorRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()

	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		require.Equal(t, ImageStreamPath, r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.Equal(t, gin.MIMEJSON, r.Header.Get("Content-Type"))
		require.Equal(t, "text/event-stream", r.Header.Get("Accept"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var payload imageRequest
		require.NoError(t, common.Unmarshal(body, &payload))
		require.Equal(t, imageRequest{
			Model:       UpstreamModel,
			Prompt:      "draw a banana",
			AspectRatio: "3:2",
			EnableNSFW:  true,
			Size:        UpstreamSize,
		}, payload)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: progress\ndata: {\"message\":\"working\",\"progress\":50}\n\n")
		_, _ = io.WriteString(w, "event: complete\ndata: {\"message\":\"done\",\ndata: \"url\":\"data:image/jpeg;base64,aGVsbG8=\"}\n\n")
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"你妈-3x2","prompt":"draw a banana","n":9}`))
	c.Request.Header.Set("Content-Type", gin.MIMEJSON)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		StartTime: time.Unix(1700000000, 0),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelconstant.ChannelTypeVyceAI,
			ChannelBaseUrl:    upstream.URL,
			ApiKey:            "test-token",
			UpstreamModelName: "你妈-3x2",
		},
	}
	requestedN := uint(9)
	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertImageRequest(c, info, dto.ImageRequest{
		Model:          "你妈-3x2",
		Prompt:         "draw a banana",
		N:              &requestedN,
		Size:           "256x256",
		ResponseFormat: "url",
	})
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)

	rawResponse, err := adaptor.DoRequest(c, info, strings.NewReader(string(body)))
	require.NoError(t, err)
	httpResponse, ok := rawResponse.(*http.Response)
	require.True(t, ok)
	info.IsStream = strings.HasPrefix(httpResponse.Header.Get("Content-Type"), "text/event-stream")
	usage, apiErr := adaptor.DoResponse(c, httpResponse, info)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, int32(1), requestCount.Load())
	require.False(t, info.IsStream)
	require.Equal(t, 1.0, info.PriceData.OtherRatios["n"])

	var response openAIImageResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, int64(1700000000), response.Created)
	require.Equal(t, []openAIImageData{{B64JSON: "aGVsbG8="}}, response.Data)
	require.NotContains(t, recorder.Body.String(), `"url"`)
}
