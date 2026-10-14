package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/qianwen"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const intentToolsPrompt = "You are Qwen, created by Alibaba Cloud. You are a helpful assistant. You may call one or more tools to assist with the user query. The tools you can use are as follows:\n[{\"name\":\"get_current_weather\",\"description\":\"查询城市天气\",\"parameters\":{\"type\":\"object\",\"properties\":{\"location\":{\"type\":\"string\"}},\"required\":[\"location\"]}}]\n"
const intentClassificationPrompt = "You are Qwen, created by Alibaba Cloud. You are a helpful assistant.\nYou should choose one tag from the tag list:\n{\"alarm_set\":\"设置闹钟\",\"weather\":\"查询天气\"}\nJust reply with the chosen tag."

func TestQianwenIntentModesAndBilling(t *testing.T) {
	for _, mode := range []struct{ name, prompt, output string }{
		{"tags", intentClassificationPrompt, "alarm_set"},
		{"intent-tools", intentToolsPrompt + "Response in INTENT_MODE.", "<tags>\n[function call, json response]\n</tags><tool_call>\n[{\"name\":\"get_current_weather\",\"arguments\":{\"location\":\"杭州\"}}]\n</tool_call><content>\n\n</content>"},
		{"tools", intentToolsPrompt + "Response in NORMAL_MODE.", "<tool_call>\n{\"name\":\"get_current_weather\",\"arguments\":{\"location\":\"杭州\"}}\n</tool_call>"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode.name, stream), func(t *testing.T) {
				messages := []dto.Message{{Role: "system", Content: mode.prompt}, {Role: "user", Content: "我想查天气"}, {Role: "assistant", Content: "<tags>\n[weather inquiry]\n</tags><tool_call>\n[]\n</tool_call><content>\n请问哪个城市？\n</content>"}, {Role: "user", Content: "杭州"}}
				if mode.name == "tags" {
					messages = []dto.Message{{Role: "system", Content: mode.prompt}, {Role: "user", Content: "星期五早上九点叫醒我"}}
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "/prefix/compatible-mode/v1/chat/completions", r.URL.Path)
					require.Equal(t, "Bearer intent-key", r.Header.Get("Authorization"))
					var request dto.GeneralOpenAIRequest
					require.NoError(t, common.DecodeJson(r.Body, &request))
					require.Equal(t, constant.QianwenIntentModel, request.Model)
					require.Equal(t, messages, request.Messages)
					require.NotNil(t, request.Temperature)
					require.Zero(t, *request.Temperature)
					require.EqualValues(t, 64, *request.MaxTokens)
					content, err := common.Marshal(mode.output)
					require.NoError(t, err)
					response := strings.ReplaceAll(qianwenMockResponse("/v1/chat/completions", stream), `"OK"`, string(content))
					response = strings.ReplaceAll(response, qianwen.DefaultTestModel, constant.QianwenIntentModel)
					w.Header().Set("Content-Type", "application/json")
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					io.WriteString(w, response)
				}))
				defer upstream.Close()
				db, engine, ch := setupQianwenGateway(t, upstream.URL+"/prefix/api/v1/services/aigc/text-generation/generation", "intent-key")
				ch.ModelMapping = common.GetPointer(`{"qianwen-test":"tongyi-intent-detect-v3"}`)
				ch.ParamOverride = common.GetPointer(`{"max_tokens":64}`)
				require.NoError(t, ch.Update())
				body, err := common.Marshal(map[string]any{"model": qianwenAlias, "messages": messages, "stream": stream, "temperature": 0})
				require.NoError(t, err)
				w := requestMiMo(t, engine, "/v1/chat/completions", string(body), mimoGatewayKey)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				if stream {
					var output strings.Builder
					for _, line := range strings.Split(w.Body.String(), "\n") {
						if strings.HasPrefix(line, "data: {") {
							output.WriteString(gjson.Get(strings.TrimPrefix(line, "data: "), "choices.0.delta.content").String())
						}
					}
					require.Equal(t, mode.output, output.String())
					require.Equal(t, 1, strings.Count(w.Body.String(), "data: [DONE]"))
				} else {
					require.Equal(t, mode.output, gjson.Get(w.Body.String(), "choices.0.message.content").String())
				}
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				require.Equal(t, 95, logs[0].Quota)
				assertMiMoQuota(t, db, 95)
			})
		}
	}
}

func TestQianwenIntentChannelTestAndCapabilities(t *testing.T) {
	require.Contains(t, qianwen.ModelList, constant.QianwenIntentModel)
	require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, common.GetEndpointTypesByChannelType(constant.ChannelTypeQianwenAI, constant.QianwenIntentModel))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, constant.QianwenIntentModel, gjson.GetBytes(data, "model").String())
		require.Contains(t, gjson.GetBytes(data, "messages.0.content").String(), "Just reply with the chosen tag.")
		require.Equal(t, "你好", gjson.GetBytes(data, "messages.1.content").String())
		stream := gjson.GetBytes(data, "stream").Bool()
		w.Header().Set("Content-Type", "application/json")
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		io.WriteString(w, strings.ReplaceAll(qianwenMockResponse("/v1/chat/completions", stream), "OK", "greeting"))
	}))
	defer upstream.Close()
	_, _, ch := setupQianwenGateway(t, upstream.URL, "key")
	// Follow chained mappings before choosing the smoke-test payload.
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"intent-alias","intent-alias":"tongyi-intent-detect-v3"}`)
	require.NoError(t, ch.Update())
	for _, endpoint := range []string{"", "openai"} {
		for _, stream := range []bool{false, true} {
			result := testChannel(ch, qianwenAlias, endpoint, stream)
			require.NoError(t, result.localErr)
			require.Nil(t, result.newAPIError, "%v", result.newAPIError)
			require.NotNil(t, result.usage)
		}
	}
}

func TestQianwenIntentRejectsOtherProtocols(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unsupported protocol reached upstream") }))
	defer upstream.Close()
	db, engine, ch := setupQianwenGateway(t, upstream.URL, "key")
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"tongyi-intent-detect-v3"}`)
	require.NoError(t, ch.Update())
	for _, endpoint := range []string{"/v1/messages", "/v1/responses"} {
		w := requestMiMo(t, engine, endpoint, qianwenTestBody(endpoint, false), mimoGatewayKey)
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "/v1/chat/completions")
	}
	assertMiMoQuota(t, db, 0)
}

func TestQianwenIntentLive(t *testing.T) {
	key := os.Getenv("QIANWEN_API_KEY")
	if key == "" {
		t.Skip("set QIANWEN_API_KEY to verify the real intent model")
	}
	_, engine, ch := setupQianwenGateway(t, "https://maas.qianwenaiapi.com", key)
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"tongyi-intent-detect-v3"}`)
	require.NoError(t, ch.Update())
	body, err := common.Marshal(map[string]any{"model": qianwenAlias, "messages": []dto.Message{{Role: "system", Content: intentClassificationPrompt}, {Role: "user", Content: "星期五早上九点叫醒我"}}})
	require.NoError(t, err)
	w := requestMiMo(t, engine, "/v1/chat/completions", string(body), mimoGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "alarm_set", strings.TrimSpace(gjson.Get(w.Body.String(), "choices.0.message.content").String()))
}
