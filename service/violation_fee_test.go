package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

type violationPollingAdaptor struct{}

func (violationPollingAdaptor) Init(*relaycommon.RelayInfo) {}

func (violationPollingAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"status":"failed","error":{"message":"Content violates usage guidelines"},"progress":100}`)),
	}, nil
}

func (violationPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return relaycommon.FailTaskInfo(ContentViolatesUsageMarker), nil
}

func (violationPollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}

func TestNormalizeViolationFeeErrorForRelayRequiresGrokContext(t *testing.T) {
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: ContentViolatesUsageMarker,
		Type:    "invalid_request_error",
		Code:    "invalid_request",
	}, http.StatusBadRequest)

	xaiInfo := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeXai},
	}
	got := NormalizeViolationFeeErrorForRelay(xaiInfo, apiErr)
	require.Equal(t, types.ErrorCodeViolationFeeGrokCSAM, got.GetErrorCode())
	require.True(t, types.IsSkipRetryError(got))

	nonGrokInfo := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
		OriginModelName: "gpt-4o",
	}
	got = NormalizeViolationFeeErrorForRelay(nonGrokInfo, apiErr)
	require.Equal(t, types.ErrorCode("invalid_request"), got.GetErrorCode())
}

func TestHasViolationMarkerChecksOpenAIErrorMessage(t *testing.T) {
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "request failed: " + ContentViolatesUsageMarker,
		Type:    "invalid_request_error",
		Code:    "invalid_request",
	}, http.StatusBadRequest)

	require.True(t, HasCSAMViolationMarker(apiErr))
}

func TestChargeTaskViolationFeeAfterRefund(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 101, 101, 101
	const initQuotaAfterPreConsume = 7000
	const preConsumed = 3000
	const tokenRemainAfterPreConsume = 2000

	seedUser(t, userID, initQuotaAfterPreConsume)
	seedToken(t, tokenID, userID, "sk-grok-task", tokenRemainAfterPreConsume)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("type", constant.ChannelTypeXai).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Properties.OriginModelName = "grok-imagine-video"
	task.Properties.UpstreamModelName = "grok-imagine-video"
	task.PrivateData.BillingContext.OriginModelName = "grok-imagine-video"
	task.PrivateData.BillingContext.GroupRatio = 1

	RefundTaskQuota(ctx, task, ContentViolatesUsageMarker)
	charged := ChargeTaskViolationFeeIfNeeded(ctx, task, constant.ChannelTypeXai, http.StatusBadRequest, ContentViolatesUsageMarker)
	require.True(t, charged)

	feeQuota := calcViolationFeeQuota(model_setting.GetGrokSettings().ViolationDeductionAmount, 1)
	require.Equal(t, initQuotaAfterPreConsume+preConsumed-feeQuota, getUserQuota(t, userID))
	require.Equal(t, tokenRemainAfterPreConsume+preConsumed-feeQuota, getTokenRemainQuota(t, tokenID))

	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, model.LogTypeConsume, log.Type)
	require.Equal(t, feeQuota, log.Quota)
	require.Equal(t, "grok-imagine-video", log.ModelName)

	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, true, other["violation_fee"])
	require.Equal(t, string(types.ErrorCodeViolationFeeGrokCSAM), other["violation_fee_code"])
	require.Equal(t, float64(feeQuota), other["fee_quota"])
}

func TestChargeTaskViolationFeeSkipsNonGrokContext(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{OriginModelName: "gpt-4o"},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{OriginModelName: "gpt-4o", GroupRatio: 1},
		},
	}

	require.False(t, ShouldChargeTaskViolationFee(constant.ChannelTypeOpenAI, task, ContentViolatesUsageMarker))
}

func TestUpdateVideoSingleTaskChargesViolationFeeOnFailedPoll(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 102, 102, 102
	const initQuotaAfterPreConsume = 9000
	const preConsumed = 1000
	const tokenRemainAfterPreConsume = 4000

	seedUser(t, userID, initQuotaAfterPreConsume)
	seedToken(t, tokenID, userID, "sk-grok-poll", tokenRemainAfterPreConsume)
	seedChannel(t, channelID)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("type", constant.ChannelTypeXai).Error)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.TaskID = "task_grok_poll_violation"
	task.Status = model.TaskStatusInProgress
	task.Progress = "50%"
	task.Properties.OriginModelName = "grok-imagine-video"
	task.Properties.UpstreamModelName = "grok-imagine-video"
	task.PrivateData.BillingContext.OriginModelName = "grok-imagine-video"
	task.PrivateData.BillingContext.GroupRatio = 1
	require.NoError(t, task.Insert())

	var ch model.Channel
	require.NoError(t, model.DB.First(&ch, channelID).Error)
	err := updateVideoSingleTask(ctx, violationPollingAdaptor{}, &ch, task.TaskID, map[string]*model.Task{
		task.TaskID: task,
	})
	require.NoError(t, err)

	feeQuota := calcViolationFeeQuota(model_setting.GetGrokSettings().ViolationDeductionAmount, 1)
	require.Equal(t, initQuotaAfterPreConsume+preConsumed-feeQuota, getUserQuota(t, userID))
	require.Equal(t, tokenRemainAfterPreConsume+preConsumed-feeQuota, getTokenRemainQuota(t, tokenID))

	var reloaded model.Task
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	require.Equal(t, "100%", reloaded.Progress)
	require.Equal(t, ContentViolatesUsageMarker, reloaded.FailReason)

	log := getLastLog(t)
	require.NotNil(t, log)
	require.Equal(t, model.LogTypeConsume, log.Type)
	other, err := common.StrToMap(log.Other)
	require.NoError(t, err)
	require.Equal(t, true, other["violation_fee"])
	require.Equal(t, string(types.ErrorCodeViolationFeeGrokCSAM), other["violation_fee_code"])
}
