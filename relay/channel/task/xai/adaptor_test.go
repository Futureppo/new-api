package xai

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseTaskResultDone(t *testing.T) {
	body := []byte(`{
		"status": "done",
		"video": {
			"url": "https://vidgen.x.ai/result.mp4",
			"duration": 6,
			"respect_moderation": true
		},
		"model": "grok-imagine-video",
		"usage": {
			"cost_in_usd_ticks": 500000000
		},
		"progress": 100
	}`)

	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult(body)

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
	require.Equal(t, "100%", taskInfo.Progress)
	require.Equal(t, "https://vidgen.x.ai/result.mp4", taskInfo.Url)
}

func TestEstimateBillingUsesDurationAndResolution(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("xai_video_request", map[string]any{
		"duration":   float64(6),
		"resolution": "720p",
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(ctx, nil)

	require.Equal(t, 6.0, ratios["seconds"])
	require.Equal(t, 1.4, ratios["resolution"])
}

func TestConvertToOpenAIVideoReturnsXAIShapeForPendingTask(t *testing.T) {
	task := &model.Task{
		TaskID:   "task_public",
		Status:   model.TaskStatusQueued,
		Progress: "20%",
		Properties: model.Properties{
			OriginModelName: "grok-imagine-video",
		},
		Data: []byte(`{"request_id":"upstream_id"}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, common.Unmarshal(data, &got))
	require.Equal(t, "pending", got["status"])
	require.Equal(t, "grok-imagine-video", got["model"])
	require.Equal(t, float64(20), got["progress"])
}
