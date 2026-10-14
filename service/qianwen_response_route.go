package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const QianwenResponsePlatform constant.TaskPlatform = "qianwen-response"
const QianwenResponseRetention = 7 * 24 * time.Hour

// Reuse the existing durable resource/task table. No new table or migration is
// needed; completed response bindings are never picked up by the task poller.
// Only routing metadata is retained, never conversation content.
func RecordQianwenResponse(c *gin.Context, info *relaycommon.RelayInfo, responseID string) error {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeQianwenAI ||
		info.UserId == 0 || !c.GetBool("qianwen_store_response") || responseID == "" {
		return nil
	}
	if c.GetString("qianwen_recorded_response") == responseID {
		return nil
	}
	task := model.InitTask(QianwenResponsePlatform, info)
	task.TaskID, task.Action, task.Status, task.Progress = responseID, "response", model.TaskStatusSuccess, "100%"
	task.FinishTime = time.Now().Unix()
	task.PrivateData.Key = info.ApiKey
	task.PrivateData.TokenId = info.TokenId
	task.PrivateData.QianwenBaseURL = info.ChannelBaseUrl
	if err := task.Insert(); err != nil {
		return fmt.Errorf("save QianwenAI response routing: %w", err)
	}
	c.Set("qianwen_recorded_response", responseID)
	return nil
}

func GetQianwenResponseRoute(responseID string, userID, tokenID int) (*model.Task, error) {
	var task model.Task
	err := model.DB.Where("platform = ? AND task_id = ? AND user_id = ? AND finish_time >= ?", QianwenResponsePlatform, responseID, userID, time.Now().Add(-QianwenResponseRetention).Unix()).Order("id DESC").First(&task).Error
	if err != nil {
		return nil, err
	}
	if task.PrivateData.TokenId != tokenID {
		return nil, fmt.Errorf("response not found")
	}
	return &task, nil
}
