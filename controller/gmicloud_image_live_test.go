package controller

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	taskgmicloud "github.com/QuantumNous/new-api/relay/channel/task/gmicloud"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// Explicitly opt in only after checking current pricing. At most two images,
// 1024x1024, no automatic resubmission. Credentials stay in process memory.
func TestGMICloudHYLive(t *testing.T) {
	if os.Getenv("GMICLOUD_HY_LIVE_TEST") != "1" {
		t.Skip("opt-in paid upstream test")
	}
	key := os.Getenv("GMICLOUD_API_KEY")
	if key == "" {
		t.Fatal("GMICLOUD_API_KEY is required")
	}
	db, engine, ch := setupGMICloudImageGateway(t, gmicloud.DefaultBaseURL)
	ch.Key = key
	require.NoError(t, ch.Update())
	for _, mode := range []string{"sync", "async"} {
		path, body := "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"A small red apple on a plain white background","size":"1024x1024"}`
		if mode == "async" {
			path, body = "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":{"prompt":"A small green pear on a plain white background","size":"1024x1024","seed":0}}`
		}
		started := time.Now()
		w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
		var task model.Task
		require.NoError(t, db.Where("task_id = ?", w.Header().Get("X-New-Api-Task-Id")).First(&task).Error)
		deadline := time.Now().Add(3 * time.Minute)
		for task.Status != model.TaskStatusSuccess && task.Status != model.TaskStatusFailure && time.Now().Before(deadline) {
			time.Sleep(5 * time.Second)
			require.NoError(t, db.First(&task, task.ID).Error)
			if task.PrivateData.GMICloudImageRequest == "" && task.PrivateData.UpstreamTaskID != "" {
				require.NoError(t, service.RefreshVideoTask(context.Background(), &taskgmicloud.TaskAdaptor{}, ch, &task))
			}
		}
		t.Logf("HY %s: HTTP %d, status %s, elapsed %s", mode, w.Code, task.Status, time.Since(started).Round(time.Second))
		if task.Status != model.TaskStatusSuccess {
			t.Fatalf("upstream generation did not succeed: %s; stopping without another POST", task.FailReason)
		}
		require.Equal(t, http.StatusOK, w.Code)
		images, err := taskgmicloud.ParseImageResults(task.Data)
		require.NoError(t, err)
		require.NotEmpty(t, images)
		require.Equal(t, gmicloud.HYImageModel, task.Properties.UpstreamModelName)
		t.Logf("HY %s: %d image(s), dimensions %dx%d, result URL present", mode, len(images), images[0].Width, images[0].Height)
	}
}
