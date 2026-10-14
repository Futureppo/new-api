package controller

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	vertextask "github.com/QuantumNous/new-api/relay/channel/task/vertex"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type vertexTestTransport func(*http.Request) (*http.Response, error)

func (f vertexTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func configureVertexTestChannel(t *testing.T, ch *model.Channel, models string, keyType dto.VertexKeyType) {
	ch.Type = constant.ChannelTypeVertexAi
	ch.Models = models
	ch.Other = `{"default":"global"}`
	ch.SetOtherSettings(dto.ChannelOtherSettings{VertexKeyType: keyType})
	require.NoError(t, ch.Update())
}

func TestVertexTokenToolsBypassGenerationBilling(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.True(t, strings.HasSuffix(r.URL.Path, "/publishers/google/models/gemini-2.5-flash:countTokens"))
		require.Equal(t, "key", r.URL.Query().Get("key"))
		_, _ = io.WriteString(w, `{"totalTokens":19,"totalBillableCharacters":55}`)
	}))
	defer server.Close()
	db, engine, ch := setupTypeSafeGateway(t, server.URL, "key")
	configureVertexTestChannel(t, ch, "alias", dto.VertexKeyTypeAPIKey)
	ch.ModelMapping = common.GetPointer(`{"alias":"gemini-2.5-flash"}`)
	require.NoError(t, ch.Save())
	engine.POST("/v1/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
	w := requestTypeSafe(t, engine, "/v1/models/alias:countTokens", `{"contents":[{"parts":[{"text":"hello"}]}]}`, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"totalTokens":19`)
	assertTypeSafeQuota(t, db, 0)
	require.Equal(t, 1, calls)
	// No model prices are configured. Unsupported actions fail locally and an
	// unauthenticated request never reaches Google.
	w = requestTypeSafe(t, engine, "/v1/models/alias:countTokens", `{}`, "wrong-key")
	require.NotEqual(t, 200, w.Code)
	require.Equal(t, 1, calls)
	w = requestTypeSafe(t, engine, "/v1/models/alias:computeTokens", `{}`, typeSafeGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	require.Equal(t, 1, calls)
	assertTypeSafeQuota(t, db, 0)
}

func TestVertexEmbeddingPartialFailureRefundsAndDoesNotRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "Bearer vertex-mock-token", r.Header.Get("Authorization"))
		if calls == 2 {
			w.WriteHeader(500)
			_, _ = io.WriteString(w, `{"error":{"message":"temporary failure"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"embedding":{"values":[1,2]},"usageMetadata":{"promptTokenCount":2,"totalTokenCount":2}}`)
	}))
	defer server.Close()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	creds := vertexcore.Credentials{ProjectID: "mock-project", ClientEmail: "mock@example.iam.gserviceaccount.com", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))}
	key, err := common.Marshal(creds)
	require.NoError(t, err)
	db, engine, ch := setupTypeSafeGateway(t, server.URL, string(key))
	configureVertexTestChannel(t, ch, "embedding-alias", dto.VertexKeyTypeJSON)
	ch.ModelMapping = common.GetPointer(`{"embedding-alias":"gemini-embedding-2"}`)
	require.NoError(t, ch.Save())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"embedding-alias":1}`))
	common.RetryTimes = 3
	client := service.GetHttpClient()
	original := client.Transport
	client.Transport = vertexTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "oauth2.googleapis.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"vertex-mock-token","expires_in":3600}`))}, nil
		}
		return original.RoundTrip(r)
	})
	t.Cleanup(func() { client.Transport = original })
	engine.POST("/v1/embeddings", func(c *gin.Context) { Relay(c, types.RelayFormatEmbedding) })
	w := requestTypeSafe(t, engine, "/v1/embeddings", `{"model":"embedding-alias","input":["one","two","three"]}`, typeSafeGatewayKey)
	require.NotEqual(t, 200, w.Code, w.Body.String())
	require.Equal(t, 2, calls)
	require.NotContains(t, w.Body.String(), `"object":"list"`)
	assertTypeSafeQuota(t, db, 0)
}

func TestVertexOperationOwnershipAndSingleSettlement(t *testing.T) {
	db, _, ch := setupTypeSafeGateway(t, "https://example.invalid", "unused")
	configureVertexTestChannel(t, ch, "veo-3.1-generate-preview", dto.VertexKeyTypeJSON)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"quota": 98000, "used_quota": 2000}).Error)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"remain_quota": 98000, "used_quota": 2000}).Error)
	task := &model.Task{TaskID: "task_vertex_owner", UserId: 1, ChannelId: ch.Id, Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVertexAi)), Status: model.TaskStatusInProgress, Quota: 2000, PrivateData: model.TaskPrivateData{TokenId: 1, BillingSource: service.BillingSourceWallet, UpstreamTaskID: "private-upstream-id", BillingContext: &model.TaskBillingContext{PerCallBilling: true, OtherRatios: map[string]float64{"n": 2}}}}
	require.NoError(t, task.Insert())
	data := []byte(`{"name":"private-operation","done":true,"response":{"videos":[{"bytesBase64Encoded":"AQID","mimeType":"video/mp4"}],"raiMediaFilteredCount":1}}`)
	a := &vertextask.TaskAdaptor{}
	stale := *task
	require.NoError(t, service.ApplyTaskPollingResult(context.Background(), a, ch, task, data, 200))
	require.NoError(t, service.ApplyTaskPollingResult(context.Background(), a, ch, &stale, data, 200))
	assertTypeSafeQuota(t, db, 1000)
	c, w := vertexOperationTestContext(2)
	VertexOperationFetch(c)
	require.Equal(t, 404, w.Code)
	c, w = vertexOperationTestContext(1)
	VertexOperationFetch(c)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"name":"operations/task_vertex_owner"`)
	require.Contains(t, w.Body.String(), `"bytesBase64Encoded":"AQID"`)
	require.NotContains(t, w.Body.String(), "private-operation")
	assertTypeSafeQuota(t, db, 1000)
}
func vertexOperationTestContext(user int) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", user)
	c.Request = httptest.NewRequest("POST", "/v1/models/veo-3.1-generate-preview:fetchPredictOperation", strings.NewReader(`{"operationName":"operations/task_vertex_owner"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func TestVertexChannelTestRegistry(t *testing.T) {
	ch := &model.Channel{Type: constant.ChannelTypeVertexAi}
	require.Equal(t, "embeddings", normalizeChannelTestEndpoint(ch, "gemini-embedding-001", ""))
	require.Equal(t, "vertex-native", normalizeChannelTestEndpoint(ch, "lyria-002", ""))
	require.Equal(t, "audio-speech", normalizeChannelTestEndpoint(ch, "gemini-2.5-flash-tts", ""))
	request := buildTestRequest("mistralai/codestral-2", "vertex-native", ch, false).(*dto.VertexRequest)
	require.Equal(t, "rawPredict", request.Action)
	require.Contains(t, string(request.Fields["prompt"]), "def add")
	request2 := buildTestRequest("gemini-2.5-flash-tts", "audio-speech", ch, false).(*dto.AudioRequest)
	require.Equal(t, "wav", request2.ResponseFormat)
	require.Equal(t, "gs://bucket/second.mp4", extractVertexVideoAtIndex([]byte(`{"response":{"videos":[{"bytesBase64Encoded":"AQID"},{"gcsUri":"gs://bucket/second.mp4"}]}}`), 1))
	require.Empty(t, extractVertexVideoAtIndex([]byte(`{"response":{"videos":[]}}`), 1))
}

func TestVertexNativeVideoSubmitAndFetch(t *testing.T) {
	operation := "projects/mock-project/locations/global/publishers/google/models/veo-3.1-generate-preview/operations/op1"
	submits, fetches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer vertex-video-token", r.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		if strings.HasSuffix(r.URL.Path, ":predictLongRunning") {
			submits++
			p := body["parameters"].(map[string]any)
			require.Equal(t, 2.0, p["sampleCount"])
			require.Equal(t, 6.0, p["durationSeconds"])
			data, _ := common.Marshal(gin.H{"name": operation})
			_, _ = w.Write(data)
			return
		}
		fetches++
		require.Equal(t, operation, body["operationName"])
		_, _ = io.WriteString(w, `{"done":true,"response":{"videos":[{"bytesBase64Encoded":"AQID","mimeType":"video/mp4"},{"gcsUri":"gs://bucket/second.mp4","mimeType":"video/mp4"}]}}`)
	}))
	defer server.Close()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	key, err := common.Marshal(vertexcore.Credentials{ProjectID: "mock-project", ClientEmail: "video@example.iam.gserviceaccount.com", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))})
	require.NoError(t, err)
	db, engine, ch := setupTypeSafeGateway(t, server.URL, string(key))
	configureVertexTestChannel(t, ch, "veo-alias", dto.VertexKeyTypeJSON)
	ch.ModelMapping = common.GetPointer(`{"veo-alias":"veo-3.1-generate-preview"}`)
	require.NoError(t, ch.Save())
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"veo-alias":0.001}`))
	client := service.GetHttpClient()
	original := client.Transport
	client.Transport = vertexTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "oauth2.googleapis.com" {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"vertex-video-token","expires_in":3600}`))}, nil
		}
		return original.RoundTrip(r)
	})
	t.Cleanup(func() { client.Transport = original })
	engine.POST("/v1/models/*path", func(c *gin.Context) { Relay(c, types.RelayFormatGemini) })
	w := requestTypeSafe(t, engine, "/v1/models/veo-alias:predictLongRunning", `{"instances":[{"prompt":"cat"}],"parameters":{"durationSeconds":6,"sampleCount":2}}`, typeSafeGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	var submitted struct {
		Name string `json:"name"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &submitted))
	require.True(t, strings.HasPrefix(submitted.Name, "operations/task_"))
	task, exists, err := model.GetByTaskId(1, strings.TrimPrefix(submitted.Name, "operations/"))
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, string(key), task.PrivateData.Key)
	require.Equal(t, 6000, task.Quota)
	query, _ := common.Marshal(gin.H{"operationName": submitted.Name})
	for i := 0; i < 2; i++ {
		w = requestTypeSafe(t, engine, "/v1/models/veo-alias:fetchPredictOperation", string(query), typeSafeGatewayKey)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "second.mp4")
	}
	require.Equal(t, 1, submits)
	require.Equal(t, 1, fetches)
	assertTypeSafeQuota(t, db, 6000)
}
