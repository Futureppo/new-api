package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	core "github.com/QuantumNous/new-api/relay/channel/openrouter"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// OpenRouter reports video cost, not token counts. Keep the site's per-task
// price/ratio and group multiplier; never use upstream cost for settlement.
type TaskAdaptor struct{ taskcommon.BaseBilling }

func (*TaskAdaptor) Init(*relaycommon.RelayInfo) {}
func (*TaskAdaptor) GetModelList() []string      { return nil }
func (*TaskAdaptor) GetChannelName() string      { return "openrouter" }

func (*TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		return service.TaskErrorWrapperLocal(fmt.Errorf("OpenRouter native videos require a standard channel"), "invalid_request", 400)
	}
	if info.Action == constant.TaskActionRemix {
		return service.TaskErrorWrapperLocal(fmt.Errorf("use previous_job_id on /v1/videos to continue an OpenRouter video"), "invalid_request", 400)
	}
	info.Action = constant.TaskActionGenerate
	_, err := readFields(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", 400)
	}
	return nil
}

func readFields(c *gin.Context) (map[string]json.RawMessage, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = common.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("OpenRouter videos require a JSON object")
	}
	return fields, nil
}

func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	_, err := a.prepare(c, info)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", 400)
	}
	return nil
}

func (a *TaskAdaptor) prepare(c *gin.Context, info *relaycommon.RelayInfo) ([]byte, error) {
	fields, err := readFields(c)
	if err != nil {
		return nil, err
	}
	fields["model"], err = common.Marshal(info.UpstreamModelName)
	if err != nil {
		return nil, err
	}
	if raw, ok := fields["seconds"]; ok {
		var seconds string
		if common.Unmarshal(raw, &seconds) == nil {
			raw = json.RawMessage(seconds)
		}
		if duration, exists := fields["duration"]; exists && !bytes.Equal(bytes.TrimSpace(duration), bytes.TrimSpace(raw)) {
			return nil, fmt.Errorf("seconds conflicts with duration")
		}
		fields["duration"] = raw
		delete(fields, "seconds")
	}
	data, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	data, err = relaycommon.RemoveDisabledFields(data, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
		if err != nil {
			return nil, err
		}
	}
	fields = nil
	if err = common.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var request struct {
		Model         string  `json:"model"`
		Duration      *int    `json:"duration,omitempty"`
		GenerateAudio *bool   `json:"generate_audio,omitempty"`
		Seed          *int64  `json:"seed,omitempty"`
		PreviousJobID *string `json:"previous_job_id,omitempty"`
		Stream        *bool   `json:"stream,omitempty"`
	}
	if err = common.Unmarshal(data, &request); err != nil {
		return nil, err
	}
	if request.Model == "" || request.Model != info.UpstreamModelName {
		return nil, fmt.Errorf("use model mapping to select the video model")
	}
	if request.Duration != nil && *request.Duration < 1 {
		return nil, fmt.Errorf("duration must be a positive integer")
	}
	if request.Stream != nil && *request.Stream {
		return nil, fmt.Errorf("video generation is asynchronous; use the polling URL")
	}
	if request.PreviousJobID != nil {
		origin, exists, err := model.GetByTaskId(info.UserId, *request.PreviousJobID)
		if err != nil {
			return nil, err
		}
		if !exists || origin.Status != model.TaskStatusSuccess || origin.Platform != constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenRouter)) || origin.ChannelId != info.ChannelId {
			return nil, fmt.Errorf("previous_job_id must be your completed OpenRouter task on the same channel")
		}
		if origin.Properties.UpstreamModelName != info.UpstreamModelName {
			return nil, fmt.Errorf("continuation must use the original model")
		}
		if origin.PrivateData.Key == "" {
			return nil, fmt.Errorf("original task credential is unavailable")
		}
		info.ApiKey = origin.PrivateData.Key
		fields["previous_job_id"], err = common.Marshal(origin.GetUpstreamTaskID())
		if err != nil {
			return nil, err
		}
		return common.Marshal(fields)
	}
	return data, nil
}

func (*TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return core.NormalizeBaseURL(info.ChannelBaseUrl) + "/v1/videos", nil
}
func (*TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	headers, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("HTTP-Referer") == "" {
		req.Header.Set("HTTP-Referer", "https://www.newapi.ai")
	}
	if req.Header.Get("X-Title") == "" {
		req.Header.Set("X-Title", "New API")
	}
	return nil
}
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	data, err := a.prepare(c, info)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	c.Set("openrouter_video_submitted", true)
	resp, err := channel.DoTaskApiRequest(a, c, info, body)
	if err == nil && resp != nil && resp.StatusCode >= 400 {
		c.Set("openrouter_video_submitted", false)
	}
	return resp, err
}

type videoResponse struct {
	ID     string          `json:"id"`
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error"`
}

func (*TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapperLocal(err, "invalid_response", 502)
	}
	var result videoResponse
	if common.Unmarshal(body, &result) != nil || result.ID == "" || result.Status == "" || (len(result.Error) > 0 && string(result.Error) != "null" && string(result.Error) != `""`) {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("OpenRouter rejected the video request or returned an invalid job"), "invalid_response", 502)
	}
	if _, err := (&TaskAdaptor{}).ParseTaskResult(body); err != nil {
		return "", nil, service.TaskErrorWrapperLocal(err, "invalid_response", 502)
	}
	// The controller writes acceptance only after storing the local task.
	return result.ID, body, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	return a.FetchTaskContext(context.Background(), baseURL, key, body, proxy)
}
func (*TaskAdaptor) FetchTaskContext(ctx context.Context, baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	id, _ := body["task_id"].(string)
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, core.NormalizeBaseURL(baseURL)+"/v1/videos/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
func (*TaskAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var result videoResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.ID == "" {
		return nil, fmt.Errorf("invalid OpenRouter video job")
	}
	if len(result.Error) > 0 && string(result.Error) != "null" && string(result.Error) != `""` && result.Status != "failed" && result.Status != "cancelled" && result.Status != "expired" {
		return nil, fmt.Errorf("OpenRouter returned a video error without a failed job status")
	}
	info := &relaycommon.TaskInfo{TaskID: result.ID}
	switch result.Status {
	case "pending":
		info.Status = model.TaskStatusQueued
		info.Progress = "20%"
	case "in_progress":
		info.Status = model.TaskStatusInProgress
		info.Progress = "50%"
	case "completed":
		info.Status = model.TaskStatusSuccess
		info.Progress = "100%"
	case "failed", "cancelled", "expired":
		info.Status = model.TaskStatusFailure
		info.Progress = "100%"
		_ = common.Unmarshal(result.Error, &info.Reason)
		if info.Reason == "" {
			info.Reason = "OpenRouter video " + result.Status
		}
	default:
		return nil, fmt.Errorf("unknown OpenRouter video status %q", result.Status)
	}
	return info, nil
}

func (*TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(task.Data, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("invalid stored OpenRouter job")
	}
	set := func(key string, v any) { fields[key], _ = common.Marshal(v) }
	set("id", task.TaskID)
	set("polling_url", "/v1/videos/"+task.TaskID)
	if task.Status == model.TaskStatusFailure {
		var status string
		_ = common.Unmarshal(fields["status"], &status)
		if status != "failed" && status != "cancelled" && status != "expired" {
			set("status", "failed")
		}
		set("error", task.FailReason)
	}
	if task.Status == model.TaskStatusSuccess {
		set("status", "completed")
		var urls []string
		_ = common.Unmarshal(fields["unsigned_urls"], &urls)
		if len(urls) == 0 {
			_ = common.Unmarshal(fields["urls"], &urls)
		}
		if len(urls) == 0 {
			urls = make([]string, 1)
		}
		for i := range urls {
			urls[i] = taskcommon.BuildProxyURL(task.TaskID) + "?index=" + strconv.Itoa(i)
		}
		set("unsigned_urls", urls)
		delete(fields, "urls")
	}
	return common.Marshal(fields)
}
