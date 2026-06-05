package agnes

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	videoEndpoint       = "/v1/videos"
	requestContextKey   = "agnes_video_request"
	defaultNumFrames    = 121
	defaultFrameRate    = 24
	defaultDurationSecs = float64(defaultNumFrames) / float64(defaultFrameRate)
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

type agnesVideoResponse struct {
	ID                 string                 `json:"id,omitempty"`
	TaskID             string                 `json:"task_id,omitempty"`
	Object             string                 `json:"object,omitempty"`
	Model              string                 `json:"model,omitempty"`
	Status             string                 `json:"status,omitempty"`
	Progress           *int                   `json:"progress,omitempty"`
	CreatedAt          int64                  `json:"created_at,omitempty"`
	CompletedAt        int64                  `json:"completed_at,omitempty"`
	Seconds            string                 `json:"seconds,omitempty"`
	Size               string                 `json:"size,omitempty"`
	VideoURL           string                 `json:"video_url,omitempty"`
	RemixedFromVideoID string                 `json:"remixed_from_video_id,omitempty"`
	Video              *agnesVideoData        `json:"video,omitempty"`
	Content            *agnesVideoContent     `json:"content,omitempty"`
	Metadata           map[string]interface{} `json:"metadata,omitempty"`
	Usage              map[string]interface{} `json:"usage,omitempty"`
	Error              *agnesVideoError       `json:"error,omitempty"`
}

type agnesVideoData struct {
	URL string `json:"url,omitempty"`
}

type agnesVideoContent struct {
	VideoURL string `json:"video_url,omitempty"`
	URL      string `json:"url,omitempty"`
}

type agnesVideoError struct {
	Message string `json:"message,omitempty"`
	Code    any    `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info != nil {
		ensureTaskRelayInfo(info)
		if info.Action == constant.TaskActionRemix {
			return service.TaskErrorWrapperLocal(fmt.Errorf("agnes video remix is not supported"), "invalid_request", http.StatusBadRequest)
		}
	}

	req, err := readRequestMap(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_json", http.StatusBadRequest)
	}

	modelName := strings.TrimSpace(getString(req, "model"))
	if modelName == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if strings.TrimSpace(getString(req, "prompt")) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if err := validateFrameOptions(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	if info != nil {
		ensureChannelMeta(info)
		info.Action = constant.TaskActionTextGenerate
		if hasImageInput(req) {
			info.Action = constant.TaskActionGenerate
		}
		if info.OriginModelName == "" {
			info.OriginModelName = modelName
		}
		if info.UpstreamModelName == "" {
			info.UpstreamModelName = modelName
		}
	}
	c.Set(requestContextKey, req)
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", strings.TrimRight(a.baseURL, "/"), videoEndpoint), nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := getStoredRequest(c)
	if err != nil {
		return nil, err
	}

	payload := buildUpstreamRequest(req)
	modelName := strings.TrimSpace(getString(payload, "model"))
	if info != nil && info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		modelName = strings.TrimSpace(info.UpstreamModelName)
	}
	if modelName == "" {
		modelName = ModelVideoV20
	}
	payload["model"] = modelName

	data, err := common.Marshal(payload)
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

	agnesResp, err := decodeAgnesVideoResponse(responseBody)
	if err != nil {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("unmarshal response body failed: %w, body: %s", err, responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	upstreamID := firstNonEmpty(agnesResp.ID, agnesResp.TaskID)
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	publicTaskID := ""
	originModelName := ""
	if info != nil {
		originModelName = info.OriginModelName
		if info.TaskRelayInfo != nil {
			publicTaskID = info.PublicTaskID
		}
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = publicTaskID
	openAIVideo.TaskID = publicTaskID
	openAIVideo.Model = originModelName
	openAIVideo.Status = toOpenAIVideoStatus(agnesResp.Status, dto.VideoStatusQueued)
	openAIVideo.CreatedAt = firstNonZero(agnesResp.CreatedAt, time.Now().Unix())
	openAIVideo.Seconds = agnesResp.Seconds
	openAIVideo.Size = agnesResp.Size
	if agnesResp.Progress != nil {
		openAIVideo.Progress = clampProgress(*agnesResp.Progress)
	}
	if openAIVideo.Model == "" {
		openAIVideo.Model = ModelVideoV20
	}

	c.JSON(http.StatusOK, openAIVideo)
	return upstreamID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := fmt.Sprintf("%s%s/%s", strings.TrimRight(baseURL, "/"), videoEndpoint, strings.TrimSpace(taskID))
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
	agnesResp, err := decodeAgnesVideoResponse(respBody)
	if err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}

	taskResult := &relaycommon.TaskInfo{}
	if agnesResp.Progress != nil {
		taskResult.Progress = progressToString(*agnesResp.Progress)
	}

	switch mapAgnesStatus(agnesResp.Status) {
	case model.TaskStatusQueued:
		taskResult.Status = model.TaskStatusQueued
	case model.TaskStatusInProgress:
		taskResult.Status = model.TaskStatusInProgress
	case model.TaskStatusSuccess:
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = agnesResp.resultURL()
		if taskResult.Progress == "" {
			taskResult.Progress = taskcommon.ProgressComplete
		}
	case model.TaskStatusFailure:
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = agnesResp.errorMessage()
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
		if taskResult.Progress == "" {
			taskResult.Progress = taskcommon.ProgressComplete
		}
	default:
		if strings.TrimSpace(agnesResp.Status) == "" {
			return nil, fmt.Errorf("empty task status")
		}
		taskResult.Status = model.TaskStatusInProgress
	}

	return taskResult, nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req, err := getStoredRequest(c)
	if err != nil {
		return nil
	}
	return map[string]float64{
		"seconds": estimateSeconds(req),
	}
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	agnesResp, err := decodeAgnesVideoResponse(taskData)
	if err != nil {
		return nil
	}
	if seconds := parsePositiveFloat(agnesResp.Seconds); seconds > 0 {
		return map[string]float64{"seconds": seconds}
	}
	return nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var agnesResp agnesVideoResponse
	_ = common.Unmarshal(originTask.Data, &agnesResp)

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Model = firstNonEmpty(originTask.Properties.OriginModelName, agnesResp.Model, ModelVideoV20)
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = firstNonZero(originTask.CreatedAt, agnesResp.CreatedAt)
	openAIVideo.CompletedAt = firstNonZero(originTask.FinishTime, agnesResp.CompletedAt, originTask.UpdatedAt)
	openAIVideo.Seconds = agnesResp.Seconds
	openAIVideo.Size = agnesResp.Size

	resultURL := firstNonEmpty(originTask.GetResultURL(), agnesResp.resultURL())
	if resultURL != "" {
		openAIVideo.SetMetadata("url", resultURL)
	}
	if originTask.Status == model.TaskStatusFailure {
		message := firstNonEmpty(originTask.FailReason, agnesResp.errorMessage())
		if message != "" {
			openAIVideo.Error = &dto.OpenAIVideoError{
				Message: message,
				Code:    agnesResp.errorCode(),
			}
		}
	}

	return common.Marshal(openAIVideo)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func ensureChannelMeta(info *relaycommon.RelayInfo) {
	if info.ChannelMeta == nil {
		info.ChannelMeta = &relaycommon.ChannelMeta{}
	}
}

func ensureTaskRelayInfo(info *relaycommon.RelayInfo) {
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
}

func readRequestMap(c *gin.Context) (map[string]any, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	req := make(map[string]any)
	if err := common.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	return req, nil
}

func getStoredRequest(c *gin.Context) (map[string]any, error) {
	if v, ok := c.Get(requestContextKey); ok {
		if req, ok := v.(map[string]any); ok {
			return req, nil
		}
	}
	return readRequestMap(c)
}

func buildUpstreamRequest(req map[string]any) map[string]any {
	payload := make(map[string]any)
	for _, key := range []string{
		"model",
		"prompt",
		"image",
		"mode",
		"height",
		"width",
		"num_frames",
		"num_inference_steps",
		"seed",
		"frame_rate",
		"negative_prompt",
		"extra_body",
	} {
		if value, ok := req[key]; ok {
			payload[key] = value
		}
	}
	return payload
}

func validateFrameOptions(req map[string]any) error {
	if value, ok := req["num_frames"]; ok {
		frames, ok := numberToInt(value)
		if !ok {
			return fmt.Errorf("num_frames must be an integer")
		}
		if frames <= 0 || frames > 441 || (frames-1)%8 != 0 {
			return fmt.Errorf("num_frames must be <= 441 and satisfy 8n + 1")
		}
	}
	if value, ok := req["frame_rate"]; ok {
		frameRate := numberToFloat(value)
		if frameRate <= 0 {
			return fmt.Errorf("frame_rate must be a number")
		}
		if frameRate < 1 || frameRate > 60 {
			return fmt.Errorf("frame_rate must be between 1 and 60")
		}
	}
	return nil
}

func hasImageInput(req map[string]any) bool {
	if value, ok := req["image"]; ok && hasNonEmptyValue(value) {
		return true
	}
	if extraBody, ok := req["extra_body"].(map[string]any); ok {
		if value, ok := extraBody["image"]; ok && hasNonEmptyValue(value) {
			return true
		}
	}
	return false
}

func hasNonEmptyValue(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	default:
		return value != nil
	}
}

func decodeAgnesVideoResponse(body []byte) (agnesVideoResponse, error) {
	var direct agnesVideoResponse
	if err := common.Unmarshal(body, &direct); err != nil {
		return direct, err
	}
	if direct.hasContent() {
		return direct, nil
	}

	var wrapped struct {
		Body     agnesVideoResponse `json:"body,omitempty"`
		Response struct {
			Body agnesVideoResponse `json:"body,omitempty"`
		} `json:"response,omitempty"`
		FinalResponse struct {
			Body agnesVideoResponse `json:"body,omitempty"`
		} `json:"final_response,omitempty"`
	}
	if err := common.Unmarshal(body, &wrapped); err != nil {
		return direct, err
	}
	for _, candidate := range []agnesVideoResponse{wrapped.Body, wrapped.Response.Body, wrapped.FinalResponse.Body} {
		if candidate.hasContent() {
			return candidate, nil
		}
	}
	return direct, nil
}

func (r agnesVideoResponse) hasContent() bool {
	return firstNonEmpty(r.ID, r.TaskID, r.Status, r.Model, r.VideoURL, r.RemixedFromVideoID, r.resultURL()) != ""
}

func (r agnesVideoResponse) resultURL() string {
	if url := firstNonEmpty(r.VideoURL, r.RemixedFromVideoID); url != "" && looksLikeURL(url) {
		return url
	}
	if r.Video != nil && looksLikeURL(r.Video.URL) {
		return strings.TrimSpace(r.Video.URL)
	}
	if r.Content != nil {
		if url := firstNonEmpty(r.Content.VideoURL, r.Content.URL); looksLikeURL(url) {
			return strings.TrimSpace(url)
		}
	}
	for _, key := range []string{"url", "video_url", "result_url"} {
		if value, ok := r.Metadata[key]; ok {
			if url := strings.TrimSpace(fmt.Sprint(value)); looksLikeURL(url) {
				return url
			}
		}
	}
	if url := strings.TrimSpace(r.RemixedFromVideoID); url != "" {
		return url
	}
	return ""
}

func (r agnesVideoResponse) errorMessage() string {
	if r.Error == nil {
		return ""
	}
	return firstNonEmpty(r.Error.Message, r.Error.Type, r.errorCode())
}

func (r agnesVideoResponse) errorCode() string {
	if r.Error == nil || r.Error.Code == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(r.Error.Code))
}

func mapAgnesStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending":
		return model.TaskStatusQueued
	case "processing", "in_progress", "running":
		return model.TaskStatusInProgress
	case "completed", "succeeded", "success", "done", "complete":
		return model.TaskStatusSuccess
	case "failed", "failure", "error", "cancelled", "canceled", "expired":
		return model.TaskStatusFailure
	default:
		return ""
	}
}

func toOpenAIVideoStatus(status string, fallback string) string {
	switch mapAgnesStatus(status) {
	case model.TaskStatusQueued:
		return dto.VideoStatusQueued
	case model.TaskStatusInProgress:
		return dto.VideoStatusInProgress
	case model.TaskStatusSuccess:
		return dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		return dto.VideoStatusFailed
	default:
		return fallback
	}
}

func estimateSeconds(req map[string]any) float64 {
	if seconds := parsePositiveFloatFromMap(req, "seconds"); seconds > 0 {
		return seconds
	}
	frames := parsePositiveFloatFromMap(req, "num_frames")
	if frames <= 0 {
		frames = defaultNumFrames
	}
	frameRate := parsePositiveFloatFromMap(req, "frame_rate")
	if frameRate <= 0 {
		frameRate = defaultFrameRate
	}
	if frameRate <= 0 {
		return defaultDurationSecs
	}
	return frames / frameRate
}

func parsePositiveFloatFromMap(req map[string]any, key string) float64 {
	if req == nil {
		return 0
	}
	return parsePositiveFloat(req[key])
}

func parsePositiveFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case uint:
		return float64(v)
	case uint64:
		return float64(v)
	case uint32:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return 0
	}
}

func numberToInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		i := int(v)
		return i, v == float64(i)
	case int:
		return v, true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		return i, err == nil
	default:
		return 0, false
	}
}

func numberToFloat(value any) float64 {
	return parsePositiveFloat(value)
}

func getString(req map[string]any, key string) string {
	if req == nil {
		return ""
	}
	value, ok := req[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func progressToString(progress int) string {
	return fmt.Sprintf("%d%%", clampProgress(progress))
}

func clampProgress(progress int) int {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func looksLikeURL(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "http://") ||
		strings.HasPrefix(value, "https://") ||
		strings.HasPrefix(value, "data:")
}
