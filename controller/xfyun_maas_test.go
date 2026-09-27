package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/xfyun_maas"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
)

func TestXfyunMaasExplicitTestEndpoints(t *testing.T) {
	require.Equal(t, xfyun_maas.ModelList, channelId2Models[constant.ChannelTypeXunfeiMaas])
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic}, common.GetEndpointTypesByChannelType(constant.ChannelTypeXunfeiMaas, "xopglm53"))
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeEmbeddings}, common.GetEndpointTypesByChannelType(constant.ChannelTypeXunfeiMaas, "xop3qwen8bembedding"))
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeJinaRerank}, common.GetEndpointTypesByChannelType(constant.ChannelTypeXunfeiMaas, "xop3qwen8breranker"))
	for _, name := range []string{"xopzimageturbo", "xopqwentti20b"} {
		require.Equal(t, []constant.EndpointType{constant.EndpointTypeImageGeneration}, common.GetEndpointTypesByChannelType(constant.ChannelTypeXunfeiMaas, name))
	}
	channel := &model.Channel{Type: constant.ChannelTypeXunfeiMaas}
	for _, endpoint := range []constant.EndpointType{
		constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAIResponse,
		constant.EndpointTypeEmbeddings, constant.EndpointTypeImageGeneration, constant.EndpointTypeJinaRerank,
	} {
		require.Equal(t, string(endpoint), normalizeChannelTestEndpoint(channel, "opaque-id", string(endpoint)))
		request := buildTestRequest("opaque-id", string(endpoint), channel, false)
		switch endpoint {
		case constant.EndpointTypeAnthropic:
			require.IsType(t, &dto.ClaudeRequest{}, request)
		case constant.EndpointTypeOpenAIResponse:
			require.IsType(t, &dto.OpenAIResponsesRequest{}, request)
		case constant.EndpointTypeEmbeddings:
			require.IsType(t, &dto.EmbeddingRequest{}, request)
		case constant.EndpointTypeImageGeneration:
			require.IsType(t, &dto.ImageRequest{}, request)
		case constant.EndpointTypeJinaRerank:
			require.IsType(t, &dto.RerankRequest{}, request)
		default:
			require.IsType(t, &dto.GeneralOpenAIRequest{}, request)
		}
	}
	settings := dto.ChannelOtherSettings{UpstreamModelUpdateCheckEnabled: true}
	require.False(t, isChannelUpstreamModelUpdateEnabled(channel, settings))
	settings.CustomModelListURL = "https://example.com/models"
	require.True(t, isChannelUpstreamModelUpdateEnabled(channel, settings))
	_, err := fetchChannelModelIDsWithKey(channel, "http://127.0.0.1:1", "fixture", "")
	require.ErrorContains(t, err, "未提供公开模型列表接口")
	_, err = fetchChannelModelIDsWithKey(nil, "", "", "")
	require.ErrorContains(t, err, "channel is nil")
}

func TestXfyunMaasImageFailureRefund(t *testing.T) {
	for _, body := range []string{
		`{"header":{"code":10004,"message":"schema error","sid":"fixture"}}`,
		`{"header":{"code":0},"payload":{"choices":{"text":[]}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer upstream.Close()
			db, engine, ch := setupMiMoGateway(t, upstream.URL, "fixture-key")
			ch.Type, ch.Models = constant.ChannelTypeXunfeiMaas, "xopzimageturbo"
			require.NoError(t, ch.Update())
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"xopzimageturbo":0.001}`))
			engine.POST("/v1/images/generations", RelayImageGeneration)
			w := requestMiMo(t, engine, "/v1/images/generations", `{"model":"xopzimageturbo","prompt":"cat"}`, mimoGatewayKey)
			require.GreaterOrEqual(t, w.Code, 400)
			require.Contains(t, w.Body.String(), `"error"`)
			require.Equal(t, 1, calls)
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
			require.Zero(t, count)
			assertMiMoQuota(t, db, 0)
		})
	}
}
