package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const typeSafeGatewayKey = "typesafegatewaytesttoken"
const typeSafeInitialQuota = 100000
const typeSafeTestBody = `{"model":"jev-latest","state":{"text":"Please help urgently","zero":0},"questions":{"urgent":{"type":"noul","instructions":"Is it urgent?"},"team":{"type":"choice","instructions":"Which team?","criteria":{"technical":"Integration failures","billing":null}},"severity":{"type":"score","instructions":"Rate severity","criteria":["low","high"]}},"extra":false}`
const typeSafeTestResponse = `{"model":"jev-1.13.0","answers":{"urgent":{"type":"noul","noul":0},"team":{"type":"choice","choice":"technical","confidence":1,"probabilities":{"technical":1,"billing":0}},"severity":{"type":"score","score":0,"confidence":1,"legend":{"0":"low","1":"high"},"probabilities":{"0":1,"1":0}}},"usage":{"input_tokens":1000,"output_tokens":50},"extra":false}`

func setupTypeSafeGateway(t *testing.T, baseURL, upstreamKey string) (*gorm.DB, *gin.Engine, *model.Channel) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	oldDB, oldLogDB := model.DB, model.LOG_DB
	oldSQLite, oldMySQL, oldPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	oldMemory, oldRedis, oldBatch, oldLog := common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	oldCount, oldRetry, oldPre := constant.CountToken, common.RetryTimes, common.PreConsumedQuota
	oldRatios, oldCompletion, oldPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, false, true
	constant.CountToken, common.RetryTimes, common.PreConsumedQuota = false, 0, 500
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = oldSQLite, oldMySQL, oldPostgres
		common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = oldMemory, oldRedis, oldBatch, oldLog
		constant.CountToken, common.RetryTimes, common.PreConsumedQuota = oldCount, oldRetry, oldPre
		_ = ratio_setting.UpdateModelRatioByJSONString(oldRatios)
		_ = ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion)
		_ = ratio_setting.UpdateModelPriceByJSONString(oldPrices)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Log{}, &model.ConversationLog{}))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	service.InitHttpClient()
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "typesafe-test", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: typeSafeInitialQuota, Setting: `{"billing_preference":"wallet_only"}`}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Name: "typesafe-test", Key: typeSafeGatewayKey, Status: common.TokenStatusEnabled, RemainQuota: typeSafeInitialQuota, ExpiredTime: -1}).Error)
	ch := &model.Channel{Type: constant.ChannelTypeTypeSafe, Name: "typesafe-test", Key: upstreamKey, Models: "jev-latest", Group: "default", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0)}
	ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: true})
	require.NoError(t, ch.Insert())
	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.ModelRequestRateLimit(), middleware.Distribute())
	engine.POST("/v1/systemone", func(c *gin.Context) { Relay(c, types.RelayFormatTypeSafe) })
	engine.POST("/v1/chat/completions", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAI) })
	return db, engine, ch
}

func requestTypeSafe(t *testing.T, engine http.Handler, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	return w
}

func assertTypeSafeQuota(t *testing.T, db *gorm.DB, used int) {
	t.Helper()
	var user model.User
	var token model.Token
	require.NoError(t, db.First(&user, 1).Error)
	require.NoError(t, db.First(&token, 1).Error)
	require.Equal(t, typeSafeInitialQuota-used, user.Quota)
	require.Equal(t, typeSafeInitialQuota-used, token.RemainQuota)
	require.Equal(t, used, token.UsedQuota)
}

func TestTypeSafeGatewayBillingAndDiscovery(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"models":[{"name":"jev-latest","description":"Latest","release_date":"2026-09-10"},{"name":"jev-preview"}]}`)
			return
		}
		require.Equal(t, "/v1/systemone", r.URL.Path)
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		require.Equal(t, "jev-1.13.0", body["model"])
		require.Equal(t, false, body["extra"])
		calls.Add(1)
		_, _ = io.WriteString(w, typeSafeTestResponse)
	}))
	defer upstream.Close()
	db, engine, ch := setupTypeSafeGateway(t, upstream.URL+"/v1/", "upstream-key")
	ch.ModelMapping = common.GetPointer(`{"jev-latest":"jev-1.13.0"}`)
	require.NoError(t, ch.Save())
	w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, typeSafeTestResponse, w.Body.String())
	assertTypeSafeQuota(t, db, 21)
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, 1000, logs[0].PromptTokens)
	require.Equal(t, 50, logs[0].CompletionTokens)
	require.Equal(t, 21, logs[0].Quota)
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), ch.Key, "")
	require.NoError(t, err)
	require.Equal(t, []string{"jev-latest", "jev-preview"}, names)
	for _, base := range []string{upstream.URL, upstream.URL + "/", upstream.URL + "/v1", upstream.URL + "/v1/"} {
		require.Equal(t, upstream.URL+"/v1/models", resolveFetchModelsURL(ch.Type, base, ""))
	}
	ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: true, ModelMappingFullEnabled: true})
	require.NoError(t, ch.Save())
	w = requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"model":"jev-latest"`)
	require.NotContains(t, w.Body.String(), "jev-1.13.0")
	assertTypeSafeQuota(t, db, 42)
	require.EqualValues(t, 2, calls.Load())
}

func TestTypeSafeRejectsUnsupportedFormatsBeforeUpgrade(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatOpenAIRealtime} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/v1/realtime?model=jev-latest", nil)
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeTypeSafe)
		Relay(c, format)
		require.Equal(t, 400, w.Code)
		require.Contains(t, w.Body.String(), "/v1/systemone")
	}
}

func TestTypeSafeGatewayErrorsRefundAndPrivacy(t *testing.T) {
	for _, status := range []int{401, 422, 429, 529, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := `{"detail":[{"msg":"invalid question","type":"validation_error"}]}`
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			db, engine, ch := setupTypeSafeGateway(t, upstream.URL, "upstream-key")
			w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
			if status == 200 {
				require.Equal(t, 502, w.Code, w.Body.String())
			} else {
				require.Equal(t, status, w.Code, w.Body.String())
				require.JSONEq(t, body, w.Body.String())
				require.Equal(t, "2", w.Header().Get("Retry-After"))
			}
			assertTypeSafeQuota(t, db, 0)
			ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: false})
			require.NoError(t, ch.Save())
			w = requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
			require.NotContains(t, w.Body.String(), "invalid question")
			assertTypeSafeQuota(t, db, 0)
		})
	}
}

func TestTypeSafeGatewayRetryIsolation(t *testing.T) {
	var attempts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer first-key" {
			require.Equal(t, "jev-preview", body["model"])
			require.Equal(t, "first", body["attempt"])
			w.WriteHeader(529)
			_, _ = io.WriteString(w, `{"detail":"busy"}`)
			return
		}
		require.Equal(t, "Bearer second-key", r.Header.Get("Authorization"))
		require.Equal(t, "jev-1.13.0", body["model"])
		require.NotContains(t, body, "attempt")
		require.Equal(t, false, body["extra"])
		_, _ = io.WriteString(w, typeSafeTestResponse)
	}))
	defer upstream.Close()
	db, engine, first := setupTypeSafeGateway(t, upstream.URL, "first-key")
	common.RetryTimes = 1
	first.Priority = common.GetPointer(int64(10))
	first.ModelMapping = common.GetPointer(`{"jev-latest":"jev-preview"}`)
	first.ParamOverride = common.GetPointer(`{"attempt":"first"}`)
	require.NoError(t, first.Save())
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", first.Id).Update("priority", 10).Error)
	second := &model.Channel{Type: constant.ChannelTypeTypeSafe, Name: "second", Key: "second-key", BaseURL: &upstream.URL, Models: "jev-latest", Group: "default", Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(`{"jev-latest":"jev-1.13.0"}`)}
	require.NoError(t, second.Insert())
	w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.JSONEq(t, typeSafeTestResponse, w.Body.String())
	require.EqualValues(t, 2, attempts.Load())
	assertTypeSafeQuota(t, db, 21)
}

func TestTypeSafeGatewayValidationAndTimeout(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer upstream.Close()
	db, engine, _ := setupTypeSafeGateway(t, upstream.URL, "upstream-key")
	w := requestTypeSafe(t, engine, "/v1/systemone", typeSafeTestBody, "invalidtoken")
	require.Equal(t, 401, w.Code)
	w = requestTypeSafe(t, engine, "/v1/chat/completions", `{"model":"jev-latest","messages":[{"role":"user","content":"hi"}]}`, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code)
	w = requestTypeSafe(t, engine, "/v1/systemone", `{"model":"jev-latest","state":false,"questions":{}}`, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code)
	require.Zero(t, calls.Load())
	request := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(typeSafeTestBody))
	ctx, cancel := context.WithTimeout(request.Context(), 100*time.Millisecond)
	defer cancel()
	request = request.WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+typeSafeGatewayKey)
	w = httptest.NewRecorder()
	engine.ServeHTTP(w, request)
	require.GreaterOrEqual(t, w.Code, 500)
	assertTypeSafeQuota(t, db, 0)
}

func TestTypeSafeChannelTest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/systemone", r.URL.Path)
		var request dto.TypeSafeRequest
		require.NoError(t, common.DecodeJson(r.Body, &request))
		require.NotContains(t, request.Fields, "stream")
		require.Contains(t, string(request.Fields["questions"]), `"noul"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, typeSafeTestResponse)
	}))
	defer upstream.Close()
	_, _, ch := setupTypeSafeGateway(t, upstream.URL, "upstream-key")
	result := testChannel(ch, "", "", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.Equal(t, "jev-1.13.0", result.upstreamModel)
}

func TestTypeSafeLiveGateway(t *testing.T) {
	if os.Getenv("TYPESAFE_LIVE_TEST") != "1" {
		t.Skip("set TYPESAFE_LIVE_TEST=1 and TYPESAFE_API_KEY to run")
	}
	key := os.Getenv("TYPESAFE_API_KEY")
	require.NotEmpty(t, key)
	db, engine, ch := setupTypeSafeGateway(t, "https://api.typesafe.ai/v1/", key)
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), key, "")
	require.NoError(t, err)
	require.Contains(t, names, "jev-latest")
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	body := strings.Replace(typeSafeTestBody, `,"extra":false`, "", 1)
	request, err := http.NewRequest("POST", gateway.URL+"/v1/systemone", bytes.NewBufferString(body))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+typeSafeGatewayKey)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(request)
	require.NoError(t, err)
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode, string(responseBody))
	var result struct {
		Model   string         `json:"model"`
		Answers map[string]any `json:"answers"`
		Usage   struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	}
	require.NoError(t, common.Unmarshal(responseBody, &result))
	require.Len(t, result.Answers, 3)
	require.Positive(t, result.Usage.Input)
	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
	require.Equal(t, result.Usage.Input, log.PromptTokens)
	require.Equal(t, result.Usage.Output, log.CompletionTokens)
	assertTypeSafeQuota(t, db, log.Quota)
	t.Logf("HTTP %d, model=%s, input=%d, output=%d, quota=%d", resp.StatusCode, result.Model, result.Usage.Input, result.Usage.Output, log.Quota)
}
