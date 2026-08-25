package gmicloud

import (
	"bytes"
	"encoding/json"
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
	channelgmicloud "github.com/QuantumNous/new-api/relay/channel/gmicloud"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type TaskRequest struct {
	Model   string          `json:"model"`
	Payload json.RawMessage `json:"payload"`
}

type taskResponse struct {
	RequestID string          `json:"request_id"`
	Model     string          `json:"model"`
	Status    string          `json:"status"`
	Message   string          `json:"message,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
	Outcome   taskOutcome     `json:"outcome,omitempty"`
}

type taskOutcome struct {
	AudioURL  string      `json:"audio_url,omitempty"`
	MediaURLs []taskMedia `json:"media_urls,omitempty"`
	Medias    []taskMedia `json:"medias,omitempty"`
}

type taskMedia struct {
	URL string `json:"url"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
	apiKey  string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.baseURL = channelgmicloud.ResolveTaskBaseURL(info.ChannelBaseUrl)
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var req TaskRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	modelName := strings.TrimSpace(req.Model)
	if modelName == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model is required"), "missing_model", http.StatusBadRequest)
	}
	var payload map[string]json.RawMessage
	if len(req.Payload) == 0 || common.Unmarshal(req.Payload, &payload) != nil || payload == nil {
		return service.TaskErrorWrapperLocal(fmt.Errorf("payload must be a JSON object"), "invalid_payload", http.StatusBadRequest)
	}

	_, hasLyrics := payload["lyrics"]
	if channelgmicloud.IsMusicModel(modelName) || hasLyrics {
		if err := requirePayloadString(payload, "lyrics"); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_payload", http.StatusBadRequest)
		}
		info.Action = constant.TaskActionMusicGeneration
	} else {
		if err := requirePayloadString(payload, "text"); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_payload", http.StatusBadRequest)
		}
		_, hasSourceAudio := payload["source_audio"]
		if channelgmicloud.IsVoiceCloneModel(modelName) || hasSourceAudio {
			if err := requirePayloadString(payload, "source_audio"); err != nil {
				return service.TaskErrorWrapperLocal(err, "invalid_payload", http.StatusBadRequest)
			}
			if err := ensureVoiceID(&req, payload); err != nil {
				return service.TaskErrorWrapperLocal(err, "invalid_payload", http.StatusBadRequest)
			}
			info.Action = constant.TaskActionVoiceClone
		} else {
			info.Action = constant.TaskActionAudioGeneration
		}
	}

	if info.OriginModelName == "" {
		info.OriginModelName = modelName
	}
	c.Set("task_request", &req)
	return nil
}

func ensureVoiceID(req *TaskRequest, payload map[string]json.RawMessage) error {
	if raw, ok := payload["voice_id"]; ok {
		var voiceID string
		if common.Unmarshal(raw, &voiceID) == nil && strings.TrimSpace(voiceID) != "" {
			return nil
		}
	}

	randomPart, err := common.GenerateRandomCharsKey(20)
	if err != nil {
		return fmt.Errorf("generate voice_id failed: %w", err)
	}
	voiceID, err := common.Marshal("gmi_" + randomPart)
	if err != nil {
		return fmt.Errorf("marshal voice_id failed: %w", err)
	}
	payload["voice_id"] = voiceID
	req.Payload, err = common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload failed: %w", err)
	}
	return nil
}

func requirePayloadString(payload map[string]json.RawMessage, field string) error {
	raw, ok := payload[field]
	if !ok {
		return fmt.Errorf("payload.%s is required", field)
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
		return fmt.Errorf("payload.%s must be a non-empty string", field)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + channelgmicloud.TaskRequestsPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	value, ok := c.Get("task_request")
	if !ok {
		return nil, fmt.Errorf("task_request not found in context")
	}
	req, ok := value.(*TaskRequest)
	if !ok || req == nil {
		return nil, fmt.Errorf("invalid task_request type")
	}

	modelName := strings.TrimSpace(info.UpstreamModelName)
	if modelName == "" {
		modelName = req.Model
	}
	body, err := common.Marshal(TaskRequest{Model: modelName, Payload: req.Payload})
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var upstream taskResponse
	if err := common.Unmarshal(responseBody, &upstream); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if strings.TrimSpace(upstream.RequestID) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("request_id is empty: %s", responseReason(upstream)), "invalid_response", http.StatusInternalServerError)
	}

	status := strings.ToLower(strings.TrimSpace(upstream.Status))
	if status == "" {
		status = "queued"
	}
	c.JSON(http.StatusOK, gin.H{
		"id":      info.PublicTaskID,
		"task_id": info.PublicTaskID,
		"model":   info.OriginModelName,
		"status":  status,
	})
	return upstream.RequestID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	requestURL := channelgmicloud.ResolveTaskBaseURL(baseURL) + channelgmicloud.TaskRequestsPath + "/" + url.PathEscape(taskID)
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
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
	var response taskResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("unmarshal GMICLOUD task result failed: %w", err)
	}

	result := &relaycommon.TaskInfo{TaskID: response.RequestID}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "queued", "pending", "submitted", "dispatched":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "processing", "running", "in_progress":
		result.Status = model.TaskStatusInProgress
		result.Progress = "50%"
	case "success", "succeeded", "completed", "done":
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		result.Url = response.Outcome.primaryURL()
		if result.Url == "" {
			result.Status = model.TaskStatusFailure
			result.Reason = "GMICLOUD task succeeded without an audio result URL"
		}
	case "failed", "failure", "cancelled", "canceled":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = responseReason(response)
		if result.Reason == "" {
			result.Reason = "GMICLOUD task failed"
		}
	default:
		reason := responseReason(response)
		if reason != "" {
			result.Status = model.TaskStatusFailure
			result.Progress = taskcommon.ProgressComplete
			result.Reason = reason
		}
	}
	return result, nil
}

func (o taskOutcome) primaryURL() string {
	if strings.TrimSpace(o.AudioURL) != "" {
		return o.AudioURL
	}
	for _, items := range [][]taskMedia{o.MediaURLs, o.Medias} {
		for _, item := range items {
			if strings.TrimSpace(item.URL) != "" {
				return item.URL
			}
		}
	}
	return ""
}

func responseReason(response taskResponse) string {
	if strings.TrimSpace(response.Message) != "" {
		return response.Message
	}
	if len(response.Error) == 0 || string(response.Error) == "null" {
		return ""
	}
	var message string
	if err := common.Unmarshal(response.Error, &message); err == nil {
		return message
	}
	var detail struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := common.Unmarshal(response.Error, &detail); err == nil {
		if detail.Message != "" {
			return detail.Message
		}
		if detail.Detail != "" {
			return detail.Detail
		}
	}
	return strings.TrimSpace(string(response.Error))
}

func (a *TaskAdaptor) GetModelList() []string {
	return channelgmicloud.AudioModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return channelgmicloud.ChannelName
}
