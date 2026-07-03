package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type enhancementAPIResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    map[string]interface{} `json:"data"`
}

func setupEnhancementOptionControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalOptionMap := common.OptionMap
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalRedisEnabled := common.RedisEnabled
	originalThreshold := setting.GetEnhancementSetting().ModelStatusRequestCountHideThreshold

	model.DB = db
	model.LOG_DB = db
	common.OptionMap = map[string]string{}
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	require.NoError(t, db.AutoMigrate(&model.Option{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.OptionMap = originalOptionMap
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		common.RedisEnabled = originalRedisEnabled
		setting.GetEnhancementSetting().ModelStatusRequestCountHideThreshold = originalThreshold
	})

	return db
}

func newEnhancementRouter() *gin.Engine {
	router := gin.New()
	group := router.Group("/api/enhancements")
	RegisterEnhancementRoutes(group)
	RegisterEnhancementRootRoutes(group)
	return router
}

func enhancementJSONRequest(t *testing.T, method string, target string, body any) *http.Request {
	t.Helper()

	payload, err := common.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeEnhancementAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) enhancementAPIResponse {
	t.Helper()

	var response enhancementAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestModelStatusRequestCountHideThresholdAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupEnhancementOptionControllerTestDB(t)
	router := newEnhancementRouter()

	saveRecorder := httptest.NewRecorder()
	router.ServeHTTP(saveRecorder, enhancementJSONRequest(
		t,
		http.MethodPut,
		"/api/enhancements/model-status/config/request-count-hide-threshold",
		map[string]interface{}{"value": 5},
	))
	require.Equal(t, http.StatusOK, saveRecorder.Code)
	saveResponse := decodeEnhancementAPIResponse(t, saveRecorder)
	require.True(t, saveResponse.Success)
	require.Equal(t, true, saveResponse.Data["saved"])
	require.Equal(t, 5, setting.GetEnhancementSetting().ModelStatusRequestCountHideThreshold)

	var option model.Option
	require.NoError(t, db.Where("key = ?", "enhancement_setting.model_status_request_count_hide_threshold").First(&option).Error)
	require.Equal(t, "5", option.Value)

	configRecorder := httptest.NewRecorder()
	router.ServeHTTP(configRecorder, httptest.NewRequest(
		http.MethodGet,
		"/api/enhancements/model-status/config/request-count-hide-threshold",
		nil,
	))
	require.Equal(t, http.StatusOK, configRecorder.Code)
	configResponse := decodeEnhancementAPIResponse(t, configRecorder)
	require.True(t, configResponse.Success)
	require.Equal(t, float64(5), configResponse.Data["model_status_request_count_hide_threshold"])
	require.NotContains(t, configResponse.Data, "show_zero_request_models")

	invalidRecorder := httptest.NewRecorder()
	router.ServeHTTP(invalidRecorder, enhancementJSONRequest(
		t,
		http.MethodPut,
		"/api/enhancements/model-status/config/request-count-hide-threshold",
		map[string]interface{}{"value": 1000001},
	))
	require.Equal(t, http.StatusOK, invalidRecorder.Code)
	invalidResponse := decodeEnhancementAPIResponse(t, invalidRecorder)
	require.False(t, invalidResponse.Success)
	require.Contains(t, invalidResponse.Message, "request count hide threshold")
	require.Equal(t, 5, setting.GetEnhancementSetting().ModelStatusRequestCountHideThreshold)
}
