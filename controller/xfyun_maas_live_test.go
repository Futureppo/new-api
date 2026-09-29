package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/xfyun_maas"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Opt-in, real requests through authentication, distribution, mapping, relay and
// settlement. The shared gateway fixture uses an in-memory DB and synthetic
// prices; neither credentials nor account data are persisted.
func TestXfyunMaasLiveGateway(t *testing.T) {
	if os.Getenv("XFYUN_MAAS_LIVE_TEST") != "1" {
		t.Skip("set XFYUN_MAAS_LIVE_TEST=1 and XFYUN_MAAS_API_KEY to run")
	}
	key := os.Getenv("XFYUN_MAAS_API_KEY")
	require.NotEmpty(t, key)
	db, engine, ch := setupMiMoGateway(t, xfyun_maas.DefaultBaseURL, key)
	models := map[string]string{
		"maas-spark": "spark-x2.5-1.7b", "maas-qwen": "xop35qwen2b",
		"maas-vision": "xophunyuanocr", "maas-z-image": "xopzimageturbo",
		"maas-qwen-image": "xopqwentti20b", "maas-embedding": "xop3qwen8bembedding",
		"maas-rerank": "xop3qwen8breranker",
	}
	var names []string
	ratios := make(map[string]float64)
	for alias := range models {
		names = append(names, alias)
		ratios[alias] = 1
	}
	mapping, err := common.Marshal(models)
	require.NoError(t, err)
	ch.Type, ch.Name, ch.Models = constant.ChannelTypeXunfeiMaas, "maas-live-test", strings.Join(names, ",")
	ch.ModelMapping = common.GetPointer(string(mapping))
	require.NoError(t, ch.Update())
	b, err := common.Marshal(ratios)
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(b)))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"maas-z-image":0.001,"maas-qwen-image":0.001}`))
	engine.POST("/v1/responses", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIResponses) })
	engine.POST("/v1/images/generations", RelayImageGeneration)
	engine.POST("/v1/embeddings", func(c *gin.Context) { Relay(c, types.RelayFormatEmbedding) })
	engine.POST("/v1/rerank", func(c *gin.Context) { Relay(c, types.RelayFormatRerank) })
	request := func(t *testing.T, path string, body map[string]any) []byte {
		t.Helper()
		// Free endpoints have low QPS quotas. Never automatically retry a generation.
		time.Sleep(450 * time.Millisecond)
		data, err := common.Marshal(body)
		require.NoError(t, err)
		w := requestMiMo(t, engine, path, string(data), mimoGatewayKey)
		require.Equal(t, http.StatusOK, w.Code, "response: %.1000s", w.Body.String())
		require.NotContains(t, w.Body.String(), `"error"`)
		return w.Body.Bytes()
	}
	successes := 0
	for _, alias := range []string{"maas-spark", "maas-qwen"} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s%s/stream=%t", alias, path, stream), func(t *testing.T) {
					body := map[string]any{"model": alias, "stream": stream, "temperature": 0}
					if path == "/v1/responses" {
						body["input"], body["max_output_tokens"] = "Reply with exactly OK.", 128
					} else {
						body["messages"] = []any{map[string]any{"role": "user", "content": "Reply with exactly OK."}}
						body["max_tokens"] = 128
					}
					if path == "/v1/messages" {
						body["thinking"] = map[string]any{"type": "disabled"}
					} else {
						body["enable_thinking"] = false
					}
					if stream && path == "/v1/chat/completions" {
						body["stream_options"] = map[string]any{"include_usage": true}
					}
					data := request(t, path, body)
					require.Contains(t, string(data), "OK")
					require.Contains(t, string(data), `"usage"`)
					if stream {
						terminal := "[DONE]"
						if path == "/v1/responses" {
							terminal = "response.completed"
						}
						if path == "/v1/messages" {
							terminal = "message_stop"
						}
						require.Contains(t, string(data), terminal)
					}
					successes++
				})
			}
		}
	}
	for _, feature := range []string{"tools", "thinking"} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s%s/stream=%t", feature, path, stream), func(t *testing.T) {
					prompt := "What is 17 times 23?"
					if feature == "tools" {
						prompt = "Call get_weather with city Paris. Do not answer without calling the tool."
					}
					body := map[string]any{"model": "maas-spark", "stream": stream, "enable_thinking": feature == "thinking"}
					if path == "/v1/responses" {
						body["input"], body["max_output_tokens"] = prompt, 512
					} else {
						body["messages"] = []any{map[string]any{"role": "user", "content": prompt}}
						body["max_tokens"] = 512
					}
					if path == "/v1/messages" {
						delete(body, "enable_thinking")
						thinking := "disabled"
						if feature == "thinking" {
							thinking = "enabled"
						}
						body["thinking"] = map[string]any{"type": thinking}
					}
					if stream && path == "/v1/chat/completions" {
						body["stream_options"] = map[string]any{"include_usage": true}
					}
					if feature == "tools" {
						params := map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}}
						function := map[string]any{"name": "get_weather", "description": "Get current weather for a city", "parameters": params}
						switch path {
						case "/v1/responses":
							function["type"] = "function"
							body["tools"], body["tool_choice"] = []any{function}, "required"
						case "/v1/messages":
							delete(function, "parameters")
							function["input_schema"] = params
							body["tools"], body["tool_choice"] = []any{function}, map[string]any{"type": "any"}
						default:
							body["tools"], body["tool_choice"] = []any{map[string]any{"type": "function", "function": function}}, "required"
						}
					}
					data := string(request(t, path, body))
					if feature == "tools" {
						require.Contains(t, data, "get_weather")
						require.Contains(t, data, "Paris")
					} else {
						marker := "reasoning_content"
						if path == "/v1/messages" {
							marker = "thinking"
						}
						if path == "/v1/responses" {
							marker = "reasoning"
						}
						require.Contains(t, data, marker)
					}
					require.Contains(t, data, `"usage"`)
					successes++
				})
			}
		}
	}
	imageBytes, err := os.ReadFile("../relay/channel/xfyun_maas/testdata/ocr.png")
	require.NoError(t, err)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("vision/stream=%t", stream), func(t *testing.T) {
			body := map[string]any{"model": "maas-vision", "stream": stream, "max_tokens": 128, "messages": []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "Read the text in this image."},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageBytes)}},
			}}}}
			if stream {
				body["stream_options"] = map[string]any{"include_usage": true}
			}
			data := request(t, "/v1/chat/completions", body)
			require.Contains(t, string(data), "123")
			require.Contains(t, string(data), `"usage"`)
			successes++
		})
	}
	for _, alias := range []string{"maas-z-image", "maas-qwen-image"} {
		t.Run(alias, func(t *testing.T) {
			data := request(t, "/v1/images/generations", map[string]any{"model": alias, "prompt": "A mountain under a blue sky.", "seed": 0, "size": "768x768", "negative_prompt": "black and white"})
			var out struct {
				Data []struct {
					B64 string `json:"b64_json"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(data, &out))
			require.Len(t, out.Data, 1)
			img, err := base64.StdEncoding.DecodeString(out.Data[0].B64)
			require.NoError(t, err)
			config, format, err := image.DecodeConfig(bytes.NewReader(img))
			require.NoError(t, err)
			require.Equal(t, 768, config.Width)
			require.Equal(t, 768, config.Height)
			t.Logf("%s: %s %dx%d, %d bytes", alias, format, config.Width, config.Height, len(img))
			successes++
		})
	}
	for _, encoding := range []string{"float", "base64"} {
		t.Run("embedding/"+encoding, func(t *testing.T) {
			input := any("hello world")
			if encoding == "base64" {
				input = []string{"hello world", "hello world"}
			}
			data := request(t, "/v1/embeddings", map[string]any{"model": "maas-embedding", "input": input, "dimensions": 32, "encoding_format": encoding})
			if encoding == "float" {
				var out struct {
					Data []struct{ Embedding []float32 }
				}
				require.NoError(t, common.Unmarshal(data, &out))
				require.Len(t, out.Data, 1)
				require.Len(t, out.Data[0].Embedding, 32)
			} else {
				var out struct{ Data []struct{ Embedding string } }
				require.NoError(t, common.Unmarshal(data, &out))
				require.Len(t, out.Data, 2)
				for _, item := range out.Data {
					decoded, err := base64.StdEncoding.DecodeString(item.Embedding)
					require.NoError(t, err)
					require.Len(t, decoded, 32*4)
					// Exact conversion is checked against the same upstream response
					// by TestXfyunMaasEmbeddingDimensionsLive. A second inference
					// can legitimately differ in its vector values.
					for i := 0; i < 32; i++ {
						value := float64(math.Float32frombits(binary.LittleEndian.Uint32(decoded[i*4:])))
						require.False(t, math.IsNaN(value) || math.IsInf(value, 0))
					}
				}
			}
			successes++
		})
	}
	t.Run("rerank", func(t *testing.T) {
		data := request(t, "/v1/rerank", map[string]any{"model": "maas-rerank", "query": "What is the capital of France?", "documents": []string{"Berlin is in Germany.", "Paris is the capital of France.", "The sky is blue."}, "top_n": 1, "return_documents": true})
		var out dto.RerankResponse
		require.NoError(t, common.Unmarshal(data, &out))
		require.Len(t, out.Results, 1)
		require.Equal(t, 1, out.Results[0].Index)
		require.Contains(t, string(data), "Paris is the capital of France.")
		require.Positive(t, out.Usage.TotalTokens)
		successes++
	})
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, successes)
	used := 0
	for _, log := range logs {
		require.Positive(t, log.PromptTokens)
		require.Positive(t, log.Quota)
		used += log.Quota
		t.Logf("%s: input=%d output=%d quota=%d", log.ModelName, log.PromptTokens, log.CompletionTokens, log.Quota)
	}
	assertMiMoQuota(t, db, used)
}
