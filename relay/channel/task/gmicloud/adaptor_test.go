package gmicloud

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	channelgmicloud "github.com/QuantumNous/new-api/relay/channel/gmicloud"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateRequestAndBuildBodyPreservesExplicitZeroValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/generations", strings.NewReader(`{
		"model":"minimax-tts-speech-2.8-turbo",
		"payload":{"text":"hello","pitch":0,"vm_pitch":0,"need_noise_reduction":false}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		OriginModelName: "minimax-tts-speech-2.8-turbo",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    channelgmicloud.DefaultBaseURL,
			ApiKey:            "test-key",
			UpstreamModelName: "minimax-tts-speech-2.8-turbo",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Equal(t, constant.TaskActionAudioGeneration, info.Action)
	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	body, err := io.ReadAll(requestBody)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, common.Unmarshal(body, &decoded))
	payload := decoded["payload"].(map[string]any)
	require.Contains(t, payload, "pitch")
	require.Equal(t, float64(0), payload["pitch"])
	require.Contains(t, payload, "vm_pitch")
	require.Equal(t, float64(0), payload["vm_pitch"])
	require.Contains(t, payload, "need_noise_reduction")
	require.Equal(t, false, payload["need_noise_reduction"])

	requestURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, channelgmicloud.TaskBaseURL+channelgmicloud.TaskRequestsPath, requestURL)
}

func TestValidateRequestSetsModelSpecificActions(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		action string
	}{
		{
			name:   "music",
			body:   `{"model":"minimax-music-3.0","payload":{"lyrics":"[Verse]\nHello"}}`,
			action: constant.TaskActionMusicGeneration,
		},
		{
			name:   "voice clone",
			body:   `{"model":"minimax-audio-voice-clone-speech-2.8-hd","payload":{"text":"hello","source_audio":"https://example.com/voice.wav"}}`,
			action: constant.TaskActionVoiceClone,
		},
		{
			name:   "gemini batch lowercase alias",
			body:   `{"model":"gemini-batch-inference","payload":{"model":"gemini-3-flash-preview","input_data":"{\"request\":{\"contents\":[]}}"}}`,
			action: constant.TaskActionBatchInference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/generations", strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))
			require.Equal(t, tt.action, info.Action)
			if tt.action == constant.TaskActionVoiceClone {
				stored, ok := c.Get("task_request")
				require.True(t, ok)
				request := stored.(*TaskRequest)
				var payload map[string]any
				require.NoError(t, common.Unmarshal(request.Payload, &payload))
				require.Regexp(t, `^gmi_[A-Za-z0-9]{20}$`, payload["voice_id"])
			}
		})
	}
}

func TestBatchRequestUsesCanonicalUpstreamModel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/batch/generations", strings.NewReader(`{
		"model":"gemini-batch-inference",
		"payload":{"model":"gemini-3-flash-preview","input_data":"{\"request\":{\"contents\":[]}}"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-batch-inference",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-batch-inference",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	requestBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	var request TaskRequest
	require.NoError(t, common.Unmarshal(requestBytes, &request))
	require.Equal(t, channelgmicloud.BatchInferenceModel, request.Model)
	require.Equal(t, channelgmicloud.BatchInferenceModel, info.UpstreamModelName)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(request.Payload, &payload))
	require.Equal(t, "gemini-3-flash-preview", payload["model"])
	require.Contains(t, payload["input_data"], "contents")
}

func TestDoResponseUsesPublicTaskID(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"request_id":"upstream-secret-id",
			"model":"minimax-music-3.0",
			"status":"success"
		}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "minimax-music-3.0",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "upstream-secret-id", upstreamID)
	require.Contains(t, string(taskData), "upstream-secret-id")
	require.Contains(t, recorder.Body.String(), "task_public")
	require.NotContains(t, recorder.Body.String(), "upstream-secret-id")
}

func TestFetchTaskUsesQueueEndpointAndBearerAuth(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, channelgmicloud.TaskRequestsPath+"/request-123", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"request_id":"request-123","status":"processing"}`))
	}))
	defer server.Close()

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "test-key", map[string]any{"task_id": "request-123"}, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestParseTaskResult(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		status   model.TaskStatus
		url      string
		reason   string
		progress string
	}{
		{
			name:     "queued",
			body:     `{"request_id":"1","status":"queued"}`,
			status:   model.TaskStatusQueued,
			progress: "20%",
		},
		{
			name:     "dispatched",
			body:     `{"request_id":"1b","status":"dispatched"}`,
			status:   model.TaskStatusQueued,
			progress: "20%",
		},
		{
			name:     "music success",
			body:     `{"request_id":"2","status":"success","outcome":{"audio_url":"https://example.com/music.mp3"}}`,
			status:   model.TaskStatusSuccess,
			url:      "https://example.com/music.mp3",
			progress: "100%",
		},
		{
			name:     "speech success",
			body:     `{"request_id":"3","status":"success","outcome":{"media_urls":[{"id":"0","url":"https://example.com/speech.mp3"}]}}`,
			status:   model.TaskStatusSuccess,
			url:      "https://example.com/speech.mp3",
			progress: "100%",
		},
		{
			name:     "failed",
			body:     `{"request_id":"4","status":"failed","error":{"message":"generation failed"}}`,
			status:   model.TaskStatusFailure,
			reason:   "generation failed",
			progress: "100%",
		},
		{
			name:     "batch running",
			body:     `{"request_id":"5","status":"processing","outcome":{"batch_job_state":"JOB_STATE_RUNNING"}}`,
			status:   model.TaskStatusInProgress,
			progress: "50%",
		},
		{
			name:     "batch success",
			body:     `{"request_id":"6","status":"success","outcome":{"batch_job_state":"JOB_STATE_SUCCEEDED","output_url":"https://example.com/predictions.jsonl","token_usage":{"total_prompt_tokens":5,"total_candidates_tokens":8}}}`,
			status:   model.TaskStatusSuccess,
			url:      "https://example.com/predictions.jsonl",
			progress: "100%",
		},
		{
			name:     "batch failed with outcome error",
			body:     `{"request_id":"7","status":"processing","outcome":{"batch_job_state":"JOB_STATE_FAILED","error":{"message":"vertex batch failed"}}}`,
			status:   model.TaskStatusFailure,
			reason:   "vertex batch failed",
			progress: "100%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(tt.body))
			require.NoError(t, err)
			require.Equal(t, string(tt.status), result.Status)
			require.Equal(t, tt.url, result.Url)
			require.Equal(t, tt.reason, result.Reason)
			require.Equal(t, tt.progress, result.Progress)
		})
	}
}

func TestGetModelListIncludesFreeSpeechAndMusicModels(t *testing.T) {
	models := (&TaskAdaptor{}).GetModelList()
	require.Contains(t, models, "minimax-tts-speech-2.8-turbo")
	require.Contains(t, models, "minimax-tts-speech-2.8-hd")
	require.Contains(t, models, "minimax-audio-voice-clone-speech-2.8-turbo")
	require.Contains(t, models, "minimax-audio-voice-clone-speech-2.8-hd")
	require.Contains(t, models, "minimax-music-3.0")
	require.Contains(t, models, channelgmicloud.BatchInferenceModel)
}
