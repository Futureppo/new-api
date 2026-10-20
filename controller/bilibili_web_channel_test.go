package controller

import (
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
	"github.com/QuantumNous/new-api/relay/channel/bilibili_web"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBilibiliWebChannelManagement(t *testing.T) {
	db := openChannelRetryControllerTestDB(t)
	for i, key := range []string{"", " test-key "} {
		ch := model.Channel{Type: constant.ChannelTypeBilibiliWeb, Name: fmt.Sprint("bilibili-web-", i), Key: key, Models: bilibili_web.DefaultModel, Group: "default"}
		ch.OtherSettings = `{"bilibili_web_source_language":"ja","bilibili_web_target_language":"ko"}`
		callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: "single", Channel: &ch}, AddChannel)
		var saved model.Channel
		require.NoError(t, db.Where("name = ?", ch.Name).First(&saved).Error)
		require.Equal(t, strings.TrimSpace(key), saved.Key)
		require.Equal(t, "https://index-translate.bilibili.com", saved.GetBaseURL())
		require.Equal(t, bilibili_web.DefaultModel, *saved.TestModel)
		edit := map[string]any{"id": saved.Id, "key": "", "name": saved.Name, "models": saved.Models, "group": "default"}
		callKiloChannelMutation(t, http.MethodPut, edit, UpdateChannel)
		updated, err := model.GetChannelById(saved.Id, true)
		require.NoError(t, err)
		require.Equal(t, saved.Key, updated.Key)
		require.Equal(t, saved.OtherSettings, updated.OtherSettings)
		edit["clear_key"] = true
		callKiloChannelMutation(t, http.MethodPut, edit, UpdateChannel)
		updated, err = model.GetChannelById(saved.Id, true)
		require.NoError(t, err)
		require.Empty(t, updated.Key)
		_, _, apiErr := updated.GetNextEnabledKey()
		require.Nil(t, apiErr)
		edit["settings"] = `{"bilibili_web_source_language":"en","bilibili_web_target_language":"en"}`
		rejectBilibiliMutation(t, http.MethodPut, edit, UpdateChannel)
		edit["settings"] = `invalid JSON`
		rejectBilibiliMutation(t, http.MethodPut, edit, UpdateChannel)
		updated, err = model.GetChannelById(saved.Id, true)
		require.NoError(t, err)
		require.Equal(t, saved.OtherSettings, updated.OtherSettings)
	}
	for _, mode := range []string{"batch", "multi_to_single"} {
		ch := model.Channel{Type: constant.ChannelTypeBilibiliWeb, Name: mode, Key: " \n ", Models: bilibili_web.DefaultModel, Group: "default"}
		rejectBilibiliMutation(t, http.MethodPost, AddChannelRequest{Mode: mode, Channel: &ch}, AddChannel)
		ch.Key = " key-a \n\n key-b "
		callKiloChannelMutation(t, http.MethodPost, AddChannelRequest{Mode: mode, Channel: &ch}, AddChannel)
	}
	var multi model.Channel
	require.NoError(t, db.Where("name = ?", "multi_to_single").First(&multi).Error)
	require.Equal(t, []string{"key-a", "key-b"}, multi.GetKeys())
	rejectBilibiliMutation(t, http.MethodPut, map[string]any{"id": multi.Id, "clear_key": true}, UpdateChannel)
	status, result := runFetchModelsRequest(t, map[string]any{"type": constant.ChannelTypeBilibiliWeb, "key": ""})
	require.Equal(t, http.StatusOK, status)
	require.True(t, result.Success)
	require.Equal(t, bilibili_web.ModelList, result.Data)
	// The maintained catalog must not depend on /v1/models or a live server.
	base := "http://unreachable.invalid"
	ch := &model.Channel{Type: constant.ChannelTypeBilibiliWeb, BaseURL: &base, Models: bilibili_web.DefaultModel}
	add, remove, _, err := collectPendingUpstreamModelChanges(ch, dto.ChannelOtherSettings{})
	require.NoError(t, err)
	require.Len(t, add, 4)
	require.Empty(t, remove)
	require.Equal(t, bilibili_web.ModelList, channelId2Models[constant.ChannelTypeBilibiliWeb])
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, common.GetEndpointTypesByChannelType(constant.ChannelTypeBilibiliWeb, "index-echo-2b-s2st"))
}

func TestBilibiliWebGatewayTextMappingOverridesAndFailure(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "/v1/chat/completions", r.URL.Query().Get("p"))
		require.Empty(t, r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, bilibili_web.DefaultModel, gjson.GetBytes(body, "model").String())
		require.Equal(t, "0.25", gjson.GetBytes(body, "temperature").Raw)
		require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\n")
		if fail.Load() {
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":23,\"completion_tokens\":2,\"total_tokens\":25}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	db, engine, ch := setupDecisionsGateway(t, server.URL)
	ch.Type, ch.Key, ch.Models = constant.ChannelTypeBilibiliWeb, "", "translation-alias"
	ch.ModelMapping = common.GetPointer(`{"translation-alias":"index-mt-9b-syllable"}`)
	ch.ParamOverride = common.GetPointer(`{"temperature":0.25}`)
	ch.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true})
	require.NoError(t, ch.Save())
	require.NoError(t, ch.UpdateAbilities(db))
	settings := model_setting.GetGlobalSettings()
	old := *settings
	settings.PassThroughRequestEnabled = true
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"*"}}
	t.Cleanup(func() { *settings = old })
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"translation-alias":1}`))
	for _, stream := range []bool{false, true} {
		for _, responses := range []bool{false, true} {
			path, endpoint := "/v1/chat/completions", "openai"
			raw := `{"model":"translation-alias","messages":[{"role":"user","content":"Translate 你好"}],"stream":%t,"stream_options":{"include_usage":true}}`
			if responses {
				path, endpoint = "/v1/responses", "openai-response"
				raw = `{"model":"translation-alias","input":"Translate 你好","stream":%t}`
			}
			w := requestTypeSafe(t, engine, path, fmt.Sprintf(raw, stream), typeSafeGatewayKey)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "Hello")
			require.Contains(t, w.Body.String(), "translation-alias")
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
		require.Equal(t, 23, log.PromptTokens)
		require.Equal(t, 2, log.CompletionTokens)
		require.Positive(t, log.Quota)
	}
	fail.Store(true)
	// Partial upstream SSE errors must refund and emit one SSE error, without a
	// trailing JSON object, success event, whole-request retry or consumption log.
	for _, responses := range []bool{false, true} {
		path, body := "/v1/chat/completions", `{"model":"translation-alias","messages":[{"role":"user","content":"hi"}],"stream":true}`
		if responses {
			path, body = "/v1/responses", `{"model":"translation-alias","input":"hi","stream":true}`
		}
		w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "data: [DONE]")
		require.NotContains(t, w.Body.String(), "response.completed")
		for _, line := range strings.Split(w.Body.String(), "\n") {
			require.True(t, line == "" || strings.HasPrefix(line, "event: ") || strings.HasPrefix(line, "data: "), line)
		}
	}
	require.EqualValues(t, 10, calls.Load())
	var count int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&count).Error)
	require.EqualValues(t, 8, count)
}

func TestBilibiliWebGatewayEchoPricingAndChannelTest(t *testing.T) {
	var submits atomic.Int32
	var fail atomic.Bool
	sample, err := common.Marshal(bilibili_web.EchoTestRequest("echo-alias"))
	require.NoError(t, err)
	audio := gjson.GetBytes(sample, "messages.0.content.0.input_audio.data").String()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			submits.Add(1)
			require.Equal(t, "/api/echo/s2st", r.URL.Query().Get("p"))
			require.NoError(t, r.ParseMultipartForm(1<<20))
			defer r.MultipartForm.RemoveAll()
			require.Equal(t, "2b", r.FormValue("model"))
			require.Equal(t, "en", r.FormValue("src"))
			require.Equal(t, "zh", r.FormValue("lang"))
			_, _ = io.WriteString(w, `{"ok":true,"job_id":"test"}`)
			return
		}
		require.Equal(t, "/api/echo/result?job_id=test", r.URL.Query().Get("p"))
		if fail.Load() {
			_, _ = io.WriteString(w, `{"ok":true,"status":"done","echo":{"ok":false,"error":"unavailable"}}`)
		} else {
			_, _ = fmt.Fprintf(w, `{"ok":true,"status":"done","echo":{"ok":true,"text":"你好","audio_b64":%q}}`, audio)
		}
	}))
	defer server.Close()
	db, engine, ch := setupDecisionsGateway(t, server.URL)
	ch.Type, ch.Key, ch.Models = constant.ChannelTypeBilibiliWeb, "", "echo-alias"
	ch.ModelMapping = common.GetPointer(`{"echo-alias":"index-echo-2b-s2st"}`)
	require.NoError(t, ch.Save())
	require.NoError(t, ch.UpdateAbilities(db))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"echo-alias":1}`))
	w := requestTypeSafe(t, engine, "/v1/chat/completions", string(sample), typeSafeGatewayKey)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "per-request pricing")
	require.Zero(t, submits.Load())
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"echo-alias":0.01}`))
	w = requestTypeSafe(t, engine, "/v1/chat/completions", string(sample), typeSafeGatewayKey)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, audio, gjson.Get(w.Body.String(), "choices.0.message.audio.data").String())
	require.False(t, gjson.Get(w.Body.String(), "usage").Exists())
	assertTypeSafeQuota(t, db, int(common.QuotaPerUnit*.01))
	result := testChannel(ch, "echo-alias", "openai", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.Zero(t, result.usage.TotalTokens)
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Zero(t, log.PromptTokens)
		require.Zero(t, log.CompletionTokens)
		require.Equal(t, int(common.QuotaPerUnit*.01), log.Quota)
		require.False(t, log.IsStream)
	}
	fail.Store(true)
	w = requestTypeSafe(t, engine, "/v1/chat/completions", string(sample), typeSafeGatewayKey)
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	assertTypeSafeQuota(t, db, int(common.QuotaPerUnit*.01))
	require.EqualValues(t, 3, submits.Load())
	result = testChannel(ch, "echo-alias", "openai-response", false)
	require.Error(t, result.localErr)
}
