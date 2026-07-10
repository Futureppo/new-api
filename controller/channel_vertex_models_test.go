package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fetchModelsTestResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    []string `json:"data"`
}

func runFetchModelsRequest(t *testing.T, payload map[string]any) (int, fetchModelsTestResponse) {
	t.Helper()
	body, err := common.Marshal(payload)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/fetch_models", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	FetchModels(ctx)
	var response fetchModelsTestResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return recorder.Code, response
}

func TestFetchModelsPreservesMultilineVertexServiceAccountJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	credential := "{\n  \"project_id\": \"test-project\",\n  \"private_key\": \"line-1\\nline-2\",\n  \"client_email\": \"test@example.com\"\n}"
	require.Equal(t, credential, normalizeFetchModelsKey(constant.ChannelTypeVertexAi, dto.VertexKeyTypeJSON, credential))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer custom-list-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"vertex-custom-model"}]}`))
	}))
	defer upstream.Close()
	headerOverride, err := common.Marshal(map[string]string{"Authorization": "Bearer custom-list-token"})
	require.NoError(t, err)

	status, response := runFetchModelsRequest(t, map[string]any{
		"type":                  constant.ChannelTypeVertexAi,
		"key":                   credential,
		"vertex_key_type":       dto.VertexKeyTypeJSON,
		"other":                 `{"default":"global"}`,
		"custom_model_list_url": upstream.URL,
		"header_override":       string(headerOverride),
	})
	require.Equal(t, http.StatusOK, status, response.Message)
	require.True(t, response.Success, response.Message)
	require.Equal(t, []string{"vertex-custom-model"}, response.Data)
}

func TestFetchModelsRejectsVertexAPIKeyWithoutCustomList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status, response := runFetchModelsRequest(t, map[string]any{
		"type":            constant.ChannelTypeVertexAi,
		"key":             "vertex-api-key",
		"vertex_key_type": dto.VertexKeyTypeAPIKey,
		"other":           `{"default":"global"}`,
	})
	require.Equal(t, http.StatusInternalServerError, status)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "不支持 API Key")
}

func TestFetchModelsAllowsVertexAPIKeyWithCustomList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer vertex-api-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":"custom-vertex-model"}]}`))
	}))
	defer upstream.Close()

	status, response := runFetchModelsRequest(t, map[string]any{
		"type":                  constant.ChannelTypeVertexAi,
		"key":                   "vertex-api-key",
		"vertex_key_type":       dto.VertexKeyTypeAPIKey,
		"other":                 `{"default":"global"}`,
		"custom_model_list_url": upstream.URL,
	})
	require.Equal(t, http.StatusOK, status)
	require.True(t, response.Success, response.Message)
	require.Equal(t, []string{"custom-vertex-model"}, response.Data)
}
