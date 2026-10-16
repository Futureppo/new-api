package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExplicitSystemOneUsesConfiguredChannelAddressAndCredentials(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/proxy/v1/systemone", r.URL.Path)
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		require.Equal(t, "configured", r.Header.Get("X-Probe"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "jev-1.14-free", gjson.GetBytes(body, "model").String())
		require.True(t, gjson.GetBytes(body, "questions").IsObject())
		require.Equal(t, "noul", gjson.GetBytes(body, "questions.urgent.type").String())
		require.Equal(t, "false", gjson.GetBytes(body, "extra").Raw)
		require.False(t, gjson.GetBytes(body, "stream").Bool())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"jev-1.14-free","answers":{"urgent":{"type":"noul","noul":0}},"usage":{"input_tokens":23,"output_tokens":0}}`)
	}))
	defer upstream.Close()
	db, _, ch := setupDecisionsGateway(t, upstream.URL+"/proxy/v1/")
	ch.ModelMapping = common.GetPointer(`{"alias":"jev-1.14-free"}`)
	ch.ParamOverride = common.GetPointer(`{"extra":false}`)
	ch.HeaderOverride = common.GetPointer(`{"X-Probe":"configured"}`)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"alias":0.1}`))
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeMiMo, constant.ChannelTypeCohere} {
		ch.Type = channelType
		result := testChannel(ch, "alias", "typesafe-systemone", true)
		require.NoError(t, result.localErr)
		require.Nil(t, result.newAPIError)
		require.Equal(t, "jev-1.14-free", result.upstreamModel)
		require.Equal(t, 23, result.usage.PromptTokens)
		require.Zero(t, result.usage.CompletionTokens)
		require.Equal(t, channelType, ch.Type, "test must not mutate the configured channel")
	}
	require.EqualValues(t, 3, calls.Load())
	var logs []model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").Find(&logs).Error)
	require.Len(t, logs, 3)
	for _, log := range logs {
		require.Equal(t, ch.Id, log.ChannelId)
		require.Equal(t, "alias", log.ModelName)
		require.Equal(t, "/v1/systemone", gjson.Get(log.Other, "request_path").String())
		require.False(t, log.IsStream)
	}
}

func TestEveryExplicitEndpointReachesUpstreamRegardlessOfChannelType(t *testing.T) {
	var calls atomic.Int32
	var wantPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, wantPath, r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		switch wantPath {
		case "/v1/messages":
			require.Equal(t, "hi", gjson.GetBytes(body, "messages.0.content").String())
		case "/v1beta/models/gemini-2.5-flash:generateContent":
			require.Equal(t, "hi", gjson.GetBytes(body, "contents.0.parts.0.text").String())
		case "/v2/embed":
			require.True(t, gjson.GetBytes(body, "texts").IsArray())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"probe reached upstream"}}`)
	}))
	defer upstream.Close()
	_, _, ch := setupDecisionsGateway(t, upstream.URL)
	prices, err := common.Marshal(map[string]float64{
		"probe-model": 0.001, "probe-model" + ratio_setting.CompactModelSuffix: 0.001,
		"probe-model-openai-decisions": 0.001, "gemini-2.5-flash": 0.001,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(prices)))
	endpoints := []struct{ endpoint, path string }{
		{"openai", "/v1/chat/completions"}, {"openai-response", "/v1/responses"},
		{"openai-response-compact", "/v1/responses/compact"}, {"openai-decisions", "/v1/decisions"},
		{"typesafe-systemone", "/v1/systemone"}, {"anthropic", "/v1/messages"},
		{"gemini", "/v1beta/models/gemini-2.5-flash:generateContent"},
		{"vertex-native", "/v1/models/gemini-2.5-flash:generateContent"},
		{"cohere-chat", "/v2/chat"}, {"cohere-rerank", "/v2/rerank"}, {"cohere-embeddings", "/v2/embed"},
		{"jina-rerank", "/v1/rerank"}, {"embeddings", "/v1/embeddings"}, {"moderations", "/v1/moderations"},
		{"image-generation", "/v1/images/generations"}, {"audio-speech", "/v1/audio/speech"},
		{"audio-transcription", "/v1/audio/transcriptions"}, {"openai-video", "/v1/videos"},
		{"batch-generation", "/v1/batch/generations"},
	}
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeTypeSafe, constant.ChannelTypeMidjourney} {
		ch.Type = channelType
		for _, tc := range endpoints {
			t.Run(fmt.Sprintf("%d/%s", channelType, tc.endpoint), func(t *testing.T) {
				wantPath = tc.path
				name := "probe-model"
				if strings.Contains(tc.path, "gemini-2.5-flash") {
					name = "gemini-2.5-flash"
				}
				before := calls.Load()
				result := testChannel(ch, name, tc.endpoint, false)
				require.Equal(t, before+1, calls.Load(), "request rejected locally: %v", result.localErr)
				require.ErrorContains(t, result.localErr, "probe reached upstream")
				require.Equal(t, channelType, ch.Type)
			})
		}
	}
}

func TestExplicitEndpointUsesOriginalDefaultBaseURL(t *testing.T) {
	source := &model.Channel{Type: constant.ChannelTypeCohere, Key: "keep-this-key"}
	resolved, direct, err := channelForExplicitEndpointTest(source, constant.EndpointTypeTypeSafeSystemOne, "alias")
	require.NoError(t, err)
	require.True(t, direct)
	require.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeCohere], resolved.GetBaseURL())
	require.Equal(t, source.Key, resolved.Key)
	require.Nil(t, source.BaseURL)
	source.Type = constant.ChannelTypeCustom
	_, _, err = channelForExplicitEndpointTest(source, constant.EndpointTypeTypeSafeSystemOne, "alias")
	require.ErrorContains(t, err, "base URL")
}

func TestExplicitTaskEndpointsAcceptUpstreamJob(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Contains(t, []string{"/v1/videos", "/v1/batch/generations"}, r.URL.Path)
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "mapped-model", gjson.GetBytes(body, "model").String())
		require.Equal(t, "false", gjson.GetBytes(body, "extra").Raw)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = io.WriteString(w, `{"id":"task-probe","model":"mapped-model","status":"queued"}`)
	}))
	defer upstream.Close()
	_, _, ch := setupDecisionsGateway(t, upstream.URL)
	ch.Type = constant.ChannelTypeTypeSafe
	ch.ModelMapping = common.GetPointer(`{"alias":"mapped-model"}`)
	ch.ParamOverride = common.GetPointer(`{"extra":false}`)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"alias":0.001}`))
	for _, endpoint := range []string{"openai-video", "batch-generation"} {
		result := testChannel(ch, "alias", endpoint, true)
		require.NoError(t, result.localErr)
		require.Nil(t, result.newAPIError)
		require.Equal(t, "mapped-model", result.upstreamModel)
	}
	require.EqualValues(t, 2, calls.Load())
}
