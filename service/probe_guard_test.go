package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
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
	require.NoError(t, i18n.Init())
	oldDB, oldLogDB, oldRedis := model.DB, model.LOG_DB, common.RedisEnabled
	oldCfg := setting.GetProbeGuardSetting()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.IPBan{}, &model.ProbeIPAbuseState{}, &model.ProbeUserAbuseState{}, &model.Log{}))
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	values, err := config.ConfigToMap(&oldCfg)
	require.NoError(t, err)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), map[string]string{
		"enabled": "true", "dry_run": fmt.Sprint(dryRun), "text_rules": setting.DefaultProbeGuardTextRules, "probe_ping_enabled": "true",
		"batch_window_seconds": "30", "batch_request_count": "3",
		"first_user_ban_minutes": "7", "second_user_ban_minutes": "25", "whitelist_user_ids": "",
		"first_ban_reason": "first {count} {minutes} {until}", "second_ban_reason": "second {count} {minutes}", "permanent_ban_reason": "permanent {count}",
	}))
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), values))
		_ = sqlDB.Close()
	})
	gin.SetMode(gin.TestMode)
}

func seedProbeGuardUser(t *testing.T, id int, role int) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("probe-%d", id), AffCode: fmt.Sprint(id), Role: role, Status: common.UserStatusEnabled}).Error)
}

func probeGuardRequest(userId, tokenId int, ip string, start time.Time, text string) (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.RemoteAddr = ip + ":12345"
	c.Set(common.RequestIdKey, "test-request")
	info := &relaycommon.RelayInfo{UserId: userId, TokenId: tokenId, StartTime: start, OriginModelName: "test-model", Request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: text}}}}
	return c, info
}

func TestProbeGuardAccountEscalationAndExpiry(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1001, common.RoleCommonUser)
	// Old IP offenses never carry into account penalties, even for the same IP/user.
	require.NoError(t, model.DB.Create(&model.ProbeIPAbuseState{TargetIP: "8.8.8.8", LastUserId: 1001, OffenseCount: 3}).Error)
	for index, minutes := range []int64{7, 25, 0} {
		for attempt := 0; attempt < 2; attempt++ {
			c, info := probeGuardRequest(1001, attempt+1, "8.8.8.8", time.Now(), "say OK")
			require.Nil(t, CheckProbeGuard(c, info), "each new batch requires three requests, even for the same model")
		}
		c, info := probeGuardRequest(1001, index+1, fmt.Sprintf("8.8.8.%d", index+1), time.Now(), "Reply with exactly OK")
		err := CheckProbeGuard(c, info)
		require.NotNil(t, err)
		require.Equal(t, http.StatusForbidden, err.StatusCode)
		require.Equal(t, string(types.ErrorCodeAccessDenied), fmt.Sprint(err.ToOpenAIError().Code))
		user, dbErr := model.GetUserById(1001, false)
		require.NoError(t, dbErr)
		require.Equal(t, common.UserStatusDisabled, user.Status)
		require.Equal(t, minutes, user.DisableDurationMinutes)
		if minutes == 0 {
			require.Zero(t, user.DisableUntil)
			require.Equal(t, "permanent 3", user.DisableReason)
		} else {
			require.InDelta(t, time.Now().Unix()+minutes*60, user.DisableUntil, 2)
		}
		deadline := user.DisableUntil
		for retry := 0; retry < 3; retry++ {
			c, info = probeGuardRequest(1001, 99, "127.0.0.1", time.Now(), "say OK")
			require.NotNil(t, CheckProbeGuard(c, info))
		}
		state, dbErr := model.GetProbeUserAbuseState(1001)
		require.NoError(t, dbErr)
		require.Equal(t, index+1, state.OffenseCount)
		require.Equal(t, "test-request", state.LastRequestID)
		require.NoError(t, model.DB.First(user, user.Id).Error)
		require.Equal(t, deadline, user.DisableUntil)
		if minutes > 0 {
			require.NoError(t, model.ResolveUserDisableExpiry(user, deadline))
			require.Equal(t, common.UserStatusEnabled, user.Status)
		}
	}
	var bans int64
	require.NoError(t, model.DB.Model(&model.IPBan{}).Count(&bans).Error)
	require.Zero(t, bans)
}

func TestProbeGuardConcurrentBurstCountsOnce(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1002, common.RoleCommonUser)
	start := time.Now()
	errors := make(chan *types.NewAPIError, 24)
	var wait sync.WaitGroup
	for i := 0; i < cap(errors); i++ {
		wait.Add(1)
		go func(token int) {
			defer wait.Done()
			c, info := probeGuardRequest(1002, token, "8.8.8.8", start, "say OK")
			errors <- CheckProbeGuard(c, info)
		}(i)
	}
	wait.Wait()
	close(errors)
	allowed := 0
	for err := range errors {
		if err == nil {
			allowed++
		} else {
			require.Equal(t, http.StatusForbidden, err.StatusCode)
		}
	}
	require.Equal(t, 2, allowed)
	state, err := model.GetProbeUserAbuseState(1002)
	require.NoError(t, err)
	require.Equal(t, 1, state.OffenseCount)
	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&logs).Error)
	require.Equal(t, int64(1), logs)
	// An old request cannot apply a new penalty after an administrator unbans.
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1002).Updates(model.UserEnableUpdates()).Error)
	c, info := probeGuardRequest(1002, 99, "1.1.1.1", start, "say OK")
	require.NotNil(t, CheckProbeGuard(c, info))
	state, err = model.GetProbeUserAbuseState(1002)
	require.NoError(t, err)
	require.Equal(t, 1, state.OffenseCount)
}

func TestProbeGuardDryRunAndExclusions(t *testing.T) {
	setupProbeGuardTest(t, true)
	seedProbeGuardUser(t, 1003, common.RoleCommonUser)
	c, info := probeGuardRequest(1003, 1, "8.8.8.8", time.Now(), "say OK")
	require.Nil(t, CheckProbeGuard(c, info))
	for attempt := 0; attempt < 2; attempt++ {
		c, info = probeGuardRequest(1003, 1, "8.8.8.8", time.Now(), "say OK")
		require.Nil(t, CheckProbeGuard(c, info))
	}
	state, err := model.GetProbeUserAbuseState(1003)
	require.NoError(t, err)
	require.Zero(t, state.OffenseCount)
	require.Zero(t, state.LastOffenseAt)
	var logs int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logs).Error)
	require.Equal(t, int64(1), logs)
	user, err := model.GetUserById(1003, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, user.Status)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), map[string]string{"dry_run": "false", "whitelist_user_ids": "1003"}))
	require.Nil(t, CheckProbeGuard(c, info))
	seedProbeGuardUser(t, 1004, common.RoleAdminUser)
	c, info = probeGuardRequest(1004, 1, "8.8.8.8", time.Now(), "say OK")
	require.Nil(t, CheckProbeGuard(c, info))
	// Another account sharing the IP continues normal work.
	seedProbeGuardUser(t, 1005, common.RoleCommonUser)
	c, info = probeGuardRequest(1005, 1, "8.8.8.8", time.Now(), "Please review this code")
	require.Nil(t, CheckProbeGuard(c, info))
}

func TestProbeGuardPreservesManualBanAndReset(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1006, common.RoleCommonUser)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), map[string]string{"batch_request_count": "1"}))
	c, info := probeGuardRequest(1006, 1, "8.8.8.8", time.Now(), "say OK")
	require.NotNil(t, CheckProbeGuard(c, info))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1006).Updates((model.UserDisablePeriod{}).Updates("manual permanent")).Error)
	c, info = probeGuardRequest(1006, 2, "1.1.1.1", time.Now(), "say OK")
	err := CheckProbeGuard(c, info)
	require.ErrorContains(t, err, "manual permanent")
	require.NoError(t, model.ResetProbeUserAbuseState(1006))
	state, dbErr := model.GetProbeUserAbuseState(1006)
	require.NoError(t, dbErr)
	require.Zero(t, state.OffenseCount)
	user, dbErr := model.GetUserById(1006, false)
	require.NoError(t, dbErr)
	require.Equal(t, common.UserStatusDisabled, user.Status)
	require.Equal(t, "manual permanent", user.DisableReason)
	require.Zero(t, user.DisableUntil)
}

func TestProbeGuardPenaltyRollsBackWithCounter(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1007, common.RoleCommonUser)
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("probe_guard_setting"), map[string]string{"batch_request_count": "1"}))
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("fail_probe_state", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "ProbeUserAbuseState" {
			tx.AddError(fmt.Errorf("counter write failed"))
		}
	}))
	c, info := probeGuardRequest(1007, 1, "8.8.8.8", time.Now(), "say OK")
	err := CheckProbeGuard(c, info)
	require.NotNil(t, err)
	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
	user, dbErr := model.GetUserById(1007, false)
	require.NoError(t, dbErr)
	require.Equal(t, common.UserStatusEnabled, user.Status)
}

func TestProbeGuardOnlyCountsMatchedRequestsForTheSameAccount(t *testing.T) {
	setupProbeGuardTest(t, false)
	seedProbeGuardUser(t, 1008, common.RoleCommonUser)
	seedProbeGuardUser(t, 1009, common.RoleCommonUser)
	for _, userId := range []int{1008, 1009} {
		for attempt := 0; attempt < 2; attempt++ {
			c, info := probeGuardRequest(userId, attempt+1, "8.8.8.8", time.Now(), "say OK")
			require.Nil(t, CheckProbeGuard(c, info))
		}
	}
	for i := 0; i < 5; i++ {
		c, info := probeGuardRequest(1008, 1, "8.8.8.8", time.Now(), "Please explain this code")
		require.Nil(t, CheckProbeGuard(c, info))
		c, info = probeGuardRequest(1008, 1, "8.8.8.8", time.Now(), "say OK")
		info.Request.(*dto.GeneralOpenAIRequest).Messages = append(info.Request.(*dto.GeneralOpenAIRequest).Messages, dto.Message{Role: "assistant", Content: "history"})
		require.Nil(t, CheckProbeGuard(c, info))
	}
	c, info := probeGuardRequest(1008, 3, "1.1.1.1", time.Now(), "Reply with exactly OK")
	require.NotNil(t, CheckProbeGuard(c, info), "different matching rules share the same request counter")
	other, err := model.GetUserById(1009, false)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, other.Status)
}
