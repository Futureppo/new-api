package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestProbeGuardAuthBlocksEveryKeyAndProtocol(t *testing.T) {
	for _, useRedis := range []bool{false, true} {
		t.Run(map[bool]string{false: "database", true: "redis"}[useRedis], func(t *testing.T) {
			setupAuthMiddlewareTestDB(t)
			require.NoError(t, i18n.Init())
			require.NoError(t, model.DB.AutoMigrate(&model.ProbeUserAbuseState{}, &model.Log{}))
			if useRedis {
				address := os.Getenv("TEST_USER_DISABLE_REDIS_ADDR")
				if address == "" {
					t.Skip("set TEST_USER_DISABLE_REDIS_ADDR to test an isolated Redis server")
				}
				oldClient := common.RDB
				client := redis.NewClient(&redis.Options{Addr: address})
				require.NoError(t, client.Ping(context.Background()).Err())
				common.RDB, common.RedisEnabled = client, true
				t.Cleanup(func() { common.RedisEnabled = false; common.RDB = oldClient; _ = client.Close() })
			}
			oldCfg := setting.GetProbeGuardSetting()
			oldValues, err := config.ConfigToMap(&oldCfg)
			require.NoError(t, err)
			require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), map[string]string{"enabled": "true", "dry_run": "false", "text_rules": "say OK", "whitelist_user_ids": "", "first_user_ban_minutes": "7", "first_ban_reason": "custom reason {count} {minutes} {until}", "batch_request_count": "3", "batch_window_seconds": "30"}))
			t.Cleanup(func() {
				require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), oldValues))
			})
			user := createAuthMiddlewareUser(t, common.UserStatusEnabled, "")
			keyA, keyB := "probeautha", "probeauthb"
			createAuthMiddlewareToken(t, user.Id, keyA)
			createAuthMiddlewareToken(t, user.Id, keyB)
			if useRedis {
				t.Cleanup(func() { model.InvalidateUserAuthCaches(user.Id) })
			}
			forwarded := 0
			router := gin.New()
			router.POST("/v1/chat/completions", TokenAuth(), func(c *gin.Context) {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.DecodeJson(c.Request.Body, &request))
				err := service.CheckProbeGuard(c, &relaycommon.RelayInfo{UserId: user.Id, TokenId: 1, OriginModelName: "test", StartTime: time.Now(), Request: &request})
				if err != nil {
					c.JSON(err.StatusCode, gin.H{"error": err.ToOpenAIError()})
					return
				}
				forwarded++
				c.Status(http.StatusOK)
			})
			for _, path := range []string{"/v1/responses", "/v1/messages"} {
				router.POST(path, TokenAuth(), func(c *gin.Context) { forwarded++; c.Status(http.StatusOK) })
			}
			send := func(path, key, body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer sk-"+key)
				req.Header.Set("Accept-Language", "zh-CN")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			// Populate auth caches for both keys before imposing a ban.
			for _, key := range []string{keyA, keyB} {
				require.Equal(t, http.StatusOK, send("/v1/chat/completions", key, `{"messages":[{"role":"user","content":"hello"}]}`).Code)
			}
			if useRedis {
				require.Eventually(t, func() bool { return common.RDB.Exists(context.Background(), "user:"+strconv.Itoa(user.Id)).Val() > 0 }, time.Second, time.Millisecond)
			}
			forwarded = 0
			// Identical model/prompt requests from different keys count separately.
			for _, key := range []string{keyA, keyB} {
				require.Equal(t, http.StatusOK, send("/v1/chat/completions", key, `{"messages":[{"role":"user","content":"say OK"}]}`).Code)
			}
			require.Equal(t, 2, forwarded)
			forwarded = 0
			first := send("/v1/chat/completions", keyA, `{"messages":[{"role":"user","content":"say OK"}]}`)
			require.Equal(t, http.StatusForbidden, first.Code)
			require.Contains(t, first.Body.String(), "custom reason 1 7")
			for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages?beta=true"} {
				for _, key := range []string{keyA, keyB} {
					w := send(path, key, `{}`)
					require.Equal(t, http.StatusForbidden, w.Code)
					require.Contains(t, w.Body.String(), "custom reason 1 7")
					if strings.Contains(path, "messages") {
						require.Contains(t, w.Body.String(), `"type":"error"`)
					} else {
						require.Contains(t, w.Body.String(), `"code":"access_denied"`)
					}
				}
			}
			require.Zero(t, forwarded)
			state, err := model.GetProbeUserAbuseState(user.Id)
			require.NoError(t, err)
			require.Equal(t, 1, state.OffenseCount)
		})
	}
}
