package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func CheckProbeGuard(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	cfg := setting.GetProbeGuardSetting()
	if !cfg.Enabled || relayInfo == nil || relayInfo.UserId <= 0 || cfg.IsUserWhitelisted(relayInfo.UserId) {
		return nil
	}
	// Ordinary traffic exits without an extra database/cache lookup.
	match := MatchProbeGuardRequest(relayInfo.Request, cfg)
	if match == nil || (c != nil && c.GetInt("role") >= common.RoleAdminUser) {
		return nil
	}
	if cfg.DryRun {
		if model.IsAdmin(relayInfo.UserId) {
			return nil
		}
		recordProbeAccountLog(c, relayInfo, match, 0, true)
		return nil
	}
	requestID := ""
	if c != nil {
		requestID = c.GetString(common.RequestIdKey)
	}
	result, err := model.ApplyProbeUserPenalty(relayInfo.UserId, relayInfo.StartTime, match.RuleID, requestID,
		int64(cfg.FirstUserBanMinutes), int64(cfg.SecondUserBanMinutes), func(count int, period model.UserDisablePeriod) string {
			template := cfg.PermanentBanReason
			if count == 1 {
				template = cfg.FirstBanReason
			} else if count == 2 {
				template = cfg.SecondBanReason
			}
			return setting.RenderProbeGuardReason(template, count, period.Minutes, period.Until)
		})
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeUpdateDataError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	if result.User.Role >= common.RoleAdminUser {
		return nil
	}
	if result.Applied {
		model.InvalidateUserAuthCaches(relayInfo.UserId)
		recordProbeAccountLog(c, relayInfo, match, result.State.OffenseCount, false)
	}
	message := "测活请求已拦截，本次未追加账号处罚"
	if result.User.Status == common.UserStatusDisabled {
		message = UserDisabledMessage(c, result.User.ToBaseUser(), i18n.MsgAuthUserBanned)
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeAccessDenied, http.StatusForbidden,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}

func recordProbeAccountLog(c *gin.Context, info *relaycommon.RelayInfo, match *ProbeGuardMatch, count int, dryRun bool) {
	requestID := ""
	if c != nil {
		requestID = c.GetString(common.RequestIdKey)
	}
	model.RecordLogWithContext(c, info.UserId, model.LogTypeManage, fmt.Sprintf(
		"单轮测活防护: version=1 rule=%s token_id=%d request_id=%s model=%s offense=%d dry_run=%t",
		match.RuleID, info.TokenId, requestID, info.OriginModelName, count, dryRun))
}
