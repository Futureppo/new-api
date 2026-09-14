package service

import (
	"context"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// Suno has a separate batch polling response, but uses the same atomic funding
// transition as video tasks. Non-terminal writes also use CAS so a stale poller
// cannot reopen an already settled task.
func updateDeferredSunoTask(ctx context.Context, task *model.Task, result dto.SunoDataResponse) error {
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		return nil
	}
	original := *task
	if result.Status != "" {
		task.Status = model.TaskStatus(result.Status)
	}
	if result.FailReason != "" {
		task.Status, task.FailReason = model.TaskStatusFailure, result.FailReason
	}
	if result.SubmitTime != 0 {
		task.SubmitTime = result.SubmitTime
	}
	if result.StartTime != 0 {
		task.StartTime = result.StartTime
	}
	if result.FinishTime != 0 {
		task.FinishTime = result.FinishTime
	}
	task.Data = result.Data
	var err error
	if task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure {
		task.Progress = "100%"
		_, err = completeDeferredTask(ctx, task, original.Status, nil)
	} else {
		_, err = task.UpdateWithStatus(original.Status)
	}
	if err != nil {
		*task = original
	}
	return err
}

// Negative configured pricing is frozen at submission. Provider-reported costs
// must not replace the administrator's credit with a positive upstream charge.
func deferredTaskQuota(task *model.Task, result *relaycommon.TaskInfo) int {
	bc := task.PrivateData.BillingContext
	if task.Status != model.TaskStatusSuccess {
		return 0
	}
	if bc.PerCallBilling || result == nil || result.TotalTokens <= 0 {
		return bc.EstimatedQuota
	}
	quota := float64(result.TotalTokens) * bc.ModelRatio * bc.GroupRatio
	for _, ratio := range bc.OtherRatios {
		quota *= ratio
	}
	return int(quota)
}

func completeDeferredTask(ctx context.Context, task *model.Task, from model.TaskStatus, result *relaycommon.TaskInfo) (bool, error) {
	reserved := task.Quota
	won, err := model.CompleteDeferredTask(task, from, deferredTaskQuota(task, result))
	if err != nil || !won {
		return won, err
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = reserved
	other["actual_quota"] = task.Quota
	other["deferred_settlement"] = true
	logType, quota := model.LogTypeConsume, task.Quota
	content := ""
	if task.Status == model.TaskStatusFailure {
		logType, quota = model.LogTypeRefund, reserved
		content = sanitizeTaskModelText(task, task.FailReason)
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId: task.UserId, LogType: logType, Quota: quota,
		ChannelId: task.ChannelId, ModelName: taskModelName(task),
		RequestId: task.RequestId, TokenId: task.PrivateData.TokenId,
		Group: task.Group, Content: content, Other: other,
	})
	return true, nil
}
