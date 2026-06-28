package openailocal

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	baseopenailocal "github.com/QuantumNous/new-api/relay/channel/openailocal"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type editableFileTaskRequest struct {
	Prompt       string   `json:"prompt"`
	Base64Images []string `json:"base64_images"`
	ClientTaskID string   `json:"client_task_id,omitempty"`
}

type editableFileTaskResult struct {
	PrimaryURL string `json:"primary_url,omitempty"`
	ZipURL     string `json:"zip_url,omitempty"`
}

type editableFileTaskItem struct {
	ID        string                 `json:"id,omitempty"`
	TaskID    string                 `json:"taskId,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Kind      string                 `json:"kind,omitempty"`
	Result    editableFileTaskResult `json:"result,omitempty"`
	Error     any                    `json:"error,omitempty"`
	CreatedAt string                 `json:"created_at,omitempty"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
}

type editableFileTasksResponse struct {
	Items      []editableFileTaskItem `json:"items"`
	MissingIDs []string               `json:"missing_ids,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	apiKey  string
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.apiKey = info.ApiKey
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var req editableFileTaskRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	switch {
	case strings.HasPrefix(c.Request.URL.Path, "/v1/ppt/generations"):
		info.Action = constant.TaskActionPPT
		if info.OriginModelName == "" {
			info.OriginModelName = baseopenailocal.ModelPPT
		}
	case strings.HasPrefix(c.Request.URL.Path, "/v1/psd/generations"):
		info.Action = constant.TaskActionPSD
		if info.OriginModelName == "" {
			info.OriginModelName = baseopenailocal.ModelPSD
		}
		if len(req.Base64Images) == 0 {
			return service.TaskErrorWrapperLocal(fmt.Errorf("base64_images is required"), "invalid_request", http.StatusBadRequest)
		}
	default:
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported OpenAI-local task path"), "invalid_request", http.StatusBadRequest)
	}

	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: req.Prompt,
		Images: req.Base64Images,
		Metadata: map[string]interface{}{
			"client_task_id": req.ClientTaskID,
		},
	})
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	switch info.Action {
	case constant.TaskActionPPT:
		return strings.TrimRight(a.baseURL, "/") + "/v1/ppt/generations", nil
	case constant.TaskActionPSD:
		return strings.TrimRight(a.baseURL, "/") + "/v1/psd/generations", nil
	default:
		return "", fmt.Errorf("unsupported OpenAI-local task action: %s", info.Action)
	}
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var payload map[string]any
	if err := common.Unmarshal(responseBody, &payload); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_body_failed", http.StatusInternalServerError)
	}

	upstreamID := common.Interface2String(payload["id"])
	if upstreamID == "" {
		upstreamID = common.Interface2String(payload["taskId"])
	}
	if upstreamID == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("task id is empty"), "invalid_response", http.StatusInternalServerError)
	}

	payload["id"] = info.PublicTaskID
	payload["taskId"] = info.PublicTaskID
	if payload["kind"] == nil || common.Interface2String(payload["kind"]) == "" {
		payload["kind"] = info.Action
	}
	rewriteBody, err := common.Marshal(payload)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "marshal_response_body_failed", http.StatusInternalServerError)
	}

	c.Data(http.StatusOK, "application/json", rewriteBody)
	return upstreamID, rewriteBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID := common.Interface2String(body["task_id"])
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/editable-file-tasks?ids=%s", strings.TrimRight(baseUrl, "/"), url.QueryEscape(taskID))
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return resp, err
	}
	responseBody, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp, nil
	}
	normalizedBody := normalizeEditableFileTaskURLs(baseUrl, responseBody)
	resp.Body = io.NopCloser(bytes.NewReader(normalizedBody))
	resp.ContentLength = int64(len(normalizedBody))
	return resp, nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var taskResp editableFileTasksResponse
	if err := common.Unmarshal(respBody, &taskResp); err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}
	if len(taskResp.Items) == 0 {
		return relaycommon.FailTaskInfo("task not found"), nil
	}

	item := taskResp.Items[0]
	taskInfo := &relaycommon.TaskInfo{
		TaskID: item.ID,
		Reason: common.Interface2String(item.Error),
	}
	if taskInfo.TaskID == "" {
		taskInfo.TaskID = item.TaskID
	}

	switch strings.ToLower(item.Status) {
	case "queued", "pending":
		taskInfo.Status = model.TaskStatusQueued
		taskInfo.Progress = taskcommon.ProgressQueued
	case "running", "processing", "in_progress":
		taskInfo.Status = model.TaskStatusInProgress
		taskInfo.Progress = taskcommon.ProgressInProgress
	case "success", "completed", "succeeded", "done":
		taskInfo.Status = model.TaskStatusSuccess
		taskInfo.Progress = taskcommon.ProgressComplete
		taskInfo.Url = item.Result.PrimaryURL
		if taskInfo.Url == "" {
			taskInfo.Url = item.Result.ZipURL
		}
	case "error", "failed", "failure", "cancelled", "canceled":
		taskInfo.Status = model.TaskStatusFailure
		taskInfo.Progress = taskcommon.ProgressComplete
		if taskInfo.Reason == "" {
			taskInfo.Reason = "task failed"
		}
	default:
		taskInfo.Status = model.TaskStatusInProgress
		taskInfo.Progress = taskcommon.ProgressInProgress
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return baseopenailocal.ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return baseopenailocal.ChannelName
}

func normalizeEditableFileTaskURLs(baseURL string, body []byte) []byte {
	var taskResp editableFileTasksResponse
	if err := common.Unmarshal(body, &taskResp); err != nil {
		return body
	}
	for i := range taskResp.Items {
		taskResp.Items[i].Result.PrimaryURL = normalizeEditableFileTaskURL(baseURL, taskResp.Items[i].Result.PrimaryURL)
		taskResp.Items[i].Result.ZipURL = normalizeEditableFileTaskURL(baseURL, taskResp.Items[i].Result.ZipURL)
	}
	normalized, err := common.Marshal(taskResp)
	if err != nil {
		return body
	}
	return normalized
}

func normalizeEditableFileTaskURL(baseURL string, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(raw, "/")
}
