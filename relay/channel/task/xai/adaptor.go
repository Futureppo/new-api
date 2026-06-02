package xai

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const (
	videoGenerationsEndpoint = "/v1/videos/generations"
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

type submitResponse struct {
	RequestID string `json:"request_id"`
}

type videoResultResponse struct {
	Status   string       `json:"status,omitempty"`
	Video    *videoData   `json:"video,omitempty"`
	Model    string       `json:"model,omitempty"`
	Usage    *usageData   `json:"usage,omitempty"`
	Progress int          `json:"progress,omitempty"`
	Error    *resultError `json:"error,omitempty"`
}

type videoData struct {
	URL               string `json:"url,omitempty"`
	Duration          any    `json:"duration,omitempty"`
	RespectModeration *bool  `json:"respect_moderation,omitempty"`
}

type usageData struct {
	CostInUSDTicks int64 `json:"cost_in_usd_ticks,omitempty"`
}

type resultError struct {
	Message string `json:"message,omitempty"`
	Code    string `json:"code,omitempty"`
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	body, err := readRequestBody(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "read_request_failed", http.StatusBadRequest)
	}

	var req map[string]any
	if err := common.Unmarshal(body, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}

	if strings.TrimSpace(getString(req, "model")) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if strings.TrimSpace(getString(req, "prompt")) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	info.Action = constant.TaskActionTextGenerate
	if hasMediaInput(req) {
		info.Action = constant.TaskActionGenerate
	}
	c.Set("xai_video_request", req)
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", strings.TrimRight(a.baseURL, "/"), videoGenerationsEndpoint), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	body, err := readRequestBody(c)
	if err != nil {
		return nil, errors.Wrap(err, "read request body failed")
	}

	var req map[string]any
	if err := common.Unmarshal(body, &req); err != nil {
		return nil, errors.Wrap(err, "unmarshal request body failed")
	}

	req["model"] = info.UpstreamModelName
	normalizeImageInput(req)

	data, err := common.Marshal(req)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var xResp submitResponse
	if err := common.Unmarshal(responseBody, &xResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(xResp.RequestID) == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("request_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	if isXAIOfficialSubmitPath(c.Request.URL.Path) {
		c.JSON(http.StatusOK, submitResponse{RequestID: info.PublicTaskID})
	} else {
		ov := dto.NewOpenAIVideo()
		ov.ID = info.PublicTaskID
		ov.TaskID = info.PublicTaskID
		ov.Model = info.OriginModelName
		c.JSON(http.StatusOK, ov)
	}
	return xResp.RequestID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s/v1/videos/%s", strings.TrimRight(baseURL, "/"), taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res videoResultResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{}
	taskResult.Progress = xaiProgressToString(res.Progress)
	if res.Video != nil && strings.TrimSpace(res.Video.URL) != "" {
		taskResult.Url = res.Video.URL
	}

	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "done", "completed", "complete", "succeeded", "success":
		taskResult.Status = model.TaskStatusSuccess
		if taskResult.Progress == "" {
			taskResult.Progress = taskcommon.ProgressComplete
		}
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
		taskResult.Status = model.TaskStatusFailure
		if res.Error != nil && res.Error.Message != "" {
			taskResult.Reason = res.Error.Message
		} else if res.Error != nil && res.Error.Code != "" {
			taskResult.Reason = res.Error.Code
		} else {
			taskResult.Reason = "task failed"
		}
		if taskResult.Progress == "" {
			taskResult.Progress = taskcommon.ProgressComplete
		}
	case "queued", "pending":
		taskResult.Status = model.TaskStatusQueued
		if taskResult.Progress == "" {
			taskResult.Progress = taskcommon.ProgressQueued
		}
	case "processing", "in_progress", "running":
		taskResult.Status = model.TaskStatusInProgress
		if taskResult.Progress == "" {
			taskResult.Progress = taskcommon.ProgressInProgress
		}
	default:
		if res.Status == "" {
			return nil, fmt.Errorf("empty task status")
		}
		taskResult.Status = model.TaskStatusInProgress
	}

	return taskResult, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req := getStoredRequest(c)
	seconds := getInt(req, "duration")
	if seconds <= 0 {
		seconds = 1
	}

	resolutionRatio := 1.0
	resolution := strings.ToLower(strings.TrimSpace(getString(req, "resolution")))
	if strings.Contains(resolution, "720") {
		resolutionRatio = 1.4
	}

	return map[string]float64{
		"seconds":    float64(seconds),
		"resolution": resolutionRatio,
	}
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, _ *relaycommon.TaskInfo) int {
	var res videoResultResponse
	if err := common.Unmarshal(task.Data, &res); err != nil || res.Usage == nil || res.Usage.CostInUSDTicks <= 0 {
		return 0
	}

	groupRatio := 1.0
	if task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.GroupRatio > 0 {
		groupRatio = task.PrivateData.BillingContext.GroupRatio
	}

	costUSD := float64(res.Usage.CostInUSDTicks) / 10_000_000_000
	return int(math.Round(costUSD * common.QuotaPerUnit * groupRatio))
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res videoResultResponse
	_ = common.Unmarshal(task.Data, &res)

	if res.Model == "" {
		res.Model = task.Properties.OriginModelName
	}
	if res.Status == "" {
		res.Status = taskStatusToXAIStatus(task.Status)
	}
	if res.Progress == 0 {
		res.Progress = progressStringToInt(task.Progress)
	}
	if task.Status == model.TaskStatusSuccess && res.Progress == 0 {
		res.Progress = 100
	}
	if task.Status == model.TaskStatusFailure && res.Error == nil {
		res.Error = &resultError{Message: task.FailReason}
	}
	if res.Video == nil && strings.TrimSpace(task.GetResultURL()) != "" && task.Status == model.TaskStatusSuccess {
		res.Video = &videoData{URL: task.GetResultURL()}
	}

	return common.Marshal(res)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func readRequestBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func getStoredRequest(c *gin.Context) map[string]any {
	if v, ok := c.Get("xai_video_request"); ok {
		if req, ok := v.(map[string]any); ok {
			return req
		}
	}
	return nil
}

func isXAIOfficialSubmitPath(path string) bool {
	return strings.HasPrefix(path, videoGenerationsEndpoint)
}

func hasMediaInput(req map[string]any) bool {
	for _, key := range []string{"image", "images", "input_reference", "video"} {
		if _, ok := req[key]; ok {
			return true
		}
	}
	return false
}

func normalizeImageInput(req map[string]any) {
	if image, ok := req["image"]; ok {
		if imageURL, ok := image.(string); ok && strings.TrimSpace(imageURL) != "" {
			req["image"] = map[string]any{"url": imageURL}
		}
		return
	}

	if imageURL := getString(req, "image_url"); imageURL != "" {
		req["image"] = map[string]any{"url": imageURL}
		delete(req, "image_url")
		return
	}

	if inputReference := getString(req, "input_reference"); inputReference != "" {
		req["image"] = map[string]any{"url": inputReference}
		delete(req, "input_reference")
		return
	}

	if images, ok := req["images"].([]any); ok && len(images) > 0 {
		if imageURL, ok := images[0].(string); ok && strings.TrimSpace(imageURL) != "" {
			req["image"] = map[string]any{"url": imageURL}
			delete(req, "images")
		}
	}
}

func getString(req map[string]any, key string) string {
	if req == nil {
		return ""
	}
	value, ok := req[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func getInt(req map[string]any, key string) int {
	if req == nil {
		return 0
	}
	value, ok := req[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}

func xaiProgressToString(progress int) string {
	if progress <= 0 {
		return ""
	}
	if progress > 100 {
		progress = 100
	}
	return fmt.Sprintf("%d%%", progress)
}

func progressStringToInt(progress string) int {
	progress = strings.TrimSpace(strings.TrimSuffix(progress, "%"))
	n, _ := strconv.Atoi(progress)
	return n
}

func taskStatusToXAIStatus(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "done"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "pending"
	case model.TaskStatusInProgress:
		return "processing"
	default:
		return "pending"
	}
}
