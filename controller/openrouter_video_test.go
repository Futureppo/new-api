package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	taskopenrouter "github.com/QuantumNous/new-api/relay/channel/task/openrouter"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func TestOpenRouterVideoLifecycle(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed=%t", failed), func(t *testing.T) {
			var posts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts.Add(1)
					require.Equal(t, "/proxy/v1/videos", r.URL.Path)
					require.Equal(t, "https://www.newapi.ai", r.Header.Get("HTTP-Referer"))
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, "vendor/mapped", gjson.GetBytes(body, "model").String())
					require.Equal(t, "0", gjson.GetBytes(body, "seed").Raw)
					require.Equal(t, "false", gjson.GetBytes(body, "generate_audio").Raw)
					require.Equal(t, int64(8), gjson.GetBytes(body, "duration").Int())
					require.Equal(t, "image_url", gjson.GetBytes(body, "input_references.0.type").String())
					require.False(t, gjson.GetBytes(body, "safety_identifier").Exists())
					if posts.Load() > 1 {
						require.Equal(t, "job-1", gjson.GetBytes(body, "previous_job_id").String())
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"job-%d","status":"pending","polling_url":"/api/v1/videos/job-%d"}`, posts.Load(), posts.Load()))
				} else if strings.HasSuffix(r.URL.Path, "/content") {
					require.Equal(t, "/proxy/v1/videos/job-1/content", r.URL.Path)
					require.Equal(t, "1", r.URL.Query().Get("index"))
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = io.WriteString(w, "video-bytes")
				} else {
					require.Equal(t, "/proxy/v1/videos/job-1", r.URL.Path)
					if failed {
						_, _ = io.WriteString(w, `{"id":"job-1","status":"failed","error":"generation failed"}`)
					} else {
						_, _ = io.WriteString(w, `{"id":"job-1","status":"completed","unsigned_urls":["https://not-used.example/one","https://not-used.example/two"],"usage":{"cost":999},"extension":false}`)
					}
				}
			}))
			defer upstream.Close()
			db, engine, ch := setupOpenRouterGateway(t, upstream.URL+"/proxy/v1/")
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"or-test":0.002}`))
			ch.ParamOverride = common.GetPointer(`{"seed":0,"generate_audio":false}`)
			require.NoError(t, ch.Update())
			engine.POST("/v1/videos", RelayTask)
			engine.GET("/v1/videos/:task_id", RelayTaskFetch)
			engine.GET("/v1/videos/:task_id/content", VideoProxy)
			body := `{"model":"or-test","prompt":"cat","seconds":"8","seed":42,"generate_audio":true,"safety_identifier":"private","input_references":[{"type":"image_url","image_url":{"url":"https://example.com/ref.png"}}]}`
			w := requestMiMo(t, engine, "/v1/videos", body, mimoGatewayKey)
			require.Equal(t, 202, w.Code, w.Body.String())
			id := gjson.Get(w.Body.String(), "id").String()
			require.True(t, strings.HasPrefix(id, "task_"), id)
			require.Equal(t, "/v1/videos/"+id, gjson.Get(w.Body.String(), "polling_url").String())
			var task model.Task
			require.NoError(t, db.Where("task_id = ?", id).First(&task).Error)
			require.Equal(t, "upstream-key", task.PrivateData.Key)
			assertMiMoQuota(t, db, 1000)
			ch.Key = "changed-key"
			require.NoError(t, ch.Update())
			stale := task
			adaptor := &taskopenrouter.TaskAdaptor{}
			require.NoError(t, service.RefreshVideoTask(context.Background(), adaptor, ch, &task))
			require.NoError(t, service.RefreshVideoTask(context.Background(), adaptor, ch, &stale))
			if failed {
				require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
				assertMiMoQuota(t, db, 0)
				return
			}
			assertMiMoQuota(t, db, 1000)
			get := func(path string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.Header.Set("Authorization", "Bearer "+mimoGatewayKey)
				out := httptest.NewRecorder()
				engine.ServeHTTP(out, r)
				return out
			}
			status := get("/v1/videos/" + id)
			require.Equal(t, 200, status.Code, status.Body.String())
			require.Equal(t, id, gjson.Get(status.Body.String(), "id").String())
			require.Contains(t, gjson.Get(status.Body.String(), "unsigned_urls.1").String(), "/v1/videos/"+id+"/content?index=1")
			require.Equal(t, "false", gjson.Get(status.Body.String(), "extension").Raw)
			fetch := system_setting.GetFetchSetting()
			oldSSRF := fetch.EnableSSRFProtection
			fetch.EnableSSRFProtection = false
			t.Cleanup(func() { fetch.EnableSSRFProtection = oldSSRF })
			content := get("/v1/videos/" + id + "/content?index=1")
			require.Equal(t, 200, content.Code, content.Body.String())
			require.Equal(t, "video-bytes", content.Body.String())
			require.Equal(t, "video/mp4", content.Header().Get("Content-Type"))
			require.Equal(t, "private, no-store", content.Header().Get("Cache-Control"))
			continued := strings.TrimSuffix(body, "}") + `,"previous_job_id":"` + id + `"}`
			w = requestMiMo(t, engine, "/v1/videos", continued, mimoGatewayKey)
			require.Equal(t, 202, w.Code, w.Body.String())
			assertMiMoQuota(t, db, 2000)
			// Another user's resource cannot be used as a continuation or downloaded.
			require.NoError(t, db.Model(&model.Task{}).Where("task_id = ?", id).Update("user_id", 2).Error)
			w = requestMiMo(t, engine, "/v1/videos", continued, mimoGatewayKey)
			require.Equal(t, 400, w.Code, w.Body.String())
			require.Equal(t, 404, get("/v1/videos/"+id+"/content").Code)
			require.EqualValues(t, 2, posts.Load())
			assertMiMoQuota(t, db, 2000)
		})
	}
}

func TestOpenRouterVideoRejectedSubmissionRefunds(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request"}}`)
	}))
	defer upstream.Close()
	db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
	common.RetryTimes = 2
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"or-test":0.002}`))
	engine.POST("/v1/videos", RelayTask)
	w := requestMiMo(t, engine, "/v1/videos", `{"model":"or-test","prompt":"cat"}`, mimoGatewayKey)
	require.Equal(t, 502, w.Code, w.Body.String())
	assertMiMoQuota(t, db, 0)
	var count int64
	require.NoError(t, db.Model(&model.Task{}).Count(&count).Error)
	require.Zero(t, count)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenRouterVideoSubmissionFailureRefunds(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		t.Run(fmt.Sprint(disconnected), func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if disconnected {
					conn, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					_ = conn.Close()
					return
				}
				w.WriteHeader(202)
				_, _ = io.WriteString(w, `{"id":"job-1","status":"pending"}`)
			}))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
			common.RetryTimes = 2
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"or-test":0.002}`))
			if !disconnected {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_openrouter_task", func(tx *gorm.DB) {
					if tx.Statement.Table == "tasks" {
						tx.AddError(fmt.Errorf("simulated task persistence failure"))
					}
				}))
			}
			engine.POST("/v1/videos", RelayTask)
			w := requestMiMo(t, engine, "/v1/videos", `{"model":"or-test","prompt":"cat"}`, mimoGatewayKey)
			require.Equal(t, 500, w.Code, w.Body.String())
			require.EqualValues(t, 1, calls.Load(), "an ambiguous submit must never create another billable job")
			assertMiMoQuota(t, db, 0)
		})
	}
}

func TestOpenRouterVideoFinalValidationAndChannelTest(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/v1/videos", r.URL.Path)
		w.WriteHeader(202)
		_, _ = io.WriteString(w, `{"id":"job-1","status":"pending"}`)
	}))
	defer upstream.Close()
	db, engine, ch := setupOpenRouterGateway(t, upstream.URL)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"or-test":0.002}`))
	engine.POST("/v1/videos", RelayTask)
	for _, override := range []string{`{"duration":0}`, `{"stream":true}`, `{"model":"bypass/mapping"}`, `{"previous_job_id":"not-owned"}`} {
		ch.ParamOverride = common.GetPointer(override)
		require.NoError(t, ch.Update())
		w := requestMiMo(t, engine, "/v1/videos", `{"model":"or-test","prompt":"cat"}`, mimoGatewayKey)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	ch.ParamOverride = common.GetPointer(`{}`)
	require.NoError(t, ch.Update())
	origin := &model.Task{TaskID: "task_other_provider", UserId: 1, ChannelId: ch.Id, Platform: "1", Status: model.TaskStatusSuccess}
	require.NoError(t, origin.Insert())
	w := requestMiMo(t, engine, "/v1/videos", `{"model":"or-test","previous_job_id":"task_other_provider"}`, mimoGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "OpenRouter video task")
	require.Zero(t, calls.Load())
	assertMiMoQuota(t, db, 0)
	ch.ParamOverride = common.GetPointer(`{"seed":0,"generate_audio":false}`)
	require.NoError(t, ch.Update())
	test := testChannel(ch, "or-test", string(constant.EndpointTypeOpenAIVideo), false)
	require.NoError(t, test.localErr)
	require.Nil(t, test.newAPIError)
	require.EqualValues(t, 1, calls.Load())
}

func TestOpenRouterVideoPollingRejectsTransientErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{503, `{"id":"job-1","status":"failed","error":"temporary"}`},
		{200, `{"error":{"message":"retry later"}}`},
		{200, `{"id":"another-job","status":"failed","error":"not yours"}`},
		{200, `{"id":"job-1","status":"completed","error":"broken"}`},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			db, _, ch := setupOpenRouterGateway(t, upstream.URL)
			task := &model.Task{TaskID: "task_test", Status: model.TaskStatusQueued, PrivateData: model.TaskPrivateData{UpstreamTaskID: "job-1", Key: "upstream-key"}}
			require.Error(t, service.RefreshVideoTask(context.Background(), &taskopenrouter.TaskAdaptor{}, ch, task))
			require.Equal(t, model.TaskStatus(model.TaskStatusQueued), task.Status)
			assertMiMoQuota(t, db, 0)
		})
	}
}
