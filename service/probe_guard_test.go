package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupProbeGuardTest(t *testing.T, dryRun bool) {
	t.Helper()

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldRedisEnabled := common.RedisEnabled
	oldCfg := setting.GetProbeGuardSetting()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.IPBan{}, &model.ProbeIPAbuseState{}, &model.Log{}))
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	resetProbeGuardMemoryForTest()
	updateProbeGuardTestConfig(t, map[string]string{
		"enabled":                 "true",
		"dry_run":                 strconv.FormatBool(dryRun),
		"window_seconds":          "60",
		"distinct_model_count":    "5",
		"first_ip_ban_minutes":    "10",
		"second_ip_ban_minutes":   "60",
		"permanent_offense_count": "3",
		"offense_dedupe_seconds":  "10",
		"max_ips_per_offense":     "32",
		"whitelist_user_ids":      "",
	})

	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.RedisEnabled = oldRedisEnabled
		resetProbeGuardMemoryForTest()
		restoreProbeGuardTestConfig(t, oldCfg)
	})
}

func updateProbeGuardTestConfig(t *testing.T, values map[string]string) {
	t.Helper()
	cfg := config.GlobalConfig.Get("probe_guard_setting")
	require.NotNil(t, cfg)
	require.NoError(t, config.UpdateConfigFromMap(cfg, values))
}

func restoreProbeGuardTestConfig(t *testing.T, cfg setting.ProbeGuardSetting) {
	t.Helper()
	updateProbeGuardTestConfig(t, map[string]string{
		"enabled":                 strconv.FormatBool(cfg.Enabled),
		"dry_run":                 strconv.FormatBool(cfg.DryRun),
		"window_seconds":          strconv.Itoa(cfg.WindowSeconds),
		"distinct_model_count":    strconv.Itoa(cfg.DistinctModelCount),
		"first_ip_ban_minutes":    strconv.Itoa(cfg.FirstIPBanMinutes),
		"second_ip_ban_minutes":   strconv.Itoa(cfg.SecondIPBanMinutes),
		"permanent_offense_count": strconv.Itoa(cfg.PermanentOffenseCount),
		"offense_dedupe_seconds":  strconv.Itoa(cfg.OffenseDedupeSeconds),
		"max_ips_per_offense":     strconv.Itoa(cfg.MaxIPsPerOffense),
		"whitelist_user_ids":      cfg.WhitelistUserIDs,
	})
}

func seedProbeGuardUser(t *testing.T, userId int) {
	seedProbeGuardUserWithRole(t, userId, common.RoleCommonUser)
}

func seedProbeGuardUserWithRole(t *testing.T, userId int, role int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{
		Id:          userId,
		Username:    fmt.Sprintf("probe-user-%d", userId),
		DisplayName: fmt.Sprintf("probe-user-%d", userId),
		Role:        role,
		Status:      common.UserStatusEnabled,
		AffCode:     fmt.Sprintf("pg-%d", userId),
	}).Error)
}

func newProbeGuardContext(ip string, username string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = ip + ":12345"
	req.Header.Set("User-Agent", "curl/8.19.0")
	c.Request = req
	c.Set("username", username)
	return c
}

func fireProbeGuardModel(t *testing.T, userId int, ip string, modelName string) *types.NewAPIError {
	t.Helper()
	c := newProbeGuardContext(ip, fmt.Sprintf("probe-user-%d", userId))
	return CheckProbeGuard(c, &relaycommon.RelayInfo{
		UserId:          userId,
		OriginModelName: modelName,
	})
}

func fireProbeGuardBurst(t *testing.T, userId int, ip string) *types.NewAPIError {
	t.Helper()
	var apiErr *types.NewAPIError
	for i := 0; i < 5; i++ {
		apiErr = fireProbeGuardModel(t, userId, ip, fmt.Sprintf("probe-model-%d", i))
	}
	return apiErr
}

func TestProbeGuardDryRunRecordsWithoutPenalties(t *testing.T) {
	setupProbeGuardTest(t, true)
	seedProbeGuardUser(t, 1001)

	apiErr := fireProbeGuardBurst(t, 1001, "8.8.8.8")
	require.Nil(t, apiErr)

	var state model.ProbeIPAbuseState
	err := model.DB.Where("target_ip = ?", "8.8.8.8").First(&state).Error
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))

	var ipBanCount int64
	require.NoError(t, model.DB.Model(&model.IPBan{}).Count(&ipBanCount).Error)
	require.Equal(t, int64(0), ipBanCount)

	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", 1001, model.LogTypeManage).Count(&logCount).Error)
	require.Equal(t, int64(1), logCount)
}

func TestProbeGuardEscalatesTemporaryThenPermanentPenalty(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1002)

	firstErr := fireProbeGuardBurst(t, 1002, "8.8.8.8")
	require.NotNil(t, firstErr)
	require.Equal(t, http.StatusTooManyRequests, firstErr.StatusCode)

	state, err := model.GetProbeIPAbuseState("8.8.8.8")
	require.NoError(t, err)
	require.Equal(t, 1, state.OffenseCount)
	ban, err := model.GetIPBanByTarget("8.8.8.8")
	require.NoError(t, err)
	require.Greater(t, ban.ExpiresAt, common.GetTimestamp())

	resetProbeGuardMemoryForTest()
	secondErr := fireProbeGuardBurst(t, 1002, "8.8.8.8")
	require.NotNil(t, secondErr)
	require.Equal(t, http.StatusTooManyRequests, secondErr.StatusCode)
	state, err = model.GetProbeIPAbuseState("8.8.8.8")
	require.NoError(t, err)
	require.Equal(t, 2, state.OffenseCount)
	ban, err = model.GetIPBanByTarget("8.8.8.8")
	require.NoError(t, err)
	require.Greater(t, ban.ExpiresAt, common.GetTimestamp()+3000)

	resetProbeGuardMemoryForTest()
	thirdErr := fireProbeGuardBurst(t, 1002, "8.8.8.8")
	require.NotNil(t, thirdErr)
	require.Equal(t, http.StatusForbidden, thirdErr.StatusCode)
	state, err = model.GetProbeIPAbuseState("8.8.8.8")
	require.NoError(t, err)
	require.Equal(t, 3, state.OffenseCount)

	var user model.User
	require.NoError(t, model.DB.Where("id = ?", 1002).First(&user).Error)
	require.Equal(t, common.UserStatusEnabled, user.Status)
	ban, err = model.GetIPBanByTarget("8.8.8.8")
	require.NoError(t, err)
	require.Equal(t, int64(0), ban.ExpiresAt)
}

func TestProbeGuardIgnoresRepeatedSingleModel(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1003)

	for i := 0; i < 5; i++ {
		apiErr := fireProbeGuardModel(t, 1003, "8.8.8.8", "same-model")
		require.Nil(t, apiErr)
	}

	var state model.ProbeIPAbuseState
	err := model.DB.Where("target_ip = ?", "8.8.8.8").First(&state).Error
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestProbeGuardAggregatesDistinctModelsByIPAcrossUsers(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1006)
	seedProbeGuardUser(t, 1007)

	for i := 0; i < 3; i++ {
		apiErr := fireProbeGuardModel(t, 1006, "9.9.9.9", fmt.Sprintf("shared-ip-model-%d", i))
		require.Nil(t, apiErr)
	}
	var apiErr *types.NewAPIError
	for i := 3; i < 5; i++ {
		apiErr = fireProbeGuardModel(t, 1007, "9.9.9.9", fmt.Sprintf("shared-ip-model-%d", i))
	}
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)

	state, err := model.GetProbeIPAbuseState("9.9.9.9")
	require.NoError(t, err)
	require.Equal(t, 1, state.OffenseCount)
	require.Equal(t, 1007, state.LastUserId)

	ban, err := model.GetIPBanByTarget("9.9.9.9")
	require.NoError(t, err)
	require.Greater(t, ban.ExpiresAt, common.GetTimestamp())
}

func TestProbeGuardDoesNotAggregateDistinctModelsAcrossDifferentIPs(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1008)

	ips := []string{"8.8.8.8", "8.8.4.4", "1.1.1.1", "9.9.9.9", "208.67.222.222"}
	for i, ip := range ips {
		apiErr := fireProbeGuardModel(t, 1008, ip, fmt.Sprintf("different-ip-model-%d", i))
		require.Nil(t, apiErr)
	}

	var stateCount int64
	require.NoError(t, model.DB.Model(&model.ProbeIPAbuseState{}).Count(&stateCount).Error)
	require.Equal(t, int64(0), stateCount)

	var ipBanCount int64
	require.NoError(t, model.DB.Model(&model.IPBan{}).Count(&ipBanCount).Error)
	require.Equal(t, int64(0), ipBanCount)
}

func TestProbeGuardSkipsWhitelistUser(t *testing.T) {
	setupProbeGuardTest(t, false)
	updateProbeGuardTestConfig(t, map[string]string{"whitelist_user_ids": "1004, 2000"})
	seedProbeGuardUser(t, 1004)

	apiErr := fireProbeGuardBurst(t, 1004, "8.8.8.8")
	require.Nil(t, apiErr)

	var state model.ProbeIPAbuseState
	err := model.DB.Where("target_ip = ?", "8.8.8.8").First(&state).Error
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}

func TestProbeGuardSkipsAdminUser(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUserWithRole(t, 1005, common.RoleAdminUser)

	apiErr := fireProbeGuardBurst(t, 1005, "8.8.8.8")
	require.Nil(t, apiErr)

	var state model.ProbeIPAbuseState
	err := model.DB.Where("target_ip = ?", "8.8.8.8").First(&state).Error
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}
