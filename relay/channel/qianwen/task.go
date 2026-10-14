package qianwen

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Video generation uses the existing durable task queue, polling and refunds.
type TaskAdaptor struct{ taskcommon.BaseBilling }

func (a *TaskAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (a *TaskAdaptor) GetChannelName() string        { return "QianwenAI" }
func (a *TaskAdaptor) GetModelList() []string {
	return []string{"wan3.0-video", "wan3.0-video-prime", "wan2.7-t2v", "wan2.7-i2v"}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if !strings.Contains(c.Request.URL.Path, "/video") || strings.HasSuffix(c.Request.URL.Path, "/remix") {
		return service.TaskErrorWrapperLocal(fmt.Errorf("QianwenAI task endpoint supports video generation; use /v1/audio/speech for audio"), "unsupported_endpoint", http.StatusBadRequest)
	}
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if req.Model == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model is required"), "invalid_request", http.StatusBadRequest)
	}
	c.Set("task_request", req)
	info.Action = constant.TaskActionTextGenerate
	if req.HasImage() {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	_, err := videoBody(c, info)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

func videoBody(c *gin.Context, info *relaycommon.RelayInfo) (map[string]any, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	input := map[string]any{}
	params := map[string]any{}
	// Native input/parameters can be supplied either at the top level or in
	// the standard video facade's metadata, without losing provider extensions.
	var raw struct {
		Input      map[string]any `json:"input"`
		Parameters map[string]any `json:"parameters"`
		Duration   *int           `json:"duration"`
	}
	if err := common.UnmarshalBodyReusable(c, &raw); err != nil {
		return nil, err
	}
	if req.Metadata != nil {
		if value, ok := req.Metadata["input"].(map[string]any); ok {
			for k, v := range value {
				input[k] = v
			}
		}
		if value, ok := req.Metadata["parameters"].(map[string]any); ok {
			for k, v := range value {
				params[k] = v
			}
		}
		if value, ok := req.Metadata["model"]; ok && value != info.UpstreamModelName {
			return nil, fmt.Errorf("metadata cannot override model")
		}
	}
	for k, v := range raw.Input {
		input[k] = v
	}
	for k, v := range raw.Parameters {
		params[k] = v
	}
	if req.Prompt != "" {
		input["prompt"] = req.Prompt
	}
	images := req.Images
	if len(images) == 0 {
		for _, value := range []string{req.InputReference, req.ImageURL, req.Image} {
			if value != "" {
				images = []string{value}
				break
			}
		}
	}
	if len(images) > 0 {
		if strings.HasPrefix(info.UpstreamModelName, "wan2.7-") && len(images) > 2 {
			return nil, fmt.Errorf("Wan2.7 accepts at most first and last frame images; use native input.media for other media")
		}
		if strings.HasPrefix(info.UpstreamModelName, "wan3.") || strings.HasPrefix(info.UpstreamModelName, "wan2.7-") {
			if _, exists := input["media"]; !exists {
				media := []any{}
				for i, image := range images {
					mediaType := "reference_image"
					if !strings.HasPrefix(info.UpstreamModelName, "wan3.") {
						mediaType = "first_frame"
						if i > 0 {
							mediaType = "last_frame"
						}
					}
					media = append(media, map[string]any{"type": mediaType, "url": image})
				}
				input["media"] = media
			}
		} else if _, exists := input["img_url"]; !exists {
			input["img_url"] = images[0]
		}
	}
	if len(input) == 0 {
		return nil, fmt.Errorf("prompt or native input media is required")
	}
	if raw.Duration != nil {
		params["duration"] = *raw.Duration
	} else if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil {
			return nil, fmt.Errorf("invalid seconds")
		}
		params["duration"] = seconds
	}
	if req.Size != "" {
		if strings.ContainsAny(req.Size, "x*") {
			if strings.HasPrefix(info.UpstreamModelName, "wan3.") || strings.HasPrefix(info.UpstreamModelName, "wan2.7-") {
				resolution, ratio, err := videoDimensions(req.Size)
				if err != nil {
					return nil, err
				}
				params["resolution"] = resolution
				if info.UpstreamModelName != "wan2.7-i2v" {
					params["ratio"] = ratio
				}
			} else {
				params["size"] = strings.ReplaceAll(req.Size, "x", "*")
			}
		} else {
			params["resolution"] = strings.ToUpper(req.Size)
		}
	}
	body := map[string]any{"model": info.UpstreamModelName, "input": input, "parameters": params}
	if len(info.ParamOverride) > 0 {
		data, err := common.Marshal(body)
		if err != nil {
			return nil, err
		}
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
		if err != nil {
			return nil, err
		}
		if err = common.Unmarshal(data, &body); err != nil {
			return nil, err
		}
		if body["model"] != info.UpstreamModelName {
			return nil, fmt.Errorf("use model mapping to select the video model; parameter overrides cannot change model")
		}
	}
	return body, nil
}

// The current Wan APIs accept resolution/ratio, not OpenAI pixel dimensions.
func videoDimensions(size string) (string, string, error) {
	parts := strings.FieldsFunc(size, func(r rune) bool { return r == 'x' || r == '*' })
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid video size")
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", "", fmt.Errorf("invalid video size")
	}
	short := min(w, h)
	resolution := ""
	switch short {
	case 480, 720, 1080:
		resolution = strconv.Itoa(short) + "P"
	}
	if resolution == "" {
		return "", "", fmt.Errorf("use video size 480P, 720P or 1080P, or pixel dimensions with that short edge")
	}
	for _, ratio := range []struct{ w, h int }{{21, 9}, {16, 9}, {4, 3}, {1, 1}, {3, 4}, {9, 16}} {
		if math.Abs(float64(w)/float64(h)-float64(ratio.w)/float64(ratio.h)) < 0.005 {
			return resolution, fmt.Sprintf("%d:%d", ratio.w, ratio.h), nil
		}
	}
	return "", "", fmt.Errorf("video size has unsupported aspect ratio; use native parameters.resolution and parameters.ratio")
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if err := ValidateBaseURL(info.ChannelBaseUrl); err != nil {
		return "", err
	}
	return NormalizeBaseURL(info.ChannelBaseUrl) + videoPath, nil
}
func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable")
	return nil
}
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if !info.PriceData.UsePrice {
		return nil, fmt.Errorf("configure QianwenAI video unit price per second")
	}
	body, err := videoBody(c, info)
	if err != nil {
		return nil, err
	}
	data, err := common.Marshal(body)
	return bytes.NewReader(data), err
}
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, body)
}
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	body, err := videoBody(c, info)
	if err != nil {
		return nil
	}
	seconds := 5.0
	if params, ok := body["parameters"].(map[string]any); ok {
		switch value := params["duration"].(type) {
		case int:
			seconds = float64(value)
		case float64:
			seconds = value
		}
	}
	if seconds <= 0 {
		seconds = 30
	} // reserve the documented smart-duration maximum
	return map[string]float64{"seconds": seconds}
}

type videoResult struct {
	nativeError
	Output struct {
		TaskID  string `json:"task_id"`
		Status  string `json:"task_status"`
		URL     string `json:"video_url"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"output"`
	Usage struct {
		Duration float64 `json:"duration"`
	} `json:"usage"`
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	var result videoResult
	data, apiErr := readJSONResponse(resp, &result)
	if apiErr != nil {
		return "", nil, service.TaskErrorFromAPIError(apiErr)
	}
	if result.Output.TaskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("QianwenAI returned no task_id"), "invalid_response", http.StatusBadGateway)
	}
	video := dto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = info.PublicTaskID, info.PublicTaskID, info.OriginModelName
	video.Status, video.CreatedAt = dto.VideoStatusQueued, common.GetTimestamp()
	c.JSON(http.StatusOK, video)
	return result.Output.TaskID, data, nil
}

func (a *TaskAdaptor) FetchTask(base, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, _ := body["task_id"].(string)
	if id == "" || strings.ContainsAny(id, "/\\?#") {
		return nil, fmt.Errorf("invalid QianwenAI task id")
	}
	if err := ValidateBaseURL(base); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, NormalizeBaseURL(base)+"/api/v1/tasks/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(data []byte) (*relaycommon.TaskInfo, error) {
	var result videoResult
	if err := common.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if apiErr := result.apiError(); apiErr != nil {
		return nil, apiErr
	}
	status := model.TaskStatusQueued
	switch result.Output.Status {
	case "PENDING":
	case "RUNNING":
		status = model.TaskStatusInProgress
	case "SUCCEEDED":
		if result.Output.URL == "" || result.Usage.Duration <= 0 {
			return nil, fmt.Errorf("QianwenAI successful video lacks URL or duration usage")
		}
		status = model.TaskStatusSuccess
	case "FAILED", "CANCELED", "UNKNOWN":
		status = model.TaskStatusFailure
	default:
		return nil, fmt.Errorf("unknown QianwenAI task status %q", result.Output.Status)
	}
	return &relaycommon.TaskInfo{Status: status, Url: result.Output.URL, Reason: strings.TrimSpace(result.Output.Code + " " + result.Output.Message)}, nil
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, _ *relaycommon.TaskInfo) int {
	bc := task.PrivateData.BillingContext
	if bc == nil {
		return 0
	}
	var result videoResult
	if err := common.Unmarshal(task.Data, &result); err != nil || result.Usage.Duration <= 0 {
		return 0
	}
	return int(math.Round(bc.ModelPrice * common.QuotaPerUnit * bc.GroupRatio * result.Usage.Duration))
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var result videoResult
	if err := common.Unmarshal(task.Data, &result); err != nil {
		return nil, err
	}
	video := dto.NewOpenAIVideo()
	video.ID, video.TaskID, video.Model = task.TaskID, task.TaskID, task.Properties.OriginModelName
	video.Status, video.CreatedAt = task.Status.ToVideoStatus(), task.CreatedAt
	video.SetProgressStr(task.Progress)
	if task.Status == model.TaskStatusSuccess {
		video.CompletedAt = task.FinishTime
		video.SetMetadata("url", result.Output.URL)
	}
	if task.Status == model.TaskStatusFailure {
		video.Error = &dto.OpenAIVideoError{Code: result.Output.Code, Message: task.FailReason}
	}
	return common.Marshal(video)
}

var _ channel.TaskAdaptor = (*TaskAdaptor)(nil)
