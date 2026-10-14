package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProbeGuardRelayBlocksBeforePricingAndUpstream(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		format     types.RelayFormat
	}{
		{"/v1/chat/completions", `{"model":"probe-test","messages":[{"role":"user","content":"say OK"}]}`, types.RelayFormatOpenAI},
		{"/v1/responses", `{"model":"probe-test","input":"say OK"}`, types.RelayFormatOpenAIResponses},
		{"/v1/messages", `{"model":"probe-test","max_tokens":1,"messages":[{"role":"user","content":"say OK"}]}`, types.RelayFormatClaude},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer upstream.Close()
			db, engine, channel := setupTypeSafeGateway(t, upstream.URL, "test-key")
			require.NoError(t, i18n.Init())
			require.NoError(t, db.AutoMigrate(&model.ProbeUserAbuseState{}))
			require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("role", common.RoleCommonUser).Error)
			channel.Type, channel.Models = constant.ChannelTypeOpenAI, "probe-test"
			require.NoError(t, channel.Save())
			require.NoError(t, channel.UpdateAbilities(db))
			if tc.path != "/v1/chat/completions" {
				engine.POST(tc.path, func(c *gin.Context) { Relay(c, tc.format) })
			}
			oldCfg := setting.GetProbeGuardSetting()
			oldValues, err := config.ConfigToMap(&oldCfg)
			require.NoError(t, err)
			require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), map[string]string{
				"enabled": "true", "dry_run": "false", "text_rules": "say OK", "whitelist_user_ids": "", "first_ban_reason": "relay test {count}", "batch_request_count": "1", "batch_window_seconds": "30",
			}))
			t.Cleanup(func() {
				require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), oldValues))
			})
			// No price is configured: the guard must run even before price lookup.
			w := requestTypeSafe(t, engine, tc.path, tc.body, typeSafeGatewayKey)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "relay test 1")
			require.Zero(t, upstreamCalls.Load())
			assertTypeSafeQuota(t, db, 0)
			state, err := model.GetProbeUserAbuseState(1)
			require.NoError(t, err)
			require.Equal(t, 1, state.OffenseCount)
			var consumptionLogs int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumptionLogs).Error)
			require.Zero(t, consumptionLogs)
		})
	}
}

func TestProbeGuardPreviewAndAccountResetAPI(t *testing.T) {
	db := setupEnhancementOptionControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.ProbeUserAbuseState{}, &model.Log{}))
	user := model.User{Username: "probe-api", Role: common.RoleCommonUser, Status: common.UserStatusDisabled, DisableReason: "manual", DisableUntil: 0}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.ProbeUserAbuseState{UserId: user.Id, OffenseCount: 3, LastOffenseAt: time.Now().Unix()}).Error)
	router := gin.New()
	router.POST("/preview", PreviewProbeGuardText)
	router.GET("/users/:id", GetProbeGuardUserState)
	router.DELETE("/users/:id", ResetProbeGuardUserState)
	for _, tc := range []struct {
		body             string
		success, matched bool
	}{
		{`{"text":" ＳＡＹ　ＯＫ ","rules":"say OK"}`, true, true},
		{`{"text":"say OK","rules":""}`, true, false},
		{`{"text":"Explain say OK","rules":"say OK"}`, true, false},
		{`{"text":"say OK","rules":false}`, false, false},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/preview", strings.NewReader(tc.body)))
		var response struct {
			Success bool `json:"success"`
			Data    struct {
				Matched bool `json:"matched"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, tc.success, response.Success)
		require.Equal(t, tc.matched, response.Data.Matched)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/"+strconv.Itoa(user.Id), nil))
	require.True(t, decodeEnhancementAPIResponse(t, w).Success)
	state, err := model.GetProbeUserAbuseState(user.Id)
	require.NoError(t, err)
	require.Zero(t, state.OffenseCount)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, user.Status)
	require.Equal(t, "manual", user.DisableReason)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/"+strconv.Itoa(user.Id), nil))
	require.True(t, decodeEnhancementAPIResponse(t, w).Success)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/users/-1", nil))
	require.False(t, decodeEnhancementAPIResponse(t, w).Success)
}
