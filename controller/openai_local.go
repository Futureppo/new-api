package controller

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type openAILocalEditableFileResult struct {
	PrimaryURL string `json:"primary_url,omitempty"`
	ZipURL     string `json:"zip_url,omitempty"`
}

type openAILocalEditableFileTaskItem struct {
	ID        string                        `json:"id"`
	TaskID    string                        `json:"taskId"`
	Status    string                        `json:"status"`
	Kind      string                        `json:"kind"`
	CreatedAt string                        `json:"created_at,omitempty"`
	UpdatedAt string                        `json:"updated_at,omitempty"`
	Result    openAILocalEditableFileResult `json:"result,omitempty"`
	Error     string                        `json:"error,omitempty"`
}

type openAILocalEditableFileTasksResponse struct {
	Items      []openAILocalEditableFileTaskItem `json:"items"`
	MissingIDs []string                          `json:"missing_ids,omitempty"`
}

func OpenAILocalEditableFileTasks(c *gin.Context) {
	userId := c.GetInt("id")
	platform := constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAILocal))
	actions := []string{constant.TaskActionPPT, constant.TaskActionPSD}
	ids := parseOpenAILocalTaskIDs(c.Query("ids"))

	var (
		tasks []*model.Task
		err   error
	)
	if len(ids) > 0 {
		tasks, err = model.GetUserTasksByIDsPlatformActions(userId, ids, platform, actions)
	} else {
		tasks, err = model.GetUserTasksByPlatformActions(userId, platform, actions)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	found := make(map[string]bool, len(tasks))
	items := make([]openAILocalEditableFileTaskItem, 0, len(tasks))
	for _, task := range tasks {
		found[task.TaskID] = true
		items = append(items, openAILocalTaskToEditableFileItem(task))
	}

	missingIDs := make([]string, 0)
	for _, id := range ids {
		if !found[id] {
			missingIDs = append(missingIDs, id)
		}
	}

	c.JSON(http.StatusOK, openAILocalEditableFileTasksResponse{
		Items:      items,
		MissingIDs: missingIDs,
	})
}

func parseOpenAILocalTaskIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids
}

func openAILocalTaskToEditableFileItem(task *model.Task) openAILocalEditableFileTaskItem {
	result := openAILocalFileResultFromTask(task)
	result.PrimaryURL = normalizeOpenAILocalFileURL(task, result.PrimaryURL)
	result.ZipURL = normalizeOpenAILocalFileURL(task, result.ZipURL)

	return openAILocalEditableFileTaskItem{
		ID:        task.TaskID,
		TaskID:    task.TaskID,
		Status:    openAILocalTaskStatus(task.Status),
		Kind:      task.Action,
		CreatedAt: openAILocalTaskTime(task.SubmitTime),
		UpdatedAt: openAILocalTaskTime(openAILocalUpdatedAt(task)),
		Result:    result,
		Error:     strings.TrimSpace(task.FailReason),
	}
}

func openAILocalFileResultFromTask(task *model.Task) openAILocalEditableFileResult {
	result := openAILocalEditableFileResult{
		PrimaryURL: task.GetResultURL(),
	}
	if len(task.Data) == 0 {
		return result
	}

	var payload struct {
		Result *openAILocalEditableFileResult `json:"result"`
		Items  []struct {
			Result openAILocalEditableFileResult `json:"result"`
		} `json:"items"`
	}
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		return result
	}
	if payload.Result != nil {
		if payload.Result.PrimaryURL != "" {
			result.PrimaryURL = payload.Result.PrimaryURL
		}
		if payload.Result.ZipURL != "" {
			result.ZipURL = payload.Result.ZipURL
		}
	}
	if len(payload.Items) > 0 {
		itemResult := payload.Items[0].Result
		if itemResult.PrimaryURL != "" {
			result.PrimaryURL = itemResult.PrimaryURL
		}
		if itemResult.ZipURL != "" {
			result.ZipURL = itemResult.ZipURL
		}
	}
	return result
}

func normalizeOpenAILocalFileURL(task *model.Task, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}

	baseURL := constant.ChannelBaseURLs[constant.ChannelTypeOpenAILocal]
	if task != nil && task.ChannelId > 0 {
		if ch, err := model.CacheGetChannel(task.ChannelId); err == nil && ch != nil && ch.GetBaseURL() != "" {
			baseURL = ch.GetBaseURL()
		}
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(raw, "/")
}

func openAILocalTaskStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusQueued, model.TaskStatusSubmitted, model.TaskStatusNotStart:
		return "queued"
	case model.TaskStatusInProgress:
		return "running"
	case model.TaskStatusSuccess:
		return "success"
	case model.TaskStatusFailure:
		return "error"
	default:
		return "running"
	}
}

func openAILocalUpdatedAt(task *model.Task) int64 {
	if task.FinishTime > 0 {
		return task.FinishTime
	}
	if task.UpdatedAt > 0 {
		return task.UpdatedAt
	}
	return task.SubmitTime
}

func openAILocalTaskTime(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).UTC().Format(time.RFC3339)
}
