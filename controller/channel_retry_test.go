package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openChannelRetryControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.MemoryCacheEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestUpdateChannelClearsRetryTimes(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	retryTimes := 2
	autoBan := 1
	channel := model.Channel{
		Type:       1,
		Key:        "test-key",
		Status:     common.ChannelStatusEnabled,
		Name:       "retry-test",
		Models:     "gpt-4o",
		Group:      "default",
		AutoBan:    &autoBan,
		RetryTimes: &retryTimes,
	}
	require.NoError(t, db.Create(&channel).Error)

	body := fmt.Sprintf(`{
		"id": %d,
		"type": 1,
		"key": "test-key",
		"status": %d,
		"name": "retry-test",
		"models": "gpt-4o",
		"group": "default",
		"auto_ban": 1,
		"retry_times": null
	}`, channel.Id, common.ChannelStatusEnabled)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Nil(t, reloaded.RetryTimes)
}

func TestUpdateChannelClearsDailySuccessLimit(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	autoBan := 1
	channel := model.Channel{
		Type:              1,
		Key:               "test-key",
		Status:            common.ChannelStatusEnabled,
		Name:              "daily-limit-test",
		Models:            "gpt-4o",
		Group:             "default",
		AutoBan:           &autoBan,
		DailySuccessLimit: 5,
		DailySuccessCount: 3,
		DailySuccessDate:  "2026-04-29",
	}
	require.NoError(t, db.Create(&channel).Error)

	body := fmt.Sprintf(`{
		"id": %d,
		"type": 1,
		"key": "test-key",
		"status": %d,
		"name": "daily-limit-test",
		"models": "gpt-4o",
		"group": "default",
		"auto_ban": 1,
		"daily_success_limit": 0
	}`, channel.Id, common.ChannelStatusEnabled)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.Equal(t, 0, reloaded.DailySuccessLimit)
	require.Equal(t, 3, reloaded.DailySuccessCount)
	require.Equal(t, "2026-04-29", reloaded.DailySuccessDate)
}

func TestResolveFetchModelsURL(t *testing.T) {
	require.Equal(
		t,
		"https://api.example.com/v1/models",
		resolveFetchModelsURL(constant.ChannelTypeOpenAI, "https://api.example.com/", ""),
	)
	require.Equal(
		t,
		"https://api.kilo.ai/api/gateway/models",
		resolveFetchModelsURL(constant.ChannelTypeOpenAI, "https://api.example.com", " https://api.kilo.ai/api/gateway/models "),
	)
	require.Equal(
		t,
		"https://dashscope.aliyuncs.com/compatible-mode/v1/models",
		resolveFetchModelsURL(constant.ChannelTypeAli, "https://dashscope.aliyuncs.com", ""),
	)
}

func TestFetchModelsUsesCustomModelListURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/gateway/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"kilo-auto/frontier"},{"id":"kilo-auto/balanced"}]}`))
	}))
	defer upstream.Close()

	body := fmt.Sprintf(`{
		"type": %d,
		"key": "test-key",
		"base_url": "https://unused.example.com",
		"custom_model_list_url": %q
	}`, constant.ChannelTypeOpenAI, upstream.URL+"/api/gateway/models")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	FetchModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)
	require.Equal(t, []string{"kilo-auto/frontier", "kilo-auto/balanced"}, resp.Data)
}

func TestFetchModelsParsesCohereCustomModelListURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/cohere/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"models":[{"name":"command-a-03-2025"},{"name":"embed-v4.0"}]}`))
	}))
	defer upstream.Close()

	body := fmt.Sprintf(`{
		"type": %d,
		"key": "test-key",
		"base_url": "https://unused.example.com",
		"custom_model_list_url": %q
	}`, constant.ChannelTypeCohere, upstream.URL+"/cohere/models")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	FetchModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)
	require.Equal(t, []string{"command-a-03-2025", "embed-v4.0"}, resp.Data)
}

func TestFetchCohereModelsPaginatesEndpointsAndSorts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		require.Equal(t, "1000", r.URL.Query().Get("page_size"))

		switch r.URL.Query().Get("endpoint") + ":" + r.URL.Query().Get("page_token") {
		case "chat:":
			_, _ = w.Write([]byte(`{"models":[{"name":"command-r-plus"}],"next_page_token":"next-chat"}`))
		case "chat:next-chat":
			_, _ = w.Write([]byte(`{"models":[{"name":"command-a-03-2025"}]}`))
		case "rerank:":
			_, _ = w.Write([]byte(`{"models":[{"name":"rerank-v4.0"}]}`))
		case "embed:":
			_, _ = w.Write([]byte(`{"models":[{"name":"embed-v4.0"},{"name":"command-a-03-2025"}]}`))
		default:
			t.Fatalf("unexpected cohere models query: %s", r.URL.RawQuery)
		}
	}))
	defer upstream.Close()

	channel := &model.Channel{
		Type: constant.ChannelTypeCohere,
		Key:  "test-key",
	}

	models, err := fetchChannelModelIDsWithKey(channel, upstream.URL, "test-key", "")
	require.NoError(t, err)
	require.Equal(t, []string{"command-a-03-2025", "command-r-plus", "embed-v4.0", "rerank-v4.0"}, models)
}

func TestFetchUpstreamModelsUsesSavedCustomModelListURL(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/custom/models", r.URL.Path)
		require.Equal(t, "Bearer saved-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"custom/model-a"},{"id":"custom/model-b"}]}`))
	}))
	defer upstream.Close()

	settingsBytes, err := common.Marshal(dto.ChannelOtherSettings{
		CustomModelListURL: upstream.URL + "/custom/models",
	})
	require.NoError(t, err)

	channel := model.Channel{
		Type:          constant.ChannelTypeOpenAI,
		Key:           "saved-key",
		Status:        common.ChannelStatusEnabled,
		Name:          "custom-model-list",
		Models:        "placeholder",
		Group:         "default",
		OtherSettings: string(settingsBytes),
	}
	require.NoError(t, db.Create(&channel).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channel.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/fetch_models/"+strconv.Itoa(channel.Id), nil)

	FetchUpstreamModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Success bool     `json:"success"`
		Message string   `json:"message"`
		Data    []string `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &resp))
	require.True(t, resp.Success, resp.Message)
	require.Equal(t, []string{"custom/model-a", "custom/model-b"}, resp.Data)
}
