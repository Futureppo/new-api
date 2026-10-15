package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/qianwen"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const qianwenDecisionQuestions = `{"team":{"type":"choice","criteria":{"support":"Technical issue","sales":"Purchase enquiry"}},"urgent":{"type":"noul","instructions":"Is urgent action needed?"},"severity":{"type":"score","criteria":["Minor","Major"]}}`
const qianwenDecisionResponse = `{"model":"decision-model-preview","request_id":"decision-test","answers":{"team":{"type":"choice","choice":"support","confidence":1,"probabilities":{"support":1,"sales":0}},"urgent":{"type":"noul","noul":0},"severity":{"type":"score","score":0,"confidence":1,"legend":{"0":"Minor","1":"Major"},"probabilities":{"0":1,"1":0}}},"usage":{"input_tokens":125},"latency_ms":0}`

func TestQianwenDecisionGatewayAndBilling(t *testing.T) {
	for _, state := range []string{`"Help with a technical issue"`, `{"text":"Help","count":0,"urgent":false}`, `[{"role":"user","content":"Help"}]`} {
		t.Run(state, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/prefix/compatible-mode/v1/systemone", r.URL.Path)
				require.Equal(t, "Bearer decision-key", r.Header.Get("Authorization"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, constant.QianwenDecisionModel, gjson.GetBytes(data, "model").String())
				require.JSONEq(t, state, gjson.GetBytes(data, "state").Raw)
				require.JSONEq(t, qianwenDecisionQuestions, gjson.GetBytes(data, "questions").Raw)
				require.Equal(t, "false", gjson.GetBytes(data, "stream").Raw)
				require.Equal(t, "false", gjson.GetBytes(data, "extra").Raw)
				require.Equal(t, "0", gjson.GetBytes(data, "threshold").Raw)
				require.False(t, gjson.GetBytes(data, "messages").Exists())
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, qianwenDecisionResponse)
			}))
			defer upstream.Close()
			db, engine, ch := setupQianwenGateway(t, upstream.URL+"/prefix/compatible-mode", "decision-key")
			engine.POST("/v1/systemone", func(c *gin.Context) { Relay(c, types.RelayFormatTypeSafe) })
			ch.ModelMapping = common.GetPointer(`{"qianwen-test":"decision-alias","decision-alias":"decision-model-preview"}`)
			ch.ParamOverride = common.GetPointer(`{"threshold":0}`)
			require.NoError(t, ch.Update())
			body := fmt.Sprintf(`{"model":"qianwen-test","state":%s,"questions":%s,"stream":false,"extra":false}`, state, qianwenDecisionQuestions)
			w := requestMiMo(t, engine, "/v1/systemone", body, mimoGatewayKey)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.JSONEq(t, qianwenDecisionResponse, w.Body.String())
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			require.Len(t, logs, 1)
			require.Equal(t, 125, logs[0].PromptTokens)
			require.Zero(t, logs[0].CompletionTokens)
			require.Equal(t, 125, logs[0].Quota)
			assertMiMoQuota(t, db, 125)
		})
	}
}

func TestQianwenDecisionChannelTest(t *testing.T) {
	require.Contains(t, qianwen.ModelList, constant.QianwenDecisionModel)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/compatible-mode/v1/systemone", r.URL.Path)
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, constant.QianwenDecisionModel, gjson.GetBytes(data, "model").String())
		require.True(t, gjson.GetBytes(data, "state").Exists())
		require.Equal(t, "noul", gjson.GetBytes(data, "questions.urgent.type").String())
		require.False(t, gjson.GetBytes(data, "stream").Bool())
		require.False(t, gjson.GetBytes(data, "messages").Exists())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, qianwenDecisionResponse)
	}))
	defer upstream.Close()
	_, _, ch := setupQianwenGateway(t, upstream.URL+"/compatible-mode/v1/systemone", "key")
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"decision-alias","decision-alias":"decision-model-preview"}`)
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"qianwen-test":1,"decision-model-preview":1}`))
	for _, name := range []string{constant.QianwenDecisionModel, qianwenAlias} {
		require.Equal(t, "typesafe-systemone", normalizeChannelTestEndpoint(ch, name, ""))
		for _, endpoint := range []string{"", "typesafe-systemone"} {
			for _, stream := range []bool{false, true} {
				result := testChannel(ch, name, endpoint, stream)
				require.NoError(t, result.localErr)
				require.Nil(t, result.newAPIError, "%v", result.newAPIError)
				require.Equal(t, 125, result.usage.PromptTokens)
				require.Zero(t, result.usage.CompletionTokens)
			}
		}
	}
	for _, name := range []string{constant.QianwenDecisionModel, "decision-model-preview-2026-09-24"} {
		require.Equal(t, []constant.EndpointType{constant.EndpointTypeTypeSafeSystemOne}, common.GetEndpointTypesByChannelType(constant.ChannelTypeQianwenAI, name))
	}
}

func TestQianwenDecisionRejectsChatAndStreaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached upstream") }))
	defer upstream.Close()
	db, engine, ch := setupQianwenGateway(t, upstream.URL, "key")
	engine.POST("/v1/systemone", func(c *gin.Context) { Relay(c, types.RelayFormatTypeSafe) })
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"decision-model-preview"}`)
	require.NoError(t, ch.Update())
	for _, endpoint := range qianwenEndpoints {
		w := requestMiMo(t, engine, endpoint, qianwenTestBody(endpoint, false), mimoGatewayKey)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "/v1/systemone")
	}
	valid := `{"model":"qianwen-test","state":"Help","questions":` + qianwenDecisionQuestions + `}`
	for _, extra := range []string{`"stream":true`, `"stream_options":{"include_usage":true}`} {
		w := requestMiMo(t, engine, "/v1/systemone", strings.TrimSuffix(valid, "}")+","+extra+"}", mimoGatewayKey)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	for _, override := range []string{`{"stream":true}`, `{"model":"another-model"}`} {
		ch.ParamOverride = &override
		require.NoError(t, ch.Update())
		w := requestMiMo(t, engine, "/v1/systemone", valid, mimoGatewayKey)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	}
	assertMiMoQuota(t, db, 0)
}

func TestQianwenDecisionErrorsRefund(t *testing.T) {
	for _, status := range []int{401, 429, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "3")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"decision unavailable"}}`)
			}))
			defer upstream.Close()
			db, engine, ch := setupQianwenGateway(t, upstream.URL, "key")
			engine.POST("/v1/systemone", func(c *gin.Context) { Relay(c, types.RelayFormatTypeSafe) })
			ch.ModelMapping = common.GetPointer(`{"qianwen-test":"decision-model-preview"}`)
			require.NoError(t, ch.Update())
			w := requestMiMo(t, engine, "/v1/systemone", `{"model":"qianwen-test","state":"Help","questions":`+qianwenDecisionQuestions+`}`, mimoGatewayKey)
			if status == 200 {
				require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
			} else {
				require.Equal(t, status, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "decision unavailable")
				require.Equal(t, "3", w.Header().Get("Retry-After"))
			}
			assertMiMoQuota(t, db, 0)
		})
	}
}

func TestQianwenDecisionLive(t *testing.T) {
	key := os.Getenv("QIANWEN_API_KEY")
	if key == "" {
		t.Skip("set QIANWEN_API_KEY to verify the real decision-model-preview endpoint")
	}
	_, engine, ch := setupQianwenGateway(t, "https://maas.qianwenaiapi.com", key)
	engine.POST("/v1/systemone", func(c *gin.Context) { Relay(c, types.RelayFormatTypeSafe) })
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"decision-model-preview"}`)
	require.NoError(t, ch.Update())
	w := requestMiMo(t, engine, "/v1/systemone", `{"model":"qianwen-test","state":"Help with a technical issue","questions":`+qianwenDecisionQuestions+`}`, mimoGatewayKey)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, gjson.Get(w.Body.String(), "answers.team.choice").Exists())
	require.True(t, gjson.Get(w.Body.String(), "usage.input_tokens").Exists())
}
