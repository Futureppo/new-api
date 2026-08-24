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
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ConversationLog{}))

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

func TestUpdateChannelPreservesConversationLogSettingForNonRoot(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	autoBan := 1
	channel := model.Channel{
		Type:    1,
		Key:     "test-key",
		Status:  common.ChannelStatusEnabled,
		Name:    "conversation-log-test",
		Models:  "gpt-4o",
		Group:   "default",
		AutoBan: &autoBan,
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{ConversationLogEnabled: true})
	require.NoError(t, db.Create(&channel).Error)

	body := fmt.Sprintf(`{
		"id": %d,
		"type": 1,
		"key": "test-key",
		"status": %d,
		"name": "conversation-log-test",
		"models": "gpt-4o",
		"group": "default",
		"auto_ban": 1,
		"settings": "{\"conversation_log_enabled\":false}"
	}`, channel.Id, common.ChannelStatusEnabled)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/channel/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var reloaded model.Channel
	require.NoError(t, db.First(&reloaded, channel.Id).Error)
	require.True(t, reloaded.GetOtherSettings().ConversationLogEnabled)
}

func TestAddChannelClearsConversationLogSettingForNonRoot(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	body := `{
		"mode": "single",
		"channel": {
			"type": 1,
			"key": "test-key",
			"status": 1,
			"name": "conversation-log-add",
			"models": "gpt-4o",
			"group": "default",
			"auto_ban": 1,
			"settings": "{\"conversation_log_enabled\":true}"
		}
	}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	AddChannel(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var reloaded model.Channel
	require.NoError(t, db.Where("name = ?", "conversation-log-add").First(&reloaded).Error)
	require.False(t, reloaded.GetOtherSettings().ConversationLogEnabled)
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
	require.Equal(
		t,
		"https://ark.cn-beijing.volces.com/api/v3/models",
		resolveFetchModelsURL(constant.ChannelTypeVolcEngine, "https://ark.cn-beijing.volces.com", ""),
	)
	require.Equal(
		t,
		"https://ark.cn-beijing.volces.com/api/v3/models",
		resolveFetchModelsURL(constant.ChannelTypeVolcEngine, "https://ark.cn-beijing.volces.com/api/v3/", ""),
	)
	require.Equal(
		t,
		"https://ark.cn-beijing.volces.com/api/coding/v3/models",
		resolveFetchModelsURL(constant.ChannelTypeVolcEngine, "doubao-coding-plan", ""),
	)
	require.Equal(
		t,
		"https://api.poe.com/v1/models",
		resolveFetchModelsURL(constant.ChannelTypePoe, constant.ChannelBaseURLs[constant.ChannelTypePoe], ""),
	)
	require.Equal(
		t,
		"https://api.cerebras.ai/v1/models",
		resolveFetchModelsURL(constant.ChannelTypeCerebras, constant.ChannelBaseURLs[constant.ChannelTypeCerebras], ""),
	)
	require.Equal(
		t,
		"https://opencode.ai/zen/go/v1/models",
		resolveFetchModelsURL(constant.ChannelTypeOpenCodeGo, constant.ChannelBaseURLs[constant.ChannelTypeOpenCodeGo], ""),
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

func TestFetchMistralConsoleModelsUsesStaticList(t *testing.T) {
	channel := &model.Channel{
		Type: constant.ChannelTypeMistralConsole,
		Key:  "ory_session_test=\"session\"",
	}

	models, err := fetchChannelModelIDsWithKey(
		channel,
		constant.ChannelBaseURLs[constant.ChannelTypeMistralConsole],
		channel.Key,
		"",
	)
	require.NoError(t, err)
	require.Equal(t, []string{"glm-5-2"}, models)
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

func TestDashboardTaskViewsExposeErrorDetailsOnlyToRoot(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)

	hiddenChannel := model.Channel{Name: "hidden-errors", Key: "key", Models: "task-model", Group: "default"}
	hiddenChannel.SetSetting(dto.ChannelSettings{})
	require.NoError(t, db.Create(&hiddenChannel).Error)
	visibleChannel := model.Channel{Name: "visible-errors", Key: "key", Models: "task-model", Group: "default"}
	visibleChannel.SetSetting(dto.ChannelSettings{ShowErrorDetails: true})
	require.NoError(t, db.Create(&visibleChannel).Error)

	tasks := []*model.Task{
		{ChannelId: hiddenChannel.Id, Status: model.TaskStatusFailure, FailReason: "hidden provider detail", Data: []byte(`{"secret":true}`)},
		{ChannelId: visibleChannel.Id, Status: model.TaskStatusFailure, FailReason: "visible provider detail", Data: []byte(`{"secret":true}`)},
		{ChannelId: 999999, Status: model.TaskStatusFailure, FailReason: "deleted channel detail", Data: []byte(`{"secret":true}`)},
	}
	nonRootTasks := tasksToDto(tasks, false, false)
	for _, task := range nonRootTasks {
		require.Equal(t, dto.TaskFailureCode, task.FailReason)
		require.Nil(t, task.Data)
	}

	rootTasks := tasksToDto(tasks, false, true)
	require.Equal(t, "hidden provider detail", rootTasks[0].FailReason)
	require.NotNil(t, rootTasks[0].Data)
	require.Equal(t, "visible provider detail", rootTasks[1].FailReason)
	require.NotNil(t, rootTasks[1].Data)
	successTask := &model.Task{
		Status:     model.TaskStatusSuccess,
		FailReason: "",
		Properties: model.Properties{Input: "user input"},
		Data:       []byte(`{"result":"ok"}`),
	}
	nonRootSuccessTask := tasksToDto([]*model.Task{successTask}, false, false)[0]
	require.Equal(t, successTask.Properties, nonRootSuccessTask.Properties)
	require.Equal(t, successTask.Data, nonRootSuccessTask.Data)

	midjourneyTasks := []*model.Midjourney{
		{ChannelId: hiddenChannel.Id, Status: "FAILURE", FailReason: "hidden mj detail", Description: "hidden description", Properties: `{"secret":true}`, State: "secret state", ImageUrl: "https://secret.example/image", VideoUrl: "https://secret.example/video", VideoUrls: `["https://secret.example/video"]`, Buttons: "secret buttons"},
		{ChannelId: visibleChannel.Id, Status: "FAILURE", FailReason: "visible mj detail", Description: "visible description", Properties: `{"secret":true}`, State: "secret state", ImageUrl: "https://secret.example/image", VideoUrl: "https://secret.example/video", VideoUrls: `["https://secret.example/video"]`, Buttons: "secret buttons"},
	}
	nonRootMidjourneyTasks := midjourneyTasksForViewer(midjourneyTasks, false)
	for _, task := range nonRootMidjourneyTasks {
		require.Equal(t, dto.TaskFailureCode, task.FailReason)
		require.Equal(t, dto.TaskFailureCode, task.Description)
		require.Empty(t, task.Properties)
		require.Empty(t, task.State)
		require.Empty(t, task.ImageUrl)
		require.Empty(t, task.VideoUrl)
		require.Empty(t, task.VideoUrls)
		require.Empty(t, task.Buttons)
	}
	rootMidjourneyTasks := midjourneyTasksForViewer(midjourneyTasks, true)
	require.Equal(t, "hidden mj detail", rootMidjourneyTasks[0].FailReason)
	require.Equal(t, "visible mj detail", rootMidjourneyTasks[1].FailReason)
	require.Equal(t, "hidden mj detail", midjourneyTasks[0].FailReason)
	successMidjourneyTask := &model.Midjourney{Status: "SUCCESS", Description: "completed", Properties: `{"result":"ok"}`, ImageUrl: "https://example.com/image"}
	nonRootSuccessMidjourneyTask := midjourneyTasksForViewer([]*model.Midjourney{successMidjourneyTask}, false)[0]
	require.Equal(t, successMidjourneyTask.Description, nonRootSuccessMidjourneyTask.Description)
	require.Equal(t, successMidjourneyTask.Properties, nonRootSuccessMidjourneyTask.Properties)
	require.Equal(t, successMidjourneyTask.ImageUrl, nonRootSuccessMidjourneyTask.ImageUrl)
}

func TestLogViewsExposeErrorDetailsOnlyToRoot(t *testing.T) {
	logs := []*model.Log{{
		Type:    model.LogTypeError,
		Content: "provider endpoint and account leaked",
		Other:   `{"error_code":"invalid_api_key","admin_info":{"use_channel":[1,2]}}`,
	}, {
		Type:    model.LogTypeConsume,
		Content: "normal usage detail",
	}}

	nonRootLogs := logsForViewer(logs, common.RoleAdminUser)
	require.Equal(t, "invalid_api_key", nonRootLogs[0].Content)
	require.NotContains(t, nonRootLogs[0].Other, "admin_info")
	require.Equal(t, "provider endpoint and account leaked", logs[0].Content)
	require.Equal(t, "normal usage detail", nonRootLogs[1].Content)

	rootLogs := logsForViewer(logs, common.RoleRootUser)
	require.Equal(t, "provider endpoint and account leaked", rootLogs[0].Content)
	require.Contains(t, rootLogs[0].Other, "admin_info")
}
