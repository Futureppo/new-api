package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const decisionsGatewayBody = `{"model":"gpt-6-luna","input":"hello","questions":[{"type":"predicate","name":"greeting","instructions":"Is this a greeting?"}],"extra":false}`
const decisionsGatewayResponse = `{"model":"gpt-6-luna-upstream","answers":[{"type":"predicate","name":"greeting","probability":0}],"usage":{"input_tokens":1000,"output_tokens":0,"total_tokens":1000},"extra":false}`

func setupDecisionsGateway(t *testing.T, baseURL string) (*gorm.DB, *gin.Engine, *model.Channel) {
	t.Helper()
	db, engine, channel := setupTypeSafeGateway(t, baseURL, "upstream-key")
	channel.Type = constant.ChannelTypeOpenAI
	channel.Models = "gpt-6-luna,alias"
	require.NoError(t, channel.Save())
	require.NoError(t, channel.UpdateAbilities(db))
	engine.POST("/v1/decisions", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIDecisions) })
	engine.POST("/v1/responses", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	oldSelfUse := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = false
	t.Cleanup(func() { operation_setting.SelfUseModeEnabled = oldSelfUse })
	savedBilling := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		if strings.HasPrefix(key, "billing_setting.") {
			savedBilling[key] = value
		}
		return nil
	}))
	clearBilling := map[string]string{"billing_setting.billing_mode": "null", "billing_setting.billing_expr": "null"}
	require.NoError(t, config.GlobalConfig.LoadFromDB(clearBilling))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(clearBilling))
		require.NoError(t, config.GlobalConfig.LoadFromDB(savedBilling))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-6-luna":3,"gpt-6-luna-openai-decisions":0.1,"alias-openai-decisions":0.2}`))
	return db, engine, channel
}

func TestDecisionsGatewayBillingMappingAndChannelTest(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/custom/v1/decisions", r.URL.Path)
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "gpt-6-luna-upstream", gjson.GetBytes(body, "model").String())
		require.Equal(t, "0", gjson.GetBytes(body, "extra").Raw)
		require.Equal(t, "false", gjson.GetBytes(body, "future_flag").Raw)
		require.False(t, gjson.GetBytes(body, "stream").Bool())
		require.False(t, gjson.GetBytes(body, "safety_identifier").Exists())
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, decisionsGatewayResponse)
	}))
	defer upstream.Close()
	db, engine, channel := setupDecisionsGateway(t, upstream.URL+"/custom")
	channel.ModelMapping = common.GetPointer(`{"gpt-6-luna":"gpt-6-luna-upstream","alias":"gpt-6-luna-upstream"}`)
	channel.ParamOverride = common.GetPointer(`{"extra":0,"future_flag":false}`)
	require.NoError(t, channel.Save())
	body := strings.TrimSuffix(decisionsGatewayBody, "}") + `,"safety_identifier":"private"}`
	w := requestTypeSafe(t, engine, "/v1/decisions", body, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, decisionsGatewayResponse, w.Body.String())
	assertTypeSafeQuota(t, db, 100)
	w = requestTypeSafe(t, engine, "/v1/decisions", strings.Replace(body, `"model":"gpt-6-luna"`, `"model":"alias"`, 1), typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	assertTypeSafeQuota(t, db, 300)
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Order("id").Find(&logs).Error)
	require.Len(t, logs, 2)
	require.Equal(t, "gpt-6-luna", logs[0].ModelName)
	require.Equal(t, "alias", logs[1].ModelName)
	require.Equal(t, "gpt-6-luna-openai-decisions", gjson.Get(logs[0].Other, "billing_model_name").String())
	require.Equal(t, "alias-openai-decisions", gjson.Get(logs[1].Other, "billing_model_name").String())
	require.Equal(t, "/v1/decisions", gjson.Get(logs[0].Other, "request_path").String())
	require.Zero(t, logs[0].CompletionTokens)

	result := testChannel(channel, "gpt-6-luna", "openai-decisions", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.Equal(t, "gpt-6-luna-upstream", result.upstreamModel)
	require.Equal(t, 1000, result.usage.PromptTokens)
	require.Zero(t, result.usage.CompletionTokens)
	require.EqualValues(t, 3, calls.Load())
	require.NoError(t, db.Where("token_name = ?", "模型测试").Find(&logs).Error)
	require.Len(t, logs, 1)
	require.False(t, logs[0].IsStream)
	require.Equal(t, 100, logs[0].Quota)
	require.Equal(t, "gpt-6-luna-openai-decisions", gjson.Get(logs[0].Other, "billing_model_name").String())
}

func TestDecisionsGatewayPricingIsolationAndModes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/responses" {
			_, _ = io.WriteString(w, `{"id":"resp-test","model":"gpt-6-luna","output":[],"usage":{"input_tokens":1000,"output_tokens":0,"total_tokens":1000}}`)
			return
		}
		_, _ = io.WriteString(w, decisionsGatewayResponse)
	}))
	defer upstream.Close()
	db, engine, _ := setupDecisionsGateway(t, upstream.URL)
	w := requestTypeSafe(t, engine, "/v1/responses", `{"model":"gpt-6-luna","input":"hello"}`, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	assertTypeSafeQuota(t, db, 3000)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-6-luna":3}`))
	w = requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "gpt-6-luna-openai-decisions")
	assertTypeSafeQuota(t, db, 3000)

	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"gpt-6-luna-openai-decisions":0.002}`))
	w = requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	used := 3000 + int(0.002*common.QuotaPerUnit)
	assertTypeSafeQuota(t, db, used)

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"gpt-6-luna-openai-decisions":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"gpt-6-luna-openai-decisions":"param(\"questions.0.type\") == \"predicate\" ? tier(\"decision\", p * 0.4) : tier(\"other\", p)"}`,
	}))
	w = requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	assertTypeSafeQuota(t, db, used+int(1000*0.4/1e6*common.QuotaPerUnit))
	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Last(&log).Error)
	require.Equal(t, "tiered_expr", gjson.Get(log.Other, "billing_mode").String())
	require.Equal(t, "decision", gjson.Get(log.Other, "matched_tier").String())
}

func TestDecisionsGatewayErrorsRefund(t *testing.T) {
	for _, status := range []int{400, 429, 500, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					_, _ = io.WriteString(w, `{"model":"gpt-6-luna","answers":[{"type":"refusal","name":null}]}`)
				} else {
					_, _ = io.WriteString(w, `{"error":{"message":"upstream rejected request","type":"invalid_request_error"}}`)
				}
			}))
			defer upstream.Close()
			db, engine, _ := setupDecisionsGateway(t, upstream.URL)
			w := requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
			expected := status
			if status == 200 {
				expected = 502
			}
			require.Equal(t, expected, w.Code, w.Body.String())
			require.Eventually(t, func() bool {
				var user model.User
				var token model.Token
				return db.First(&user, 1).Error == nil && db.First(&token, 1).Error == nil && user.Quota == typeSafeInitialQuota && token.RemainQuota == typeSafeInitialQuota
			}, time.Second, 10*time.Millisecond)
			assertTypeSafeQuota(t, db, 0)
		})
	}
}

func TestDecisionsGatewayValidationAndRefusal(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"gpt-6-luna","answers":[{"type":"refusal","name":"greeting"}],"usage":{"input_tokens":1000,"output_tokens":0,"total_tokens":1000}}`)
	}))
	defer upstream.Close()
	db, engine, channel := setupDecisionsGateway(t, upstream.URL)
	w := requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, "invalid-token")
	require.Equal(t, 401, w.Code)
	w = requestTypeSafe(t, engine, "/v1/decisions", strings.TrimSuffix(decisionsGatewayBody, "}")+`,"stream":true}`, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	channel.ParamOverride = common.GetPointer(`{"stream":true}`)
	require.NoError(t, channel.Save())
	w = requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Zero(t, calls.Load())
	channel.ParamOverride = nil
	require.NoError(t, channel.Save())
	w = requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"type":"refusal"`)
	require.EqualValues(t, 1, calls.Load())
	assertTypeSafeQuota(t, db, 100)
	channel.Type = constant.ChannelTypeAzure
	require.NoError(t, channel.Save())
	require.NoError(t, channel.UpdateAbilities(db))
	w = requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.EqualValues(t, 1, calls.Load())
}

func TestDecisionsAutomaticDetectionUnchanged(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	require.Empty(t, normalizeChannelTestEndpoint(channel, "gpt-6-luna", ""))
	require.Equal(t, "openai-decisions", normalizeChannelTestEndpoint(channel, "gpt-6-luna", "openai-decisions"))
	request := buildTestRequest("gpt-6-luna", "openai-decisions", channel, true).(*dto.OpenAIDecisionsRequest)
	require.False(t, request.IsStream(nil))
	body, err := common.Marshal(request)
	require.NoError(t, err)
	var parsed dto.OpenAIDecisionsRequest
	require.NoError(t, common.Unmarshal(body, &parsed))
}

func TestDecisionsGatewayRetryStartsFromClientRequest(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer first-key" {
			require.Equal(t, "first-model", gjson.GetBytes(body, "model").String())
			require.True(t, gjson.GetBytes(body, "first_only").Bool())
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"error":{"type":"server_error","message":"busy"}}`)
			return
		}
		require.Equal(t, "Bearer second-key", r.Header.Get("Authorization"))
		require.Equal(t, "second-model", gjson.GetBytes(body, "model").String())
		require.False(t, gjson.GetBytes(body, "first_only").Exists())
		require.Equal(t, "false", gjson.GetBytes(body, "extra").Raw)
		_, _ = io.WriteString(w, decisionsGatewayResponse)
	}))
	defer upstream.Close()
	db, engine, first := setupDecisionsGateway(t, upstream.URL)
	common.RetryTimes = 1
	first.Key = "first-key"
	first.Priority = common.GetPointer(int64(10))
	first.ModelMapping = common.GetPointer(`{"gpt-6-luna":"first-model"}`)
	first.ParamOverride = common.GetPointer(`{"first_only":true}`)
	require.NoError(t, first.Save())
	require.NoError(t, first.UpdateAbilities(db))
	second := &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "decisions-retry", Key: "second-key", BaseURL: &upstream.URL, Models: "gpt-6-luna", Group: "default", Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(`{"gpt-6-luna":"second-model"}`)}
	require.NoError(t, second.Insert())
	w := requestTypeSafe(t, engine, "/v1/decisions", decisionsGatewayBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.EqualValues(t, 2, attempts.Load())
	assertTypeSafeQuota(t, db, 100)
}
