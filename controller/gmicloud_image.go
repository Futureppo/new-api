package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	taskgmicloud "github.com/QuantumNous/new-api/relay/channel/task/gmicloud"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func RelayImageGeneration(c *gin.Context) {
	if common.GetContextKeyInt(c, constant.ContextKeyChannelType) == constant.ChannelTypeGMICloud {
		defer service.CleanupFileSources(c)
		RelayTask(c)
		return
	}
	Relay(c, types.RelayFormatOpenAIImage)
}

func RelayImageTask(c *gin.Context) {
	if common.GetContextKeyInt(c, constant.ContextKeyChannelType) != constant.ChannelTypeGMICloud {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("image tasks currently require a GMICLOUD channel"), "unsupported_channel", http.StatusBadRequest))
		return
	}
	RelayTask(c)
}

func RelayImageTaskFetch(c *gin.Context) {
	RelayTaskFetch(c)
}

func isGMICloudImageTask(info *relaycommon.RelayInfo) bool {
	return info.ChannelType == constant.ChannelTypeGMICloud && constant.IsImageTaskAction(info.Action)
}

func respondGMICloudImageError(c *gin.Context, status int, code, message, taskID string) {
	if taskID != "" {
		c.Header("X-New-Api-Task-Id", taskID)
	}
	detail := gin.H{"message": message, "type": "gmicloud_image_error", "code": code}
	if taskID != "" {
		detail["task_id"] = taskID
	}
	c.JSON(status, gin.H{"error": detail})
}

func respondGMICloudImageTask(c *gin.Context, task *model.Task, info *relaycommon.RelayInfo) {
	c.Header("X-New-Api-Task-Id", task.TaskID)
	// Load an independent record inside the worker. Never retain a gin context
	// or share mutable task state after returning the asynchronous response.
	userID, taskID := task.UserId, task.TaskID
	gopool.Go(func() {
		queued, exists, err := model.GetByTaskId(userID, taskID)
		if err != nil || !exists {
			return
		}
		ch, err := model.GetChannelById(queued.ChannelId, true)
		if err == nil {
			err = service.RefreshVideoTask(context.Background(), &taskgmicloud.TaskAdaptor{}, ch, queued)
		}
		if err != nil {
			common.SysError(fmt.Sprintf("submit GMICLOUD image task %s: %v", taskID, err))
		}
	})
	if c.Request.URL.Path == "/v1/images/tasks" {
		c.JSON(http.StatusOK, gin.H{
			"id": task.TaskID, "task_id": task.TaskID,
			"model": info.OriginModelName, "status": "queued",
		})
		return
	}
	timeout := 180 * time.Second
	if common.RelayTimeout > 0 {
		timeout = time.Duration(common.RelayTimeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	task, err := waitGMICloudImageTask(ctx, task, time.Second, model.GetByTaskId)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return // The persisted task remains owned by the background poller.
		}
		if errors.Is(err, context.DeadlineExceeded) {
			respondGMICloudImageError(c, http.StatusGatewayTimeout, "image_task_wait_timeout", "Image generation is still running; query /v1/images/tasks/"+info.PublicTaskID+" instead of submitting again", info.PublicTaskID)
			return
		}
		respondGMICloudImageError(c, http.StatusInternalServerError, "image_task_read_failed", "Unable to read image task; query it by task_id", info.PublicTaskID)
		return
	}
	if task.Status == model.TaskStatusFailure {
		reason := "GMICLOUD image generation failed"
		if model.ShouldShowChannelErrorDetails(task.ChannelId) && task.FailReason != "" {
			reason = task.FailReason
		}
		respondGMICloudImageError(c, http.StatusBadGateway, "image_generation_failed", reason, task.TaskID)
		return
	}
	response, err := buildGMICloudImageResponse(c, task, c.GetString(taskgmicloud.ImageResponseFormatKey))
	if err != nil {
		// An artifact download failure is not a failed generation and must not
		// trigger another upstream POST or refund a completed task.
		respondGMICloudImageError(c, http.StatusBadGateway, "image_result_failed", "Unable to deliver the image result; retrieve its URL using task_id", task.TaskID)
		return
	}
	c.JSON(http.StatusOK, response)
}

type imageTaskLoader func(int, string) (*model.Task, bool, error)

func waitGMICloudImageTask(ctx context.Context, task *model.Task, interval time.Duration, load imageTaskLoader) (*model.Task, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			current, exists, err := load(task.UserId, task.TaskID)
			if err != nil {
				return nil, err
			}
			if !exists {
				return nil, errors.New("image task not found")
			}
			task = current
		}
	}
	return task, nil
}

func buildGMICloudImageResponse(c *gin.Context, task *model.Task, format string) (*dto.ImageResponse, error) {
	images, err := taskgmicloud.ParseImageResults(task.Data)
	if err != nil {
		return nil, err
	}
	response := &dto.ImageResponse{Created: task.SubmitTime, Data: make([]dto.ImageData, 0, len(images))}
	for _, image := range images {
		item := dto.ImageData{Url: image.URL}
		if format == "b64_json" {
			data, mime, err := service.GetBase64Data(c, types.NewURLFileSource(image.URL), "GMICLOUD image result")
			if err != nil {
				return nil, err
			}
			if !strings.HasPrefix(mime, "image/") {
				return nil, errors.New("GMICLOUD result is not an image")
			}
			item.Url, item.B64Json = "", data
		}
		response.Data = append(response.Data, item)
	}
	return response, nil
}
