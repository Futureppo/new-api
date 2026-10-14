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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const qianwenAlias = "qianwen-test"

var qianwenEndpoints = []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"}

func setupQianwenGateway(t *testing.T, base, key string) (*gorm.DB, *gin.Engine, *model.Channel) {
	t.Helper()
	db, engine, ch := setupMiMoGateway(t, base, key)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	ch.Type, ch.Name, ch.Models = constant.ChannelTypeQianwenAI, "qianwen-test", qianwenAlias
	ch.TestModel = common.GetPointer(qianwenAlias)
	ch.ModelMapping = common.GetPointer(`{"qianwen-test":"qwen3.7-plus"}`)
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"qianwen-test":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"qianwen-test":2}`))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"qianwen-test":0.25}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{"qianwen-test":1.25}`))
	engine.POST("/v1/responses", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	engine.POST("/v1/responses/compact", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponsesCompaction) })
	engine.POST("/v1/embeddings", func(c *gin.Context) { Relay(c, types.RelayFormatEmbedding) })
	return db, engine, ch
}

func qianwenMockResponse(endpoint string, stream bool) string {
	if endpoint == "/v1/responses" {
		response := `{"id":"resp1","object":"response","status":"completed","model":"qwen3.7-plus","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110,"input_tokens_details":{"cached_tokens":40},"output_tokens_details":{"reasoning_tokens":3},"x_details":[{"x_billing_type":"response_api","input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":40,"cache_creation_input_tokens":20}}]}}`
		if !stream {
			return response
		}
		return "event: response.reasoning_text.delta\ndata: " + `{"type":"response.reasoning_text.delta","delta":"checking"}` + "\n\n" +
			"event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","delta":"OK"}` + "\n\n" +
			"event: response.completed\ndata: " + `{"type":"response.completed","response":` + response + "}\n\n"
	}
	response := strings.ReplaceAll(mimoMockResponse(endpoint == "/v1/messages", stream), "mimo-v2.6-pro-ultraspeed", qianwen.DefaultTestModel)
	if endpoint == "/v1/messages" {
		response = strings.ReplaceAll(response, `"input_tokens":60`, `"input_tokens":40`)
		response = strings.ReplaceAll(response, `"cache_read_input_tokens":40`, `"cache_read_input_tokens":40,"cache_creation_input_tokens":20`)
		// The platform reports complete cache usage only in message_delta.
		response = strings.ReplaceAll(response, `"output_tokens":0,"cache_read_input_tokens":40,"cache_creation_input_tokens":20`, `"output_tokens":0`)
	} else {
		response = strings.ReplaceAll(response, `"cached_tokens":40`, `"cached_tokens":40,"cached_creation_tokens":20`)
	}
	return response
}

func qianwenTestBody(endpoint string, stream bool) string {
	if endpoint == "/v1/responses" {
		return fmt.Sprintf(`{"model":"qianwen-test","input":[{"role":"user","content":"hi"}],"temperature":0,"max_output_tokens":0,"previous_response_id":"resp_previous","store":false,"reasoning":{"effort":"low"},"stream":%t}`, stream)
	}
	fields := `"enable_thinking":false,"preserve_thinking":false,"thinking_budget":0,"tool_stream":false,"search_options":{"forced_search":false}`
	if endpoint == "/v1/messages" {
		fields = `"thinking":{"type":"disabled"},"output_config":{"effort":"low"}`
	}
	return fmt.Sprintf(`{"model":"qianwen-test","messages":[{"role":"user","content":"hi"}],"max_tokens":0,"temperature":0,"top_p":0,%s,"stream":%t}`, fields, stream)
}

func TestQianwenGatewayProtocolsAndBilling(t *testing.T) {
	for _, endpoint := range qianwenEndpoints {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", endpoint, stream), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					wantPath := "/prefix/compatible-mode" + endpoint
					if endpoint == "/v1/messages" {
						wantPath = "/prefix/apps/anthropic/v1/messages"
						require.Equal(t, "upstream-key", r.Header.Get("x-api-key"))
						require.Empty(t, r.Header.Get("Authorization"))
						require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
					} else {
						require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
						require.Empty(t, r.Header.Get("x-api-key"))
					}
					require.Equal(t, wantPath, r.URL.Path)
					var body map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &body))
					require.Equal(t, qianwen.DefaultTestModel, body["model"])
					require.Equal(t, float64(0), body["temperature"])
					require.Equal(t, stream, body["stream"])
					require.Equal(t, "override", body["user"])
					if endpoint == "/v1/responses" {
						require.Equal(t, false, body["store"])
						require.Equal(t, "resp_previous", body["previous_response_id"])
						require.Equal(t, float64(0), body["max_output_tokens"])
					} else {
						require.Equal(t, float64(0), body["max_tokens"])
						require.Equal(t, float64(0), body["top_p"])
					}
					if endpoint == "/v1/chat/completions" {
						require.Equal(t, false, body["enable_thinking"])
						require.Equal(t, false, body["preserve_thinking"])
						require.Equal(t, float64(0), body["thinking_budget"])
						require.Equal(t, false, body["tool_stream"])
						if stream {
							require.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
						}
					}
					w.Header().Set("Content-Type", "application/json")
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
					}
					_, _ = io.WriteString(w, qianwenMockResponse(endpoint, stream))
				}))
				defer upstream.Close()
				db, engine, ch := setupQianwenGateway(t, upstream.URL+"/prefix/apps/anthropic/", "upstream-key")
				ch.ParamOverride = common.GetPointer(`{"user":"override"}`)
				require.NoError(t, ch.Update())
				body := qianwenTestBody(endpoint, stream)
				if stream && endpoint == "/v1/chat/completions" {
					body = strings.TrimSuffix(body, "}") + `,"stream_options":{"include_usage":true}}`
				}
				w := requestMiMo(t, engine, endpoint, body, mimoGatewayKey)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "OK")
				if stream {
					terminal := map[string]string{"/v1/chat/completions": "data: [DONE]", "/v1/messages": "event: message_stop", "/v1/responses": "event: response.completed"}[endpoint]
					require.Contains(t, w.Body.String(), terminal)
				}
				var logs []model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				require.Len(t, logs, 1)
				require.Equal(t, 10, logs[0].CompletionTokens)
				// 40 uncached + 40 * .25 cached + 20 * 1.25 cache creation + 10 * 2 output.
				require.Equal(t, 95, logs[0].Quota)
				assertMiMoQuota(t, db, logs[0].Quota)
			})
		}
	}
}

func TestQianwenGatewayUpstreamErrors(t *testing.T) {
	for _, status := range []int{400, 401, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"upstream unavailable","code":"test_error"}}`)
			}))
			defer upstream.Close()
			db, engine, _ := setupQianwenGateway(t, upstream.URL, "bad-key")
			for _, endpoint := range qianwenEndpoints {
				w := requestMiMo(t, engine, endpoint, qianwenTestBody(endpoint, false), mimoGatewayKey)
				require.Equal(t, status, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "upstream unavailable")
			}
			assertMiMoQuota(t, db, 0)
		})
	}
}

func TestQianwenGatewayUnsupportedRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unsupported request reached upstream") }))
	defer upstream.Close()
	db, engine, ch := setupQianwenGateway(t, upstream.URL, "key")
	ch.Models += "," + qianwenAlias + ratio_setting.CompactModelSuffix
	require.NoError(t, ch.Update())
	for _, pass := range []bool{false, true} {
		ch.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: pass, ShowErrorDetails: true})
		require.NoError(t, ch.Update())
		w := requestMiMo(t, engine, "/v1/responses", `{"model":"qianwen-test","input":"hi","background":true}`, mimoGatewayKey)
		require.Equal(t, 400, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "background")
	}
	for _, endpoint := range []string{"/v1/responses/compact"} {
		w := requestMiMo(t, engine, endpoint, `{"model":"qianwen-test","input":"hi"}`, mimoGatewayKey)
		require.Equal(t, 400, w.Code, w.Body.String())
	}
	assertMiMoQuota(t, db, 0)
}

func TestQianwenAdminChannelTests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, qianwen.DefaultTestModel, gjson.GetBytes(body, "model").String())
		endpoint := strings.TrimPrefix(r.URL.Path, "/compatible-mode")
		endpoint = strings.TrimPrefix(endpoint, "/apps/anthropic")
		stream := gjson.GetBytes(body, "stream").Bool()
		w.Header().Set("Content-Type", "application/json")
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
		}
		_, _ = io.WriteString(w, qianwenMockResponse(endpoint, stream))
	}))
	defer upstream.Close()
	_, _, ch := setupQianwenGateway(t, upstream.URL, "key")
	for _, endpoint := range []string{"", "openai", "anthropic", "openai-response"} {
		for _, stream := range []bool{false, true} {
			result := testChannel(ch, qianwenAlias, endpoint, stream)
			require.NoError(t, result.localErr)
			require.Nil(t, result.newAPIError, "%v", result.newAPIError)
			require.NotNil(t, result.usage)
			require.Equal(t, 10, result.usage.CompletionTokens)
		}
	}
}

func TestQianwenLiveGateway(t *testing.T) {
	key := os.Getenv("QIANWEN_API_KEY")
	if key == "" {
		t.Skip("set QIANWEN_API_KEY to verify the real upstream")
	}
	upstreamModel := os.Getenv("QIANWEN_TEST_MODEL")
	if upstreamModel == "" {
		upstreamModel = qianwen.DefaultTestModel
	}
	_, engine, ch := setupQianwenGateway(t, "https://maas.qianwenaiapi.com", key)
	mapping, err := common.Marshal(map[string]string{qianwenAlias: upstreamModel})
	require.NoError(t, err)
	ch.ModelMapping = common.GetPointer(string(mapping))
	require.NoError(t, ch.Update())
	names, err := fetchChannelModelIDsWithKey(ch, ch.GetBaseURL(), key, "")
	require.NoError(t, err)
	require.NotEmpty(t, names)
	for _, endpoint := range qianwenEndpoints {
		for _, stream := range []bool{false, true} {
			body := map[string]any{"model": qianwenAlias, "stream": stream}
			if endpoint == "/v1/responses" {
				body["input"], body["max_output_tokens"], body["reasoning"] = "Reply with exactly OK.", 256, map[string]string{"effort": "none"}
			} else {
				body["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly OK."}}
				body["max_tokens"] = 256
				if endpoint == "/v1/messages" {
					body["thinking"] = map[string]string{"type": "disabled"}
				} else {
					body["enable_thinking"] = false
				}
			}
			data, err := common.Marshal(body)
			require.NoError(t, err)
			w := requestMiMo(t, engine, endpoint, string(data), mimoGatewayKey)
			require.Equal(t, 200, w.Code, "endpoint=%s stream=%t: %.1000s", endpoint, stream, w.Body.String())
			require.Contains(t, w.Body.String(), "OK")
		}
	}
}

func TestQianwenGatewayPreservesToolAndVisionHistory(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aGx8AAAAASUVORK5CYII="
	for _, endpoint := range qianwenEndpoints {
		t.Run(endpoint, func(t *testing.T) {
			var fields string
			switch endpoint {
			case "/v1/chat/completions":
				fields = `"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + png + `"}},{"type":"text","text":"What is this?"}]},{"role":"assistant","content":"","reasoning_content":"retain reasoning","tool_calls":[{"id":"call1","type":"function","function":{"name":"echo","arguments":"{\"value\":0}"}}]},{"role":"tool","tool_call_id":"call1","content":"OK"}],"tools":[{"type":"function","function":{"name":"echo","parameters":{"type":"object","properties":{"value":{"type":"integer"}},"additionalProperties":false}}}],"response_format":{"type":"json_object"}`
			case "/v1/messages":
				fields = `"max_tokens":64,"system":[{"type":"text","text":"Return JSON","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + png + `"}}]},{"role":"assistant","content":[{"type":"thinking","thinking":"retain reasoning","signature":"sig"},{"type":"tool_use","id":"call1","name":"echo","input":{"value":0}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"OK","is_error":false}]}],"tools":[{"name":"echo","input_schema":{"type":"object","additionalProperties":false}}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object","additionalProperties":false}}}`
			case "/v1/responses":
				fields = `"conversation":"conv1","store":false,"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,` + png + `"},{"type":"input_text","text":"What is this?"}]},{"type":"reasoning","id":"r1","summary":[{"type":"summary_text","text":"retain reasoning"}]},{"type":"function_call","call_id":"call1","name":"echo","arguments":"{\"value\":0}"},{"type":"function_call_output","call_id":"call1","output":"OK"}],"tools":[{"type":"function","name":"echo","parameters":{"type":"object","additionalProperties":false}}],"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object","additionalProperties":false}}}`
			}
			var expected map[string]any
			require.NoError(t, common.UnmarshalJsonStr("{"+fields+"}", &expected))
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				for field, value := range expected {
					require.Equal(t, value, body[field], field)
				}
				require.NotContains(t, body, "enable_thinking")
				require.NotContains(t, body, "thinking")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, qianwenMockResponse(endpoint, false))
			}))
			defer upstream.Close()
			_, engine, _ := setupQianwenGateway(t, upstream.URL, "key")
			w := requestMiMo(t, engine, endpoint, `{"model":"qianwen-test",`+fields+`}`, mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
		})
	}
}

func TestQianwenGatewayIncompleteStreams(t *testing.T) {
	for _, endpoint := range qianwenEndpoints {
		for _, upstreamError := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/error=%t", endpoint, upstreamError), func(t *testing.T) {
				data := "data: " + `{"id":"chat1","choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\n"
				errorData := "data: " + `{"error":{"type":"server_error","message":"provider failed"}}` + "\n\n"
				terminal := "data: [DONE]"
				if endpoint == "/v1/messages" {
					data = "event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}` + "\n\n"
					errorData = "event: error\ndata: " + `{"type":"error","error":{"type":"api_error","message":"provider failed"}}` + "\n\n"
					terminal = "event: message_stop"
				} else if endpoint == "/v1/responses" {
					data = "event: response.output_text.delta\ndata: " + `{"type":"response.output_text.delta","delta":"partial"}` + "\n\n"
					errorData = "event: response.failed\ndata: " + `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"provider failed"}}}` + "\n\n"
					terminal = "event: response.completed"
				}
				if upstreamError {
					data += errorData
				}
				requests := 0
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, data)
				}))
				defer upstream.Close()
				db, engine, _ := setupQianwenGateway(t, upstream.URL, "key")
				w := requestMiMo(t, engine, endpoint, qianwenTestBody(endpoint, true), mimoGatewayKey)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "partial")
				require.NotContains(t, w.Body.String(), terminal)
				if upstreamError {
					require.Contains(t, w.Body.String(), "provider failed")
				}
				require.Equal(t, 1, requests)
				var log model.Log
				require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
				require.NotEqual(t, "ok", gjson.Get(log.Other, "stream_status.status").String(), log.Other)
			})
		}
	}
}

func TestQianwenResponsesFailedJSON(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"failed","error":{"code":"server_error","message":"provider failed"}}`)
	}))
	defer upstream.Close()
	db, engine, _ := setupQianwenGateway(t, upstream.URL, "key")
	w := requestMiMo(t, engine, "/v1/responses", qianwenTestBody("/v1/responses", false), mimoGatewayKey)
	require.Equal(t, 502, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "provider failed")
	assertMiMoQuota(t, db, 0)
}
