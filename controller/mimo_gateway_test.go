package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/mimo"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const mimoGatewayKey = "mimogatewaytesttoken"
const mimoInitialQuota = 100000

func mimoMockResponse(claude, stream bool) string {
	if claude {
		if !stream {
			return `{"id":"msg1","type":"message","role":"assistant","model":"mimo-v2.6-pro-ultraspeed","content":[{"type":"thinking","thinking":"checking","signature":"sig"},{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":60,"output_tokens":10,"cache_read_input_tokens":40}}`
		}
		return "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg1","type":"message","role":"assistant","model":"mimo-v2.6-pro-ultraspeed","content":[],"usage":{"input_tokens":60,"output_tokens":0,"cache_read_input_tokens":40}}}` + "\n\n" +
			"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
			"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}` + "\n\n" +
			"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n" +
			"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":60,"output_tokens":10,"cache_read_input_tokens":40}}` + "\n\n" +
			"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
	}
	usage := `"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":3}}`
	if !stream {
		return `{"id":"chat1","object":"chat.completion","model":"mimo-v2.6-pro-ultraspeed","choices":[{"index":0,"message":{"role":"assistant","content":"OK","reasoning_content":"checking"},"finish_reason":"stop"}],` + usage + `}`
	}
	return "data: " + `{"id":"chat1","object":"chat.completion.chunk","model":"mimo-v2.6-pro-ultraspeed","choices":[{"index":0,"delta":{"content":"OK","reasoning_content":"checking"},"finish_reason":null}]}` + "\n\n" +
		"data: " + `{"id":"chat1","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
		"data: " + `{"id":"chat1","choices":[],` + usage + `}` + "\n\ndata: [DONE]\n\n"
}

func TestMiMoGatewayBillingAndProtocols(t *testing.T) {
	for _, claude := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("claude=%t/stream=%t", claude, stream), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "upstream-test-key", r.Header.Get("api-key"))
					require.Empty(t, r.Header.Get("Authorization"))
					path := "/v1/chat/completions"
					if claude {
						path = "/anthropic/v1/messages"
						require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
					}
					require.Equal(t, path, r.URL.Path)
					var body map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &body))
					require.Equal(t, mimo.DefaultTestModel, body["model"])
					require.Equal(t, float64(0), body["max_tokens"])
					require.Equal(t, float64(0), body["temperature"])
					require.Equal(t, stream, body["stream"])
					require.Equal(t, map[string]any{"type": "disabled"}, body["thinking"])
					if stream && !claude {
						require.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
					}
					w.Header().Set("Content-Type", "application/json")
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					_, _ = io.WriteString(w, mimoMockResponse(claude, stream))
				}))
				defer upstream.Close()
				db, engine, _ := setupMiMoGateway(t, upstream.URL+"/anthropic/v1/", "upstream-test-key")
				path := "/v1/chat/completions"
				if claude {
					path = "/v1/messages"
				}
				body := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"Hi"}],"max_tokens":0,"temperature":0,"thinking":{"type":"disabled"},"stream":%t}`, mimo.DefaultTestModel, stream)
				if stream && !claude {
					body = strings.TrimSuffix(body, "}") + `,"stream_options":{"include_usage":true}}`
				}
				w := requestMiMo(t, engine, path, body, mimoGatewayKey)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "OK")
				if stream {
					terminal := "data: [DONE]"
					if claude {
						terminal = "event: message_stop"
					}
					require.Contains(t, w.Body.String(), terminal)
				}
				var log model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
				require.Equal(t, 10, log.CompletionTokens)
				// (60 uncached + 40 cached / 120 + 10 output * 2) * 3 / 1000 * RMB.
				require.Equal(t, 17, log.Quota)
				assertMiMoQuota(t, db, log.Quota)
			})
		}
	}
}

func TestMiMoGatewayUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"unsupported parameter","type":"invalid_request_error","code":"invalid_parameter"}}`)
	}))
	defer upstream.Close()
	db, engine, _ := setupMiMoGateway(t, upstream.URL, "upstream-test-key")
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		w := requestMiMo(t, engine, path, `{"model":"mimo-v2.6-pro-ultraspeed","max_tokens":64,"messages":[{"role":"user","content":"Hi"}]}`, mimoGatewayKey)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "unsupported parameter")
	}
	assertMiMoQuota(t, db, 0)
}

func TestMiMoGatewayPreservesToolHistory(t *testing.T) {
	for _, claude := range []bool{false, true} {
		t.Run(fmt.Sprintf("claude=%t", claude), func(t *testing.T) {
			messages := `[{"role":"user","content":"Echo OK"},{"role":"assistant","content":"","reasoning_content":"history to retain","tool_calls":[{"id":"call1","type":"function","function":{"name":"echo","arguments":"{\"value\":0}"}}]},{"role":"tool","tool_call_id":"call1","content":"OK"}]`
			tools := `[{"type":"function","function":{"name":"echo","parameters":{"type":"object","properties":{"value":{"type":"integer"}},"additionalProperties":false}}}]`
			path := "/v1/chat/completions"
			if claude {
				messages = `[{"role":"user","content":"Echo OK"},{"role":"assistant","content":[{"type":"thinking","thinking":"history to retain","signature":"sig"},{"type":"tool_use","id":"call1","name":"echo","input":{"value":0}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"OK","is_error":false}]}]`
				tools = `[{"name":"echo","input_schema":{"type":"object","properties":{"value":{"type":"integer"}},"additionalProperties":false}}]`
				path = "/v1/messages"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &got))
				require.NotContains(t, got, "thinking", "absent thinking must retain the provider default")
				for key, expected := range map[string]string{"messages": messages, "tools": tools} {
					data, err := common.Marshal(got[key])
					require.NoError(t, err)
					require.JSONEq(t, expected, string(data))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, mimoMockResponse(claude, false))
			}))
			defer upstream.Close()
			_, engine, _ := setupMiMoGateway(t, upstream.URL, "upstream-test-key")
			body := fmt.Sprintf(`{"model":"%s","max_tokens":64,"messages":%s,"tools":%s}`, mimo.DefaultTestModel, messages, tools)
			w := requestMiMo(t, engine, path, body, mimoGatewayKey)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		})
	}
}

func TestMiMoAdminChannelTest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		require.NoError(t, common.DecodeJson(r.Body, &body))
		require.Equal(t, mimo.DefaultTestModel, body.Model)
		require.Equal(t, "upstream-test-key", r.Header.Get("api-key"))
		w.Header().Set("Content-Type", "application/json")
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		_, _ = io.WriteString(w, mimoMockResponse(r.URL.Path == "/anthropic/v1/messages", body.Stream))
	}))
	defer upstream.Close()
	_, _, ch := setupMiMoGateway(t, upstream.URL, "upstream-test-key")
	require.NoError(t, validateChannel(ch, true))
	for _, endpoint := range []string{"openai", "anthropic"} {
		for _, stream := range []bool{false, true} {
			result := testChannel(ch, "", endpoint, stream)
			require.NoError(t, result.localErr)
			require.Nil(t, result.newAPIError)
		}
	}
}

// Opt-in smoke test: real provider calls through an ephemeral local gateway.
// Credentials remain in the invoking process environment; no channel DB persists.
func TestMiMoLiveGateway(t *testing.T) {
	if os.Getenv("MIMO_LIVE_TEST") != "1" {
		t.Skip("set MIMO_LIVE_TEST=1 and MIMO_API_KEY to run")
	}
	key := os.Getenv("MIMO_API_KEY")
	require.NotEmpty(t, key)
	db, engine, ch := setupMiMoGateway(t, "https://api.xiaomimimo.com/v1/", key)
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), key, "")
	require.NoError(t, err)
	require.Contains(t, names, mimo.DefaultTestModel)
	gateway := httptest.NewServer(engine)
	defer gateway.Close()
	client := &http.Client{Timeout: 45 * time.Second}
	for _, path := range []string{"/v1/chat/completions", "/v1/messages"} {
		for _, stream := range []bool{false, true} {
			body := fmt.Sprintf(`{"model":"%s","messages":[{"role":"user","content":"Reply with exactly OK."}],"max_tokens":64,"thinking":{"type":"disabled"},"stream":%t}`, mimo.DefaultTestModel, stream)
			if stream && path == "/v1/chat/completions" {
				body = strings.TrimSuffix(body, "}") + `,"stream_options":{"include_usage":true}}`
			}
			r, err := http.NewRequest(http.MethodPost, gateway.URL+path, bytes.NewBufferString(body))
			require.NoError(t, err)
			r.Header.Set("Authorization", "Bearer "+mimoGatewayKey)
			r.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(r)
			require.NoError(t, err)
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode, string(data))
			require.Contains(t, string(data), "OK")
			require.Contains(t, string(data), `"usage"`)
			t.Logf("%s stream=%t HTTP %d", path, stream, resp.StatusCode)
		}
	}
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 4)
	used := 0
	for _, log := range logs {
		require.Positive(t, log.PromptTokens)
		require.Positive(t, log.CompletionTokens)
		require.Positive(t, log.Quota)
		used += log.Quota
		t.Logf("usage: input=%d output=%d quota=%d", log.PromptTokens, log.CompletionTokens, log.Quota)
	}
	assertMiMoQuota(t, db, used)
}

func setupMiMoGateway(t *testing.T, baseURL, upstreamKey string) (*gorm.DB, *gin.Engine, *model.Channel) {
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
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 60
	oldRatios, oldCompletion, oldPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	model.DB, model.LOG_DB = db, db
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, false, true
	constant.CountToken, common.RetryTimes, common.PreConsumedQuota = false, 0, 500
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
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
	oldCache, oldCreate := ratio_setting.CacheRatio2JSONString(), ratio_setting.CreateCacheRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateCacheRatioByJSONString(oldCache)
		_ = ratio_setting.UpdateCreateCacheRatioByJSONString(oldCreate)
	})
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{}`))
	service.InitHttpClient()
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "mimo-test", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default", Quota: mimoInitialQuota, Setting: `{"billing_preference":"wallet_only"}`}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Name: "mimo-test", Key: mimoGatewayKey, Status: common.TokenStatusEnabled, RemainQuota: mimoInitialQuota, ExpiredTime: -1}).Error)
	ch := &model.Channel{Type: constant.ChannelTypeMiMo, Name: "mimo-test", Key: upstreamKey, Models: mimo.DefaultTestModel, Group: "default", BaseURL: &baseURL, Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0)}
	ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: true})
	require.NoError(t, ch.Insert())
	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.ModelRequestRateLimit(), middleware.Distribute())
	engine.POST("/v1/messages", func(c *gin.Context) { Relay(c, types.RelayFormatClaude) })
	engine.POST("/v1/chat/completions", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAI) })
	return db, engine, ch
}

func requestMiMo(t *testing.T, engine http.Handler, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	return w
}

func assertMiMoQuota(t *testing.T, db *gorm.DB, used int) {
	t.Helper()
	var user model.User
	var token model.Token
	require.NoError(t, db.First(&user, 1).Error)
	require.NoError(t, db.First(&token, 1).Error)
	require.Equal(t, mimoInitialQuota-used, user.Quota)
	require.Equal(t, mimoInitialQuota-used, token.RemainQuota)
	require.Equal(t, used, token.UsedQuota)
}
