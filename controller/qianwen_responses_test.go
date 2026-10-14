package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestQianwenStoredResponseResources(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var calls atomic.Int32
			var deleteFails atomic.Bool
			deleteFails.Store(true)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "Bearer original-key", r.Header.Get("Authorization"))
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					io.WriteString(w, qianwenMockResponse("/v1/responses", stream))
					return
				}
				require.True(t, strings.HasPrefix(r.URL.Path, "/prefix/compatible-mode/v1/responses/resp1"))
				if strings.HasSuffix(r.URL.Path, "/input_items") {
					require.Equal(t, "msg_previous", r.URL.Query().Get("after"))
					require.Equal(t, "2", r.URL.Query().Get("limit"))
					require.Equal(t, "asc", r.URL.Query().Get("order"))
					io.WriteString(w, `{"object":"list","data":[{"id":"msg1","role":"user"}],"has_more":false}`)
				} else if r.Method == http.MethodDelete {
					if deleteFails.Load() {
						w.WriteHeader(429)
						io.WriteString(w, `{"error":{"message":"slow down"}}`)
					} else {
						io.WriteString(w, `{"id":"resp1","deleted":true}`)
					}
				} else {
					io.WriteString(w, `{"id":"resp1","output":[],"usage":{"input_tokens":100,"output_tokens":10}}`)
				}
			}))
			defer upstream.Close()
			db, engine, ch := setupQianwenGateway(t, upstream.URL+"/prefix/compatible-mode/v1", "original-key")
			w := requestMiMo(t, engine, "/v1/responses", fmt.Sprintf(`{"model":"qianwen-test","input":"hi","store":true,"stream":%t}`, stream), mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			var stored model.Task
			require.NoError(t, db.Where("platform = ?", service.QianwenResponsePlatform).First(&stored).Error)
			require.Equal(t, "resp1", stored.TaskID)
			require.Equal(t, "original-key", stored.PrivateData.Key)
			require.Empty(t, stored.Data)
			// Lifecycle requests keep the creation URL/key even after channel edits.
			ch.Key = "rotated-key"
			ch.BaseURL = common.GetPointer("https://new-host.invalid")
			require.NoError(t, ch.Update())
			resources := gin.New()
			resources.Use(middleware.TokenAuth(), middleware.ModelRequestRateLimit())
			resources.GET("/v1/responses/:response_id", QianwenResponseResource)
			resources.GET("/v1/responses/:response_id/input_items", QianwenResponseResource)
			resources.DELETE("/v1/responses/:response_id", QianwenResponseResource)
			request := func(method, path, key string) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, path, nil)
				r.Header.Set("Authorization", "Bearer "+key)
				w := httptest.NewRecorder()
				resources.ServeHTTP(w, r)
				return w
			}
			var log model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
			require.NoError(t, db.Create(&model.Token{Id: 2, UserId: 1, Name: "other token", Key: "otherqianwentoken", Status: common.TokenStatusEnabled, RemainQuota: 10000, ExpiredTime: -1}).Error)
			for _, method := range []string{http.MethodGet, http.MethodDelete} {
				w = request(method, "/v1/responses/resp1", "otherqianwentoken")
				require.Equal(t, 404, w.Code, w.Body.String())
			}
			require.Equal(t, int32(1), calls.Load())
			w = request(http.MethodGet, "/v1/responses/unknown", mimoGatewayKey)
			require.Equal(t, 404, w.Code)
			w = request(http.MethodGet, "/v1/responses/resp1/input_items?limit=0", mimoGatewayKey)
			require.Equal(t, 400, w.Code)
			w = request(http.MethodGet, "/v1/responses/resp1?stream=true", mimoGatewayKey)
			require.Equal(t, 400, w.Code)
			w = request(http.MethodGet, "/v1/responses/resp1", mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "resp1")
			w = request(http.MethodGet, "/v1/responses/resp1/input_items?after=msg_previous&limit=2&order=asc", mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			w = request(http.MethodDelete, "/v1/responses/resp1", mimoGatewayKey)
			require.Equal(t, 429, w.Code)
			require.Contains(t, w.Body.String(), "slow down")
			require.NoError(t, db.First(&stored, stored.ID).Error)
			deleteFails.Store(false)
			w = request(http.MethodDelete, "/v1/responses/resp1", mimoGatewayKey)
			require.Equal(t, 200, w.Code)
			w = request(http.MethodGet, "/v1/responses/resp1", mimoGatewayKey)
			require.Equal(t, 404, w.Code)
			assertMiMoQuota(t, db, log.Quota)
		})
	}
}

func TestQianwenStoreFalseAndExpiry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, qianwenMockResponse("/v1/responses", false))
	}))
	defer upstream.Close()
	db, engine, _ := setupQianwenGateway(t, upstream.URL, "key")
	w := requestMiMo(t, engine, "/v1/responses", `{"model":"qianwen-test","input":"hi","store":false}`, mimoGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	var n int64
	require.NoError(t, db.Model(&model.Task{}).Where("platform = ?", service.QianwenResponsePlatform).Count(&n).Error)
	require.Zero(t, n)
	w = requestMiMo(t, engine, "/v1/responses", `{"model":"qianwen-test","input":"hi","store":true}`, mimoGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	_, err := service.GetQianwenResponseRoute("resp1", 1, 1)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Task{}).Where("platform = ?", service.QianwenResponsePlatform).Update("finish_time", time.Now().Add(-8*24*time.Hour).Unix()).Error)
	_, err = service.GetQianwenResponseRoute("resp1", 1, 1)
	require.Error(t, err)
}
