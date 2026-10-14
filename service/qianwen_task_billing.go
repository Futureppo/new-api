package service

import (
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

// Qianwen video prices are per second, including zero and administrator-defined
// negative prices. Keep fractional usage.duration and never add its input/output
// breakdown a second time.
func qianwenVideoQuota(task *model.Task) (int, bool) {
	if task.Platform != constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeQianwenAI)) || task.PrivateData.BillingContext == nil {
		return 0, false
	}
	var result struct {
		Usage struct {
			Duration *float64 `json:"duration"`
		} `json:"usage"`
	}
	if common.Unmarshal(task.Data, &result) != nil || result.Usage.Duration == nil || *result.Usage.Duration <= 0 {
		return 0, false
	}
	bc := task.PrivateData.BillingContext
	return int(math.Round(bc.ModelPrice * common.QuotaPerUnit * bc.GroupRatio * *result.Usage.Duration)), true
}
