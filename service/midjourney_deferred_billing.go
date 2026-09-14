package service

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func AttachDeferredMidjourneyBilling(task *model.Midjourney, info *relaycommon.RelayInfo, estimatedQuota int) {
	task.Quota = info.FinalPreConsumedQuota
	task.DeferredBilling = &model.TaskPrivateData{
		BillingSource: info.BillingSource, SubscriptionId: info.SubscriptionId, TokenId: info.TokenId,
		BillingContext: &model.TaskBillingContext{DeferredSettlement: true, EstimatedQuota: estimatedQuota, PerCallBilling: true,
			ModelPrice: info.PriceData.ModelPrice, ModelRatio: info.PriceData.ModelRatio,
			GroupRatio: info.PriceData.GroupRatioInfo.GroupRatio, OriginModelName: info.OriginModelName},
	}
}

func CompleteDeferredMidjourney(task *model.Midjourney, fromStatus string) (bool, error) {
	reserved := task.Quota
	won, err := model.CompleteDeferredMidjourney(task, fromStatus)
	if err != nil || !won {
		return won, err
	}
	kind, quota := model.LogTypeConsume, task.Quota
	bc := task.DeferredBilling.BillingContext
	if task.Status == model.TaskStatusFailure {
		kind, quota = model.LogTypeRefund, reserved
	}
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId: task.UserId, ChannelId: task.ChannelId, TokenId: task.DeferredBilling.TokenId,
		ModelName: task.DeferredBilling.BillingContext.OriginModelName, LogType: kind, Quota: quota,
		Other: map[string]any{"task_id": task.MjId, "pre_consumed_quota": reserved, "actual_quota": task.Quota, "deferred_settlement": true,
			"model_price": bc.ModelPrice, "model_ratio": bc.ModelRatio, "group_ratio": bc.GroupRatio,
			"use_price": bc.ModelRatio == 0, "billing_source": task.DeferredBilling.BillingSource},
	})
	return true, nil
}
