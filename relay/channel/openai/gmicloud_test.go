package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGMICloudAdaptorMetadata(t *testing.T) {
	adaptor := &Adaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeGMICloud}})

	require.Equal(t, gmicloud.ChannelName, adaptor.GetChannelName())
	require.Equal(t, gmicloud.ModelList, adaptor.GetModelList())
}

func TestGMICloudChatCompletionRequestURL(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeGMICloud,
			ChannelBaseUrl: constant.ChannelBaseURLs[constant.ChannelTypeGMICloud],
		},
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
	}

	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://api.gmi-serving.com/v1/chat/completions", url)
}

func TestGMICloudBatchModelRejectsChatCompletionEndpoint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGMICloud,
			ChannelBaseUrl:    constant.ChannelBaseURLs[constant.ChannelTypeGMICloud],
			UpstreamModelName: gmicloud.BatchInferenceModel,
		},
		RequestURLPath: "/v1/chat/completions",
	}

	requestURL, err := (&Adaptor{}).GetRequestURL(info)
	require.ErrorContains(t, err, "/v1/batch/generations")
	require.Empty(t, requestURL)
}

func TestGMICloudDoRequestUsesBearerAuthAndPreservesModel(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"model":"MiniMaxAI/MiniMax-M3"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test"}`))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeGMICloud,
			ChannelBaseUrl: upstream.URL,
			ApiKey:         "test-key",
		},
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
	}

	rawResp, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{"model":"MiniMaxAI/MiniMax-M3"}`))
	require.NoError(t, err)
	resp, ok := rawResp.(*http.Response)
	require.True(t, ok)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

func TestGMICloudKeepsStreamingButDropsUndocumentedStreamOptions(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"stream":true`)
		require.NotContains(t, string(body), "stream_options")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-test\"}\n\ndata: [DONE]\n\n"))
	}))
	defer upstream.Close()

	isStream := true
	request := &dto.GeneralOpenAIRequest{
		Model:         "MiniMaxAI/MiniMax-M2.7",
		Stream:        &isStream,
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeGMICloud,
			ChannelBaseUrl:       upstream.URL,
			ApiKey:               "test-key",
			SupportStreamOptions: false,
			UpstreamModelName:    request.Model,
		},
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
		IsStream:       true,
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, convertedRequest.Stream)
	require.True(t, *convertedRequest.Stream)
	require.Nil(t, convertedRequest.StreamOptions)
	require.Equal(t, "MiniMaxAI/MiniMax-M2.7", convertedRequest.Model)

	body, err := projectcommon.Marshal(convertedRequest)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	rawResp, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	resp, ok := rawResp.(*http.Response)
	require.True(t, ok)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(responseBody), "data: [DONE]")
	require.NoError(t, resp.Body.Close())
}
