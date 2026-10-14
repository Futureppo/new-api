package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOpenRouterGateway(t *testing.T, base string) (*gorm.DB, *gin.Engine, *model.Channel) {
	db, engine, ch := setupMiMoGateway(t, base, "upstream-key")
	ch.Type, ch.Name, ch.Models = constant.ChannelTypeOpenRouter, "openrouter-test", "or-test"
	ch.ModelMapping = common.GetPointer(`{"or-test":"vendor/mapped"}`)
	ch.ParamOverride = common.GetPointer(`{"user":"override"}`)
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"or-test":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"or-test":2}`))
	require.NoError(t, ratio_setting.UpdateCacheRatioByJSONString(`{"or-test":0.25}`))
	require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(`{"or-test":1.25}`))
	for path, format := range map[string]types.RelayFormat{"/v1/responses": types.RelayFormatOpenAIResponses, "/v1/embeddings": types.RelayFormatEmbedding, "/v1/rerank": types.RelayFormatRerank, "/v1/images/generations": types.RelayFormatOpenAIImage, "/v1/images/edits": types.RelayFormatOpenAIImage, "/v1/audio/speech": types.RelayFormatOpenAIAudio, "/v1/audio/transcriptions": types.RelayFormatOpenAIAudio} {
		engine.POST(path, func(c *gin.Context) { Relay(c, format) })
	}
	engine.POST("/v1/images", RelayOpenRouterImages)
	return db, engine, ch
}

func TestOpenRouterGatewayInterfaces(t *testing.T) {
	for _, tc := range []struct {
		path, fields, response, media string
		quota                         int
	}{
		{"/v1/chat/completions", `"messages":[{"role":"user","content":"hi"}],"max_tokens":0,"temperature":0,"reasoning_effort":"none","service_tier":"priority"`, `{"choices":[{"message":{"content":"OK","reasoning_details":[{"data":"opaque"}]}}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":40,"cached_creation_tokens":20}}}`, "application/json", 95},
		{"/v1/messages", `"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"history","signature":"sig"},{"type":"tool_use","id":"c1","name":"echo","input":{"n":0}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"c1","content":"OK","is_error":false}]}],"max_tokens":0,"thinking":{"type":"disabled"}`, `{"type":"message","content":[{"type":"text","text":"OK"}],"usage":{"input_tokens":40,"output_tokens":10,"cache_read_input_tokens":40,"cache_creation_input_tokens":20}}`, "application/json", 95},
		{"/v1/responses", `"input":"hi","store":false,"background":false,"max_output_tokens":0`, `{"id":"r1","output":[],"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":40,"cached_creation_tokens":20}}}`, "application/json", 95},
		{"/v1/embeddings", `"input":[1,2,3],"encoding_format":"base64"`, `{"data":[{"index":0,"embedding":"AAAA"}],"usage":{"prompt_tokens":3,"total_tokens":3}}`, "application/json", 3},
		{"/v1/embeddings", `"input":[{"content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}]}]`, `{"data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`, "application/json", 3},
		{"/v1/rerank", `"query":"hi","documents":[{"text":"hi","image":"https://example.com/a.png"}],"top_n":0,"return_documents":true`, `{"model":"vendor/mapped","results":[{"index":0,"document":{"text":"hi"},"relevance_score":0.9}],"usage":{"total_tokens":3,"search_units":1}}`, "application/json", 3},
		{"/v1/images", `"prompt":"draw","n":3,"seed":0,"resolution":"1K","background":"transparent"`, `{"data":[{"b64_json":"YWJj"}],"usage":{"prompt_tokens":0,"completion_tokens":10,"total_tokens":10}}`, "application/json", 20},
		{"/v1/images/generations", `"prompt":"draw","stream":true`, `{"data":[{"b64_json":"YWJj"}],"usage":{"prompt_tokens":0,"completion_tokens":10,"total_tokens":10}}`, "application/json", 20},
		{"/v1/images/edits", `"prompt":"edit","image":"https://example.com/a.png"`, `{"data":[{"b64_json":"YWJj"}],"usage":{"prompt_tokens":0,"completion_tokens":10,"total_tokens":10}}`, "application/json", 20},
		{"/v1/audio/transcriptions", `"input_audio":{"url":"https://example.com/a.wav"},"diarize":false`, `{"text":"OK","words":[{"speaker":0}],"usage":{"input_tokens":3,"output_tokens":0,"total_tokens":3}}`, "application/json", 3},
	} {
		t.Run(tc.path+tc.fields[:min(len(tc.fields), 20)], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path := tc.path
				if strings.HasPrefix(path, "/v1/images") {
					path = "/v1/images"
				}
				require.Equal(t, "/proxy"+path, r.URL.Path)
				require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
				require.Equal(t, "New API", r.Header.Get("X-OpenRouter-Title"))
				var got map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &got))
				require.Equal(t, "vendor/mapped", got["model"])
				require.Equal(t, "override", got["user"])
				require.NotContains(t, got, "service_tier")
				require.Equal(t, map[string]any{"allow_fallbacks": false}, got["provider"])
				if tc.path == "/v1/messages" {
					require.Contains(t, fmt.Sprint(got["messages"]), "signature:sig")
					require.Equal(t, float64(0), got["max_tokens"])
				}
				if tc.path == "/v1/chat/completions" {
					require.Equal(t, map[string]any{"effort": "none"}, got["reasoning"])
					require.Equal(t, float64(0), got["temperature"])
				}
				if tc.path == "/v1/rerank" {
					require.NotContains(t, got, "return_documents")
				}
				if tc.path == "/v1/images/edits" {
					require.Contains(t, got, "input_references")
					require.NotContains(t, got, "image")
				}
				w.Header().Set("Content-Type", tc.media)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL+"/proxy/v1/")
			body := `{"model":"or-test","provider":{"allow_fallbacks":false},` + tc.fields + `}`
			w := requestMiMo(t, engine, tc.path, body, mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.JSONEq(t, tc.response, w.Body.String())
			assertMiMoQuota(t, db, tc.quota)
		})
	}
}

func TestOpenRouterGatewayFailureRefundAndNoRetry(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, media string
		stream                  bool
	}{
		{"json error", "/v1/chat/completions", `{"error":{"message":"unavailable","code":503}}`, "application/json", false},
		{"stream error", "/v1/chat/completions", "data: {\"error\":{\"message\":\"unavailable\",\"code\":503}}\n\n", "text/event-stream", true},
		{"preview", "/v1/images", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"abc\"}\n\ndata: [DONE]\n\n", "text/event-stream", true},
		{"image disconnect", "/v1/images", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"abc\"}\n\n", "text/event-stream", true},
		{"chat disconnect", "/v1/chat/completions", ": waiting\n\n", "text/event-stream", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.media)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
			if tc.stream {
				common.RetryTimes = 2
			}
			w := requestMiMo(t, engine, tc.path, `{"model":"or-test","prompt":"draw","messages":[{"role":"user","content":"hi"}],"stream":true}`, mimoGatewayKey)
			require.Equal(t, int32(1), calls.Load())
			if tc.stream {
				require.Equal(t, tc.body, w.Body.String())
			} else {
				require.Equal(t, 503, w.Code)
			}
			assertMiMoQuota(t, db, 0)
		})
	}
}

func TestOpenRouterGatewayFinalValidation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid request reached upstream") }))
	defer upstream.Close()
	db, engine, ch := setupOpenRouterGateway(t, upstream.URL)
	for _, pass := range []bool{false, true} {
		ch.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: pass, ShowErrorDetails: true})
		require.NoError(t, ch.Update())
		for _, field := range []string{`"store":true`, `"background":true`, `"previous_response_id":""`} {
			w := requestMiMo(t, engine, "/v1/responses", `{"model":"or-test","input":"hi",`+field+`}`, mimoGatewayKey)
			require.Equal(t, 400, w.Code, w.Body.String())
		}
	}
	ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: true})
	ch.ParamOverride = common.GetPointer(`{"store":true}`)
	require.NoError(t, ch.Update())
	w := requestMiMo(t, engine, "/v1/responses", `{"model":"or-test","input":"hi","store":false}`, mimoGatewayKey)
	require.Equal(t, 400, w.Code, w.Body.String())
	assertMiMoQuota(t, db, 0)
}

func TestOpenRouterGatewayStreamingUsage(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			body := mimoMockResponse(path == "/v1/messages", true)
			if path == "/v1/chat/completions" {
				body = strings.ReplaceAll(body, `"choices":[]`, `"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]`)
			}
			if path == "/v1/responses" {
				body = "event: response.completed\ndata: " + `{"type":"response.completed","response":{"id":"r1","output":[],"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":40}}}}` + "\n\n"
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, path, r.URL.Path)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
			w := requestMiMo(t, engine, path, `{"model":"or-test","input":"hi","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"stream":true}`, mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, body, w.Body.String())
			assertMiMoQuota(t, db, 90)
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			require.Len(t, logs, 1)
		})
	}
}

func TestOpenRouterGatewayNativeMessagesExpression(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","content":[],"usage":{"input_tokens":40,"output_tokens":10,"cache_read_input_tokens":40,"cache_creation_input_tokens":20,"cost":999}}`)
	}))
	defer upstream.Close()
	db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	oldMode, oldExpr := settings.BillingMode, settings.BillingExpr
	t.Cleanup(func() { settings.BillingMode = oldMode; settings.BillingExpr = oldExpr })
	settings.BillingMode = map[string]string{"or-test": "tiered_expr"}
	settings.BillingExpr = map[string]string{"or-test": `tier("base", p*2 + c*4 + cr*0.5 + cc*2.5)`}
	w := requestMiMo(t, engine, "/v1/messages", `{"model":"or-test","messages":[{"role":"user","content":"hi"}],"max_tokens":64}`, mimoGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	assertMiMoQuota(t, db, 95)
}

func TestOpenRouterGatewayFixedImagesAndSpeech(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				body := `{"data":[{"b64_json":"abc"},{"b64_json":"def"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					body = "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"abc\"}\n\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"def\",\"usage\":{\"prompt_tokens\":0,\"completion_tokens\":0,\"total_tokens\":0}}\n\ndata: [DONE]\n\n"
				}
				_, _ = io.WriteString(w, body)
			}))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"or-test":0.01}`))
			w := requestMiMo(t, engine, "/v1/images", fmt.Sprintf(`{"model":"or-test","prompt":"draw","n":3,"stream":%t}`, stream), mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			assertMiMoQuota(t, db, int(.02*common.QuotaPerUnit))
		})
	}
	t.Run("speech", func(t *testing.T) {
		pcm := strings.Repeat("ab", 24000)
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/v1/audio/speech", r.URL.Path)
			var body map[string]any
			require.NoError(t, common.DecodeJson(r.Body, &body))
			require.Len(t, body["input"], 2)
			require.Equal(t, "override", body["user"])
			w.Header().Set("Content-Type", "audio/pcm")
			w.Header().Set("X-Generation-Id", "g1")
			_, _ = io.WriteString(w, pcm)
		}))
		defer upstream.Close()
		db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
		w := requestMiMo(t, engine, "/v1/audio/speech", `{"model":"or-test","input":[{"text":"Hi","voice":"a"},{"text":"Hello","voice":"b"}]}`, mimoGatewayKey)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Equal(t, pcm, w.Body.String())
		require.Equal(t, "g1", w.Header().Get("X-Generation-Id"))
		var log model.Log
		require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
		require.Contains(t, log.Other, `"usage_source":"local_estimate"`)
		assertMiMoQuota(t, db, log.Quota)
	})
}

func TestOpenRouterModelDiscovery(t *testing.T) {
	for _, base := range []string{"https://openrouter.ai/api", "https://openrouter.ai/api/v1/", "https://proxy.example/prefix/v1"} {
		got := resolveFetchModelsURL(constant.ChannelTypeOpenRouter, base, "")
		require.Equal(t, strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")+"/v1/models?output_modalities=text,image,embeddings,audio,rerank,speech,transcription", got)
		require.Equal(t, "https://custom.example/list", resolveFetchModelsURL(constant.ChannelTypeOpenRouter, base, "https://custom.example/list"))
	}
	for model, endpoint := range map[string]constant.EndpointType{"openai/text-embedding-3-small": constant.EndpointTypeEmbeddings, "cohere/rerank-v3.5": constant.EndpointTypeJinaRerank, "black-forest-labs/flux-2-pro": constant.EndpointTypeImageGeneration, "bytedance-seed/seedream-4.5": constant.EndpointTypeImageGeneration, "mistralai/voxtral-mini-tts-2603": constant.EndpointTypeAudioSpeech, "openai/whisper-1": constant.EndpointTypeAudioTranscription} {
		require.Equal(t, endpoint, common.GetEndpointTypesByChannelType(constant.ChannelTypeOpenRouter, model)[0])
	}
}

func TestOpenRouterExplicitResponsesConversion(t *testing.T) {
	policy := model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy
	t.Cleanup(func() { model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = policy })
	model_setting.GetGlobalSettings().ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{".*"}}
	for _, stream := range []bool{false, true} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/failed=%t", stream, failed), func(t *testing.T) {
				response := `{"id":"r1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "/v1/responses", r.URL.Path)
					w.Header().Set("Content-Type", "application/json")
					body := response
					if failed {
						body = `{"error":{"code":503,"message":"failed"}}`
					}
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						if failed {
							body = ": upstream disconnected\n\n"
						} else {
							body = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
						}
					}
					_, _ = io.WriteString(w, body)
				}))
				defer upstream.Close()
				db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
				w := requestMiMo(t, engine, "/v1/messages", fmt.Sprintf(`{"model":"or-test","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"stream":%t}`, stream), mimoGatewayKey)
				if failed {
					assertMiMoQuota(t, db, 0)
					require.NotContains(t, w.Body.String(), "message_stop")
				} else {
					require.Equal(t, 200, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), "OK")
					assertMiMoQuota(t, db, 5)
				}
			})
		}
	}
}

// Live calls are opt-in and may incur upstream charges. Never print the key.
func TestOpenRouterLiveChat(t *testing.T) {
	if os.Getenv("OPENROUTER_LIVE_TEST") != "1" {
		t.Skip("set OPENROUTER_LIVE_TEST=1, OPENROUTER_API_KEY and OPENROUTER_TEST_MODEL to enable")
	}
	key, modelName := os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENROUTER_TEST_MODEL")
	require.NotEmpty(t, key)
	require.NotEmpty(t, modelName)
	_, engine, ch := setupOpenRouterGateway(t, "https://openrouter.ai/api")
	ch.Key = key
	mapping, err := common.Marshal(map[string]string{"or-test": modelName})
	require.NoError(t, err)
	ch.ModelMapping = common.GetPointer(string(mapping))
	require.NoError(t, ch.Update())
	w := requestMiMo(t, engine, "/v1/chat/completions", `{"model":"or-test","messages":[{"role":"user","content":"Reply OK"}],"max_tokens":16}`, mimoGatewayKey)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"choices"`)
}
