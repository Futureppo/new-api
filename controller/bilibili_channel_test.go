package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/bilibili"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func rejectBilibiliMutation(t *testing.T, method string, body any, handler gin.HandlerFunc) {
	t.Helper()
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/channel/", bytes.NewReader(encoded))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("role", common.RoleRootUser)
	handler(c)
	require.True(t, gjson.ValidBytes(w.Body.Bytes()))
	require.False(t, gjson.GetBytes(w.Body.Bytes(), "success").Bool(), w.Body.String())
	require.NotEmpty(t, gjson.GetBytes(w.Body.Bytes(), "message").String())
}

func TestBilibiliChannelCredentialsAndDefaults(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	for i, key := range []string{"", "  ", " test-key "} {
		ch := model.Channel{Type: constant.ChannelTypeBilibili, Name: fmt.Sprint("bilibili-", i), Key: key, Models: bilibili.DefaultModel, Group: "default"}
		callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &ch}, AddChannel)
		var saved model.Channel
		require.NoError(t, db.Where("name = ?", ch.Name).First(&saved).Error)
		require.Equal(t, strings.TrimSpace(key), saved.Key)
		require.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeBilibili], saved.GetBaseURL())
		require.Equal(t, bilibili.DefaultModel, *saved.TestModel)
		var abilities int64
		require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", saved.Id).Count(&abilities).Error)
		require.EqualValues(t, 1, abilities)
		if saved.Key == "" {
			continue
		}
		// Both ordinary blank edits and partial edits must preserve the saved key.
		edit := map[string]any{"id": saved.Id, "name": saved.Name, "models": saved.Models, "group": "default", "key": ""}
		callKiloChannelMutation(t, http.MethodPut, edit, UpdateChannel)
		savedAgain, err := model.GetChannelById(saved.Id, true)
		require.NoError(t, err)
		require.Equal(t, saved.Key, savedAgain.Key)
		require.Equal(t, saved.GetBaseURL(), savedAgain.GetBaseURL())
		edit["clear_key"] = true
		edit["key"] = "conflicting-key"
		rejectBilibiliMutation(t, http.MethodPut, edit, UpdateChannel)
		delete(edit, "key")
		callKiloChannelMutation(t, http.MethodPut, edit, UpdateChannel)
		savedAgain, err = model.GetChannelById(saved.Id, true)
		require.NoError(t, err)
		require.Empty(t, savedAgain.Key)
		selected, _, apiErr := savedAgain.GetNextEnabledKey()
		require.Nil(t, apiErr)
		require.Empty(t, selected)
		edit["base_url"] = "ftp://invalid"
		rejectBilibiliMutation(t, http.MethodPut, edit, UpdateChannel)
	}
	ch := model.Channel{Type: constant.ChannelTypeBilibili, TestModel: common.GetPointer("custom-model")}
	require.NoError(t, validateChannel(&ch, true))
	require.Equal(t, "custom-model", *ch.TestModel)
}

func TestBilibiliBatchAndMultiKeyValidation(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	for _, mode := range []string{"batch", "multi_to_single"} {
		ch := model.Channel{Type: constant.ChannelTypeBilibili, Name: mode, Key: " \n ", Models: bilibili.DefaultModel, Group: "default"}
		rejectBilibiliMutation(t, http.MethodPost, AddChannelRequest{Mode: mode, Channel: &ch}, AddChannel)
		ch.Key = " key-a \n\n \n key-b "
		callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: mode, Channel: &ch}, AddChannel)
	}
	var batch []model.Channel
	require.NoError(t, db.Where("name = ?", "batch").Order("id").Find(&batch).Error)
	require.Len(t, batch, 2)
	require.Equal(t, "key-a", batch[0].Key)
	require.Equal(t, "key-b", batch[1].Key)
	var multi model.Channel
	require.NoError(t, db.Where("name = ?", "multi_to_single").First(&multi).Error)
	require.True(t, multi.ChannelInfo.IsMultiKey)
	require.Equal(t, []string{"key-a", "key-b"}, multi.GetKeys())
	rejectBilibiliMutation(t, http.MethodPut, map[string]any{"id": multi.Id, "clear_key": true}, UpdateChannel)
	require.Error(t, multi.UpdateWithKeyClear(true))
	callKiloChannelMutation(t, http.MethodPut, map[string]any{
		"id": multi.Id, "key_mode": "replace", "key": " key-c \n \n key-d ",
		"models": bilibili.DefaultModel, "group": "default",
	}, UpdateChannel)
	updated, err := model.GetChannelById(multi.Id, true)
	require.NoError(t, err)
	require.Equal(t, []string{"key-c", "key-d"}, updated.GetKeys())
	require.Equal(t, 2, updated.ChannelInfo.MultiKeySize)
	callKiloChannelMutation(t, http.MethodPut, map[string]any{
		"id": multi.Id, "key_mode": "append", "key": "key-d\n \n key-e",
		"models": bilibili.DefaultModel, "group": "default",
	}, UpdateChannel)
	updated, err = model.GetChannelById(multi.Id, true)
	require.NoError(t, err)
	require.Equal(t, []string{"key-c", "key-d", "key-e"}, updated.GetKeys())
}

func TestBilibiliModelDiscoveryAndSync(t *testing.T) {
	var expectedKey, expectedAgent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, []string{"/prefix/v1/models", "/custom"}, r.URL.Path)
		require.Equal(t, expectedKey, r.Header.Get("Authorization"))
		require.Equal(t, expectedAgent, r.Header.Get("User-Agent"))
		_, _ = io.WriteString(w, `{"data":[{"id":"Index-Translate-35B-A3B"},{"id":"future-model"}]}`)
	}))
	defer upstream.Close()
	db := openChannelRetryControllerTestDB(t)
	for _, key := range []string{"", "test-key"} {
		expectedKey, expectedAgent = "", bilibili.UserAgent
		if key != "" {
			expectedKey = "Bearer " + key
		}
		base := upstream.URL + "/prefix/v1/chat/completions"
		status, result := runFetchModelsRequest(t, map[string]any{"type": constant.ChannelTypeBilibili, "base_url": base, "key": key})
		require.Equal(t, http.StatusOK, status)
		require.True(t, result.Success)
		require.Equal(t, []string{bilibili.DefaultModel, "future-model"}, result.Data)
		ch := &model.Channel{Type: constant.ChannelTypeBilibili, Key: key, BaseURL: &base, Models: bilibili.DefaultModel}
		require.NoError(t, db.Create(ch).Error)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(ch.Id)}}
		FetchUpstreamModels(c)
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.True(t, result.Success, w.Body.String())
		add, remove, _, err := collectPendingUpstreamModelChanges(ch, dto.ChannelOtherSettings{})
		require.NoError(t, err)
		require.Equal(t, []string{"future-model"}, add)
		require.Empty(t, remove)
		expectedKey, expectedAgent = "Bearer custom", "Custom-Client"
		ch.HeaderOverride = common.GetPointer(`{"Authorization":"Bearer custom","User-Agent":"Custom-Client"}`)
		_, err = fetchChannelModelIDsWithKey(ch, base, key, upstream.URL+"/custom")
		require.NoError(t, err)
	}
	require.Equal(t, bilibili.ModelList, channelId2Models[constant.ChannelTypeBilibili])
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}, common.GetEndpointTypesByChannelType(constant.ChannelTypeBilibili, bilibili.DefaultModel))
}

func TestBilibiliGatewayAndChannelTest(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	var calls atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, bilibili.UserAgent, r.Header.Get("User-Agent"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, bilibili.DefaultModel, gjson.GetBytes(body, "model").String())
		require.Equal(t, "0", gjson.GetBytes(body, "temperature").Raw)
		require.Equal(t, "false", gjson.GetBytes(body, "chat_template_kwargs.enable_thinking").Raw)
		if fail.Load() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"translation rate limited","type":"rate_limit_error"}}`)
			return
		}
		response := `{"id":"chat1","model":"Index-Translate-35B-A3B","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":23,"completion_tokens":2,"total_tokens":25}}`
		responses := r.URL.Path == "/prefix/v1/responses"
		if responses {
			require.Contains(t, gjson.GetBytes(body, "input").String(), "Translate")
			response = `{"id":"resp1","object":"response","model":"Index-Translate-35B-A3B","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":23,"output_tokens":2,"total_tokens":25}}`
		} else {
			require.Equal(t, "/prefix/v1/chat/completions", r.URL.Path)
			require.Contains(t, gjson.GetBytes(body, "messages.0.content").String(), "Translate")
		}
		if gjson.GetBytes(body, "stream").Bool() {
			w.Header().Set("Content-Type", "text/event-stream")
			if responses {
				_, _ = io.WriteString(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":"+response+"}\n\n")
			} else {
				require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
				_, _ = io.WriteString(w, "data: {\"id\":\"chat1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"chat1\",\"choices\":[],\"usage\":{\"prompt_tokens\":23,\"completion_tokens\":2,\"total_tokens\":25}}\n\ndata: [DONE]\n\n")
			}
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, response)
		}
	}))
	defer upstream.Close()
	db, engine, ch := setupDecisionsGateway(t, upstream.URL+"/prefix/v1/")
	ch.Type, ch.Key, ch.Models = constant.ChannelTypeBilibili, "", "translation-alias"
	ch.ModelMapping = common.GetPointer(`{"translation-alias":"Index-Translate-35B-A3B"}`)
	ch.TestModel = common.GetPointer("translation-alias")
	require.NoError(t, ch.Save())
	require.NoError(t, ch.UpdateAbilities(db))
	// No built-in free pricing: the administrator must configure the site price.
	body := `{"model":"translation-alias","messages":[{"role":"user","content":"Translate to English: 你好"}]}`
	w := requestTypeSafe(t, engine, "/v1/chat/completions", body, typeSafeGatewayKey)
	require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	require.Zero(t, calls.Load())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"translation-alias":1}`))
	for _, stream := range []bool{false, true} {
		for _, responses := range []bool{false, true} {
			path, endpoint := "/v1/chat/completions", "openai"
			raw := `{"model":"translation-alias","messages":[{"role":"user","content":"Translate to English: 你好"}],"stream":%t,"stream_options":{"include_usage":true}}`
			if responses {
				path, endpoint = "/v1/responses", "openai-response"
				raw = `{"model":"translation-alias","input":"Translate to English: 你好","stream":%t}`
			}
			w = requestTypeSafe(t, engine, path, fmt.Sprintf(raw, stream), typeSafeGatewayKey)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "Hello")
			result := testChannel(ch, "translation-alias", endpoint, stream)
			require.NoError(t, result.localErr)
			require.Nil(t, result.newAPIError)
			require.Equal(t, 25, result.usage.TotalTokens)
		}
	}
	require.EqualValues(t, 8, calls.Load())
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 8)
	for _, log := range logs {
		require.Equal(t, "translation-alias", log.ModelName)
		require.Equal(t, 23, log.PromptTokens)
		require.Equal(t, 2, log.CompletionTokens)
		require.Positive(t, log.Quota)
	}
	fail.Store(true)
	w = requestTypeSafe(t, engine, "/v1/chat/completions", body, typeSafeGatewayKey)
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "translation rate limited")
}
