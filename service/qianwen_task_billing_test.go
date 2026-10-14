package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestQianwenVideoSignedDurationQuota(t *testing.T) {
	for _, price := range []float64{0.0001, 0, -0.0001, 0.000000001} {
		task := &model.Task{Platform: "77", Status: model.TaskStatusSuccess, Data: []byte(`{"usage":{"duration":7.5,"input_video_duration":2.5,"output_video_duration":5}}`), PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{ModelPrice: price, GroupRatio: 0.5, EstimatedQuota: 999}}}
		quota, ok := qianwenVideoQuota(task)
		require.True(t, ok)
		require.Equal(t, int(math.Round(price*common.QuotaPerUnit*0.5*7.5)), quota)
		require.Equal(t, quota, deferredTaskQuota(task, nil))
		task.Status = model.TaskStatusFailure
		require.Zero(t, deferredTaskQuota(task, nil))
	}
	for _, body := range []string{`{}`, `{"usage":{"duration":0}}`, `{"usage":{"duration":-1}}`} {
		task := &model.Task{Platform: "77", Data: []byte(body), PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{}}}
		_, ok := qianwenVideoQuota(task)
		require.False(t, ok)
	}
}
