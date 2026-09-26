package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	taskgmicloud "github.com/QuantumNous/new-api/relay/channel/task/gmicloud"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGMICloudImageGateway(t *testing.T, baseURL string) (*gorm.DB, *gin.Engine, *model.Channel) {
	db, engine, ch := setupTypeSafeGateway(t, baseURL, "upstream-key")
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.UserSubscription{}))
	ch.Type, ch.Models = constant.ChannelTypeGMICloud, gmicloud.HYImageModel+",hy-alias"
	ch.ModelMapping = common.GetPointer(`{"hy-alias":"hy-image-v3.5-preview"}`)
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"hy-image-v3.5-preview":0.01,"hy-alias":0.01}`))
	engine.POST("/v1/images/generations", RelayImageGeneration)
	engine.POST("/v1/images/edits", RelayImageGeneration)
	engine.POST("/v1/images/tasks", RelayImageTask)
	engine.GET("/v1/images/tasks/:task_id", RelayImageTaskFetch)
	return db, engine, ch
}

func TestGMICloudImageEditsGateway(t *testing.T) {
	for _, path := range []string{"/v1/images/edits", "/v1/images/generations", "/v1/images/tasks"} {
		t.Run(path, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				require.Equal(t, gmicloud.TaskRequestsPath, r.URL.Path)
				require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
				var body struct {
					Model   string
					Payload map[string]any
				}
				require.NoError(t, common.DecodeJson(r.Body, &body))
				require.Equal(t, gmicloud.HYImageModel, body.Model)
				require.Equal(t, "4096x4096", body.Payload["size"])
				require.Equal(t, []any{"https://example.com/reference.png"}, body.Payload["image"])
				require.Equal(t, float64(0), body.Payload["seed"])
				require.NotContains(t, body.Payload, "images")
				_, _ = io.WriteString(w, `{"request_id":"edited-image","model":"hy-image-v3.5-preview","status":"success","outcome":{"media_urls":[{"url":"https://example.com/edited.png","type":"image","width":4096,"height":4096}]}}`)
			}))
			defer server.Close()
			db, engine, _ := setupGMICloudImageGateway(t, server.URL)
			body := `{"model":"hy-alias","prompt":"make it blue","images":[{"image_url":"https://example.com/reference.png"}],"size":"4096x4096","seed":0}`
			if path == "/v1/images/tasks" {
				body = `{"model":"hy-alias","payload":{"prompt":"make it blue","image":"https://example.com/reference.png","size":"4096x4096","seed":0}}`
			}
			w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var task model.Task
			require.Eventually(t, func() bool {
				return db.First(&task).Error == nil && task.Status == model.TaskStatusSuccess
			}, 3*time.Second, 5*time.Millisecond)
			require.Equal(t, constant.TaskActionImageEdit, task.Action)
			require.Empty(t, task.PrivateData.GMICloudImageRequest)
			require.EqualValues(t, 1, posts.Load())
			assertTypeSafeQuota(t, db, 5000)
			r := httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+task.TaskID, nil)
			r.Header.Set("Authorization", "Bearer "+typeSafeGatewayKey)
			w = httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"action":"image_edit"`)
			require.Contains(t, w.Body.String(), "edited.png")
			require.NotContains(t, w.Body.String(), "upstream-key")
			// Invalid edits fail before a task, charge or upstream request.
			w = requestTypeSafe(t, engine, "/v1/images/edits", `{"model":"hy-alias","prompt":"missing reference"}`, typeSafeGatewayKey)
			require.Equal(t, http.StatusBadRequest, w.Code)
			require.EqualValues(t, 1, posts.Load())
			assertTypeSafeQuota(t, db, 5000)
		})
	}
}

func TestGMICloudImageGateway(t *testing.T) {
	for _, syncMode := range []bool{true, false} {
		t.Run(fmt.Sprint("sync=", syncMode), func(t *testing.T) {
			var posts, gets atomic.Int32
			result := `{"request_id":"upstream-image","model":"hy-image-v3.5-preview","status":"success","outcome":{"media_urls":[{"url":"https://example.com/image.png","type":"image","width":1920,"height":1080}]}}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					gets.Add(1)
					require.Equal(t, gmicloud.TaskRequestsPath+"/upstream-image", r.URL.Path)
					_, _ = io.WriteString(w, result)
					return
				}
				posts.Add(1)
				require.Equal(t, gmicloud.TaskRequestsPath, r.URL.Path)
				var body taskgmicloud.TaskRequest
				require.NoError(t, common.DecodeJson(r.Body, &body))
				require.Equal(t, gmicloud.HYImageModel, body.Model)
				require.Contains(t, string(body.Payload), `"prompt":"a cat"`)
				if syncMode {
					_, _ = io.WriteString(w, result)
				} else {
					_, _ = io.WriteString(w, `{"request_id":"upstream-image","status":"dispatched"}`)
				}
			}))
			defer server.Close()
			db, engine, ch := setupGMICloudImageGateway(t, server.URL)
			path, body := "/v1/images/tasks", `{"model":"hy-alias","payload":{"prompt":"a cat","size":"1920x1080","seed":0,"watermark":false}}`
			if syncMode {
				path, body = "/v1/images/generations", `{"model":"hy-alias","prompt":"a cat","size":"1920x1080"}`
			}
			w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if !syncMode {
				require.Eventually(t, func() bool {
					var queued model.Task
					return db.First(&queued).Error == nil && queued.Status == model.TaskStatusQueued && queued.PrivateData.UpstreamTaskID != ""
				}, 3*time.Second, 5*time.Millisecond)
			}
			var tasks []model.Task
			require.NoError(t, db.Find(&tasks).Error)
			require.Len(t, tasks, 1)
			task := &tasks[0]
			require.Equal(t, constant.TaskActionImageGeneration, task.Action)
			require.Equal(t, "upstream-image", task.GetUpstreamTaskID())
			require.Equal(t, task.TaskID, w.Header().Get("X-New-Api-Task-Id"))
			if syncMode {
				var response dto.ImageResponse
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
				require.Len(t, response.Data, 1)
				require.Equal(t, "https://example.com/image.png", response.Data[0].Url)
				require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
				require.Zero(t, gets.Load())
			} else {
				require.Contains(t, w.Body.String(), `"status":"queued"`)
				require.NoError(t, service.RefreshVideoTask(context.Background(), &taskgmicloud.TaskAdaptor{}, ch, task))
			}
			assertTypeSafeQuota(t, db, 5000)
			var logs int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
			require.EqualValues(t, 1, logs)
			r := httptest.NewRequest(http.MethodGet, "/v1/images/tasks/"+task.TaskID, nil)
			r.Header.Set("Authorization", "Bearer "+typeSafeGatewayKey)
			w = httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"status":"SUCCESS"`)
			require.Contains(t, w.Body.String(), "https://example.com/image.png")
			require.NotContains(t, w.Body.String(), "upstream-key")
			require.EqualValues(t, 1, posts.Load())
			// Public task IDs do not grant access to another user's results.
			require.NoError(t, db.Model(task).Update("user_id", 2).Error)
			w = httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			require.NotEqual(t, http.StatusOK, w.Code)
			require.NotContains(t, w.Body.String(), "image.png")
		})
	}
}

func TestGMICloudImageWaitCancellationAndCompletion(t *testing.T) {
	pending := &model.Task{TaskID: "task_test", UserId: 10, Status: model.TaskStatusInProgress}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := waitGMICloudImageTask(ctx, pending, time.Millisecond, func(int, string) (*model.Task, bool, error) {
		t.Fatal("cancelled wait must not load")
		return nil, false, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err = waitGMICloudImageTask(ctx, pending, time.Millisecond, func(int, string) (*model.Task, bool, error) { return pending, true, nil })
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), pending.Status)
	completed, err := waitGMICloudImageTask(context.Background(), pending, time.Millisecond, func(user int, id string) (*model.Task, bool, error) {
		require.Equal(t, 10, user)
		require.Equal(t, "task_test", id)
		return &model.Task{Status: model.TaskStatusSuccess}, true, nil
	})
	require.NoError(t, err)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), completed.Status)
}

func TestGMICloudImageBase64Result(t *testing.T) {
	oldMaxFile := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = oldMaxFile })
	fetchSetting := system_setting.GetFetchSetting()
	originalProtection := fetchSetting.EnableSSRFProtection
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { fetchSetting.EnableSSRFProtection = originalProtection })
	// A tiny PNG served by a mock upstream; no provider credentials or artifacts.
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aY9sAAAAASUVORK5CYII=")
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()
	service.InitHttpClient()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	task := &model.Task{SubmitTime: 123}
	task.Data = []byte(`{"outcome":{"media_urls":[{"type":"image","url":"` + server.URL + `/image.png"}]}}`)
	response, err := buildGMICloudImageResponse(c, task, "b64_json")
	require.NoError(t, err)
	require.Len(t, response.Data, 1)
	require.Empty(t, response.Data[0].Url)
	require.Equal(t, base64.StdEncoding.EncodeToString(png), response.Data[0].B64Json)
}

func TestGMICloudImageTimeoutContinuesPolling(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			_, _ = io.WriteString(w, `{"request_id":"slow-image","model":"hy-image-v3.5-preview","status":"dispatched"}`)
		} else {
			_, _ = io.WriteString(w, `{"request_id":"slow-image","model":"hy-image-v3.5-preview","status":"success","outcome":{"media_urls":[{"url":"https://example.com/late.png","type":"image"}]}}`)
		}
	}))
	defer server.Close()
	db, engine, ch := setupGMICloudImageGateway(t, server.URL)
	oldTimeout := common.RelayTimeout
	common.RelayTimeout = 1
	t.Cleanup(func() { common.RelayTimeout = oldTimeout })
	w := requestTypeSafe(t, engine, "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"a cat"}`, typeSafeGatewayKey)
	require.Equal(t, http.StatusGatewayTimeout, w.Code, w.Body.String())
	var task model.Task
	require.NoError(t, db.First(&task).Error)
	require.Equal(t, task.TaskID, w.Header().Get("X-New-Api-Task-Id"))
	require.Equal(t, model.TaskStatus(model.TaskStatusQueued), task.Status)
	assertTypeSafeQuota(t, db, 5000)
	require.NoError(t, service.RefreshVideoTask(context.Background(), &taskgmicloud.TaskAdaptor{}, ch, &task))
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	assertTypeSafeQuota(t, db, 5000)
	require.EqualValues(t, 1, posts.Load())
}

func TestGMICloudImageSubmissionFailureRefundsOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"request_id":"failed-image","model":"hy-image-v3.5-preview","status":"failed","error":{"message":"generation failed"}}`)
	}))
	defer server.Close()
	db, engine, ch := setupGMICloudImageGateway(t, server.URL)
	w := requestTypeSafe(t, engine, "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"a cat"}`, typeSafeGatewayKey)
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	var task model.Task
	require.NoError(t, db.First(&task).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
	assertTypeSafeQuota(t, db, 0)
	require.NoError(t, service.RefreshVideoTask(context.Background(), &taskgmicloud.TaskAdaptor{}, ch, &task))
	assertTypeSafeQuota(t, db, 0)
}

func TestGMICloudImageErrorCarriesTaskID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	respondGMICloudImageError(c, 504, "image_task_wait_timeout", "still running", "task_test")
	require.Equal(t, 504, w.Code)
	require.Equal(t, "task_test", w.Header().Get("X-New-Api-Task-Id"))
	require.True(t, strings.Contains(w.Body.String(), `"task_id":"task_test"`))
}

func TestGMICloudBlockedSubmissionOutlivesClientWait(t *testing.T) {
	for _, syncMode := range []bool{false, true} {
		t.Run(fmt.Sprint("sync=", syncMode), func(t *testing.T) {
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				<-release
				_, _ = io.WriteString(w, `{"request_id":"blocked-image","model":"hy-image-v3.5-preview","status":"success","outcome":{"media_urls":[{"url":"https://example.com/image.png","type":"image"}]}}`)
			}))
			defer server.Close()
			defer unblock()
			db, engine, _ := setupGMICloudImageGateway(t, server.URL)
			oldTimeout := common.RelayTimeout
			common.RelayTimeout = 1
			t.Cleanup(func() { common.RelayTimeout = oldTimeout })
			path, body := "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":{"prompt":"cat"}}`
			if syncMode {
				path, body = "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"cat"}`
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- requestTypeSafe(t, engine, path, body, typeSafeGatewayKey) }()
			var w *httptest.ResponseRecorder
			returnedEarly := false
			select {
			case w = <-done:
				returnedEarly = true
			case <-time.After(3 * time.Second):
			}
			unblock()
			if w == nil {
				w = <-done
			}
			// Drain the worker before fixture cleanup can replace the database.
			require.Eventually(t, func() bool {
				var task model.Task
				return db.First(&task).Error == nil && task.Status == model.TaskStatusSuccess
			}, 3*time.Second, 5*time.Millisecond)
			require.True(t, returnedEarly, "client response must not wait for the blocked upstream POST")
			if syncMode {
				require.Equal(t, 504, w.Code)
			} else {
				require.Equal(t, 200, w.Code)
			}
			require.EqualValues(t, 1, posts.Load())
			assertTypeSafeQuota(t, db, 5000)
		})
	}
}
