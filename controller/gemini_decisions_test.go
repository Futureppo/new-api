package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const geminiDecisionsBody = `{"model":"gemini-decision-1","input":[{"type":"text","text":"The order arrived damaged."},{"type":"image","data":"YWJj","mime_type":"image/png"}],"questions":{"damaged":{"type":"noul","instructions":"Is it damaged?"},"team":{"type":"choice","instructions":"Which team?","criteria":{"billing":"","support":""}},"severity":{"type":"score","instructions":"Rate severity","criteria":["low","high"]}},"future":{"zero":0,"flag":false,"large":9007199254740993}}`
const geminiDecisionsResponse = `{"answers":{"damaged":{"type":"noul","noul":0,"confidence":1},"team":{"type":"choice","choice":"support","probabilities":{"billing":0,"support":1}},"severity":{"type":"score","score":0,"confidence":1}},"usage":{"total_input_tokens":1000,"input_tokens_by_modality":[{"modality":"text","tokens":900},{"modality":"image","tokens":100}],"total_output_tokens":0,"total_tokens":1000},"future":false}`

func setupGeminiDecisionsGateway(t *testing.T, baseURL string) (*gorm.DB, *gin.Engine, *model.Channel) {
	t.Helper()
	db, engine, ch := setupDecisionsGateway(t, baseURL)
	ch.Type, ch.Models = constant.ChannelTypeGemini, "gemini-decision-1,alias"
	require.NoError(t, ch.Save())
	require.NoError(t, ch.UpdateAbilities(db))
	engine.POST("/v1beta/decisions", func(c *gin.Context) { Relay(c, types.RelayFormatGeminiDecisions) })
	engine.POST("/v1beta/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gemini-decision-1":0.05,"alias":0.2,"gemini-decision-1-openai-decisions":99,"alias-openai-decisions":99}`))
	return db, engine, ch
}

func TestGeminiDecisionsGatewayAuthMappingAndBilling(t *testing.T) {
	for _, path := range []string{"/v1beta/decisions", "/v1/decisions"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/custom/v1beta/decisions", r.URL.Path)
				require.Equal(t, "upstream-key", r.Header.Get("x-goog-api-key"))
				require.Empty(t, r.URL.RawQuery)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "mapped-decision", gjson.GetBytes(body, "model").String())
				require.Equal(t, "0", gjson.GetBytes(body, "override_zero").Raw)
				require.Equal(t, "false", gjson.GetBytes(body, "override_flag").Raw)
				require.Equal(t, "9007199254740993", gjson.GetBytes(body, "future.large").Raw)
				require.JSONEq(t, gjson.Get(geminiDecisionsBody, "input").Raw, gjson.GetBytes(body, "input").Raw)
				require.JSONEq(t, gjson.Get(geminiDecisionsBody, "questions").Raw, gjson.GetBytes(body, "questions").Raw)
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, geminiDecisionsResponse)
			}))
			defer upstream.Close()
			db, engine, ch := setupGeminiDecisionsGateway(t, upstream.URL+"/custom/v1beta/")
			ch.ModelMapping = common.GetPointer(`{"gemini-decision-1":"mapped-decision","alias":"mapped-decision"}`)
			ch.ParamOverride = common.GetPointer(`{"override_zero":0,"override_flag":false}`)
			require.NoError(t, ch.Save())
			auths := []string{"bearer"}
			if path == "/v1beta/decisions" {
				auths = append(auths, "header", "query")
			}
			for _, auth := range auths {
				url := path
				if auth == "query" {
					url += "?key=" + typeSafeGatewayKey
				}
				r := httptest.NewRequest(http.MethodPost, url, strings.NewReader(geminiDecisionsBody))
				r.Header.Set("Content-Type", "application/json")
				if auth == "header" {
					r.Header.Set("x-goog-api-key", typeSafeGatewayKey)
				}
				if auth == "bearer" {
					r.Header.Set("Authorization", "Bearer "+typeSafeGatewayKey)
				}
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, r)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, geminiDecisionsResponse, w.Body.String())
			}
			assertTypeSafeQuota(t, db, 50*len(auths))
			w := requestTypeSafe(t, engine, path, strings.Replace(geminiDecisionsBody, `"model":"gemini-decision-1"`, `"model":"alias"`, 1), typeSafeGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			assertTypeSafeQuota(t, db, 50*len(auths)+200)
			var log model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Last(&log).Error)
			require.Equal(t, "alias", log.ModelName)
			require.Equal(t, 1000, log.PromptTokens)
			require.Zero(t, log.CompletionTokens)
			require.NotContains(t, log.Other, "openai-decisions")
			require.Equal(t, path, gjson.Get(log.Other, "request_path").String())
			require.EqualValues(t, len(auths)+1, calls.Load())
		})
	}
}

func TestGeminiDecisionsGatewayValidationPermissionsAndChat(t *testing.T) {
	// Permission errors call i18n directly. Its process-wide sync.Once also
	// installs common.TranslateMessage, so isolate it from tests expecting raw keys.
	if os.Getenv("NEW_API_GEMINI_AUTH_TEST_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGeminiDecisionsGatewayValidationPermissionsAndChat$", "-test.count=1")
		cmd.Env = append(os.Environ(), "NEW_API_GEMINI_AUTH_TEST_CHILD=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	require.NoError(t, i18n.Init())
	for _, path := range []string{"/v1beta/decisions", "/v1/decisions"} {
		t.Run(path, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer upstream.Close()
			db, engine, ch := setupGeminiDecisionsGateway(t, upstream.URL)
			w := requestTypeSafe(t, engine, path, geminiDecisionsBody, "invalid")
			require.Equal(t, 401, w.Code)
			for _, body := range []string{
				strings.TrimSuffix(geminiDecisionsBody, "}") + `,"stream":true}`,
				`{"model":"gemini-decision-1","input":"hi","questions":[]}`,
			} {
				w = requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
				require.Equal(t, 400, w.Code, w.Body.String())
			}
			ch.ParamOverride = common.GetPointer(`{"stream":true}`)
			require.NoError(t, ch.Save())
			w = requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
			require.Equal(t, 400, w.Code, w.Body.String())
			ch.ParamOverride = nil
			require.NoError(t, ch.Save())
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"model_limits_enabled": true, "model_limits": "alias"}).Error)
			w = requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
			require.Equal(t, 403, w.Code, w.Body.String())
			require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 1).Update("model_limits_enabled", false).Error)
			ch.Group = "unavailable-group"
			require.NoError(t, ch.Update())
			w = requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
			require.Equal(t, 503, w.Code, w.Body.String())
			ch.Group = "default"
			require.NoError(t, ch.Update())
			for _, chat := range []struct{ path, body string }{
				{"/v1/chat/completions", `{"model":"gemini-decision-1","messages":[{"role":"user","content":"hi"}]}`},
				{"/v1beta/models/gemini-decision-1:generateContent", `{"contents":[{"parts":[{"text":"hi"}]}]}`},
			} {
				w = requestTypeSafe(t, engine, chat.path, chat.body, typeSafeGatewayKey)
				require.Equal(t, 400, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "/v1beta/decisions")
			}
			require.Zero(t, calls.Load())
			require.Eventually(t, func() bool { var u model.User; return db.First(&u, 1).Error == nil && u.Quota == typeSafeInitialQuota }, time.Second, 10*time.Millisecond)
			assertTypeSafeQuota(t, db, 0)
		})
	}
}

func TestGeminiDecisionsGatewayRetryIsolation(t *testing.T) {
	for _, path := range []string{"/v1beta/decisions", "/v1/decisions"} {
		t.Run(path, func(t *testing.T) {
			var attempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("x-goog-api-key") == "first-key" {
					require.Equal(t, "first-model", gjson.GetBytes(body, "model").String())
					require.True(t, gjson.GetBytes(body, "first_only").Bool())
					w.WriteHeader(500)
					_, _ = io.WriteString(w, `{"error":{"code":500,"message":"busy","status":"INTERNAL"}}`)
					return
				}
				require.Equal(t, "second-key", r.Header.Get("x-goog-api-key"))
				require.Equal(t, "second-model", gjson.GetBytes(body, "model").String())
				require.False(t, gjson.GetBytes(body, "first_only").Exists())
				require.Equal(t, "false", gjson.GetBytes(body, "future.flag").Raw)
				_, _ = io.WriteString(w, geminiDecisionsResponse)
			}))
			defer upstream.Close()
			db, engine, first := setupGeminiDecisionsGateway(t, upstream.URL)
			common.RetryTimes = 1
			first.Key, first.Priority = "first-key", common.GetPointer(int64(10))
			first.ModelMapping = common.GetPointer(`{"gemini-decision-1":"first-model"}`)
			first.ParamOverride = common.GetPointer(`{"first_only":true}`)
			require.NoError(t, first.Update())
			second := &model.Channel{Type: constant.ChannelTypeGemini, Name: "gemini-retry", Key: "second-key", BaseURL: &upstream.URL, Models: "gemini-decision-1", Group: "default", Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(`{"gemini-decision-1":"second-model"}`)}
			require.NoError(t, second.Insert())
			w := requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.EqualValues(t, 2, attempts.Load())
			assertTypeSafeQuota(t, db, 50)
		})
	}
}

func TestGeminiDecisionsGatewayBlockedAndRefunds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		body              string
		wantStatus, quota int
	}{
		{"blocked", 200, `{"answers":{},"prompt_feedback":{"block_reason":"SAFETY"},"usage":{"total_input_tokens":1000,"total_output_tokens":0}}`, 200, 50},
		{"blocked without usage", 200, `{"prompt_feedback":{"block_reason":"SAFETY"}}`, 200, 0},
		{"bad usage", 200, `{"answers":{"q":{}},"usage":{"total_input_tokens":-1,"total_output_tokens":0}}`, 502, 0},
		{"missing usage", 200, `{"answers":{"q":{}}}`, 502, 0},
		{"upstream invalid", 400, `{"error":{"code":400,"message":"invalid","status":"INVALID_ARGUMENT"}}`, 400, 0},
		{"upstream busy", 429, `{"error":{"code":429,"message":"busy","status":"RESOURCE_EXHAUSTED"}}`, 429, 0},
		{"upstream error", 500, `{"error":{"code":500,"message":"busy","status":"INTERNAL"}}`, 500, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			db, engine, _ := setupGeminiDecisionsGateway(t, upstream.URL)
			for i, path := range []string{"/v1beta/decisions", "/v1/decisions"} {
				w := requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
				require.Equal(t, tc.wantStatus, w.Code, w.Body.String())
				if tc.wantStatus == 200 {
					require.Equal(t, tc.body, w.Body.String())
				}
				wantQuota := tc.quota * (i + 1)
				require.Eventually(t, func() bool {
					var u model.User
					return db.First(&u, 1).Error == nil && u.Quota == typeSafeInitialQuota-wantQuota
				}, time.Second, 10*time.Millisecond)
				assertTypeSafeQuota(t, db, wantQuota)
			}
		})
	}
}

func TestGeminiDecisionsPlaygroundAndChannelTest(t *testing.T) {
	initPlaygroundTestColumns(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1beta/decisions", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "gemini-decision-1", gjson.GetBytes(body, "model").String())
		require.True(t, gjson.GetBytes(body, "questions").IsObject())
		require.False(t, gjson.GetBytes(body, "stream").Bool())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, geminiDecisionsResponse)
	}))
	defer upstream.Close()
	db, _, ch := setupGeminiDecisionsGateway(t, upstream.URL)
	ch.ModelMapping = common.GetPointer(`{"alias":"gemini-decision-1"}`)
	require.NoError(t, ch.Save())
	require.Equal(t, "gemini-decisions", normalizeChannelTestEndpoint(ch, "alias", ""))
	for _, endpoint := range []string{"gemini-decisions", "openai-decisions"} {
		result := testChannel(ch, "alias", endpoint, true)
		require.NoError(t, result.localErr)
		require.Nil(t, result.newAPIError)
		require.Equal(t, 1000, result.usage.PromptTokens)
		require.Zero(t, result.usage.CompletionTokens)
	}
	var logs []model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, 200, log.Quota)
		require.False(t, log.IsStream)
	}
	engine := playgroundTestRouter()
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pg/endpoints?model=alias&group=default", nil))
	require.Equal(t, 200, w.Code, w.Body.String())
	for _, endpoint := range []string{"gemini-decisions", "openai-decisions"} {
		require.Contains(t, w.Body.String(), fmt.Sprintf(`"%s"`, endpoint))
		res := requestTypeSafe(t, engine, "/pg/protocol/"+endpoint, `{"model":"alias","group":"default","native":true,"body":{"input":"Hello","questions":{"q":{"type":"noul","instructions":"Is this a greeting?"}}}}`, "")
		require.Equal(t, 200, res.Code, res.Body.String())
		require.Equal(t, geminiDecisionsResponse, res.Body.String())
	}
	require.Contains(t, w.Body.String(), `"variant":"gemini"`)
}

func TestGeminiDecisionsPassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, geminiDecisionsBody, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, geminiDecisionsResponse)
	}))
	defer upstream.Close()
	db, engine, ch := setupGeminiDecisionsGateway(t, upstream.URL)
	ch.ModelMapping = common.GetPointer(`{"gemini-decision-1":"other"}`)
	ch.ParamOverride = common.GetPointer(`{"stream":true}`)
	settings := model_setting.GetGlobalSettings()
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = oldPassThrough })
	for _, global := range []bool{false, true} {
		settings.PassThroughRequestEnabled = global
		ch.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: !global})
		require.NoError(t, ch.Save())
		for _, path := range []string{"/v1beta/decisions", "/v1/decisions"} {
			w := requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
		}
	}
	assertTypeSafeQuota(t, db, 200)
}

func TestGeminiDecisionsStatusMappingAndNativeChannelGuard(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":{"code":503,"message":"busy","status":"UNAVAILABLE"}}`)
	}))
	defer upstream.Close()
	db, engine, ch := setupGeminiDecisionsGateway(t, upstream.URL)
	ch.StatusCodeMapping = common.GetPointer(`{"503":"429"}`)
	require.NoError(t, ch.Save())
	for _, path := range []string{"/v1beta/decisions", "/v1/decisions"} {
		w := requestTypeSafe(t, engine, path, geminiDecisionsBody, typeSafeGatewayKey)
		require.Equal(t, 429, w.Code, w.Body.String())
	}
	ch.Type = constant.ChannelTypeOpenAI
	require.NoError(t, ch.Update())
	w := requestTypeSafe(t, engine, "/v1beta/decisions", geminiDecisionsBody, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "Gemini channel")
	require.EqualValues(t, 2, calls.Load())
	require.Eventually(t, func() bool { var u model.User; return db.First(&u, 1).Error == nil && u.Quota == typeSafeInitialQuota }, time.Second, 10*time.Millisecond)
	assertTypeSafeQuota(t, db, 0)
}
