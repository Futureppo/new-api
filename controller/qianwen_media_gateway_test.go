package controller

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/qianwen"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQianwenMediaGateway(t *testing.T) {
	for _, tc := range []struct {
		name, model, endpoint, upstream, body, response string
		format                                          types.RelayFormat
		units                                           float64
		tokenQuota                                      int
	}{
		{"embedding", "text-embedding-v4", "/v1/embeddings", "/compatible-mode/v1/embeddings", `"input":["hello"],"dimensions":0,"encoding_format":"float"`, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":100,"total_tokens":100}}`, types.RelayFormatEmbedding, 0, 100},
		{"rerank-compatible", "qwen3-rerank", "/v1/rerank", "/compatible-api/v1/reranks", `"query":"hi","documents":["hi"],"top_n":0,"return_documents":false,"instruct":"rank"`, `{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":100}}`, types.RelayFormatRerank, 0, 100},
		{"rerank-native", "qwen3.7-text-rerank", "/v1/rerank", "/api/v1/services/rerank/text-rerank/text-rerank", `"query":"hi","documents":["hi"],"top_n":0,"return_documents":false,"instruct":"rank","fps":0`, `{"output":{"results":[{"index":0,"relevance_score":0.9}]},"usage":{"total_tokens":100,"prompt_tokens":100}}`, types.RelayFormatRerank, 0, 100},
		{"image-compatible", "qwen-image-3.0-pro", "/v1/images/generations", "/compatible-mode/v1/images/generations", `"prompt":"cat","n":0,"seed":0,"prompt_extend":false,"enable_thinking":false,"watermark":false,"image":["https://example.com/cat.png"]`, `{"created":1,"data":[{"url":"https://example.com/1.png"},{"url":"https://example.com/2.png"}],"usage":{"output_image_count":2}}`, types.RelayFormatOpenAIImage, 2, 0},
		{"image-native-edit", "qwen-image-2.0", "/v1/images/edits", "/api/v1/services/aigc/multimodal-generation/generation", `"prompt":"cat","n":0,"seed":0,"prompt_extend":false,"enable_thinking":false,"watermark":false,"image":["https://example.com/cat.png"]`, `{"output":{"choices":[{"message":{"content":[{"image":"https://example.com/1.png"}]}}]},"usage":{"image_count":1}}`, types.RelayFormatOpenAIImage, 1, 0},
		{"tts-characters", "qwen-audio-3.0-tts-flash", "/v1/audio/speech", "/api/v1/services/audio/tts/SpeechSynthesizer", `"input":"hi","voice":"longanhuan_v3.6","metadata":{"seed":0,"volume":0,"enable_aigc_tag":false}`, `{"output":{"finish_reason":"stop","audio":{"data":"YXVkaW8="}},"usage":{"characters":5}}`, types.RelayFormatOpenAIAudio, 5, 0},
		{"audio-seconds", "qwen-audio-3.1-tts-next", "/v1/audio/speech", "/api/v1/services/audio/tts/SpeechSynthesizer", `"input":"a bird singing","metadata":{"seed":0,"volume":0,"enable_aigc_tag":false}`, `{"output":{"finish_reason":"stop","audio":{"data":"YXVkaW8="}},"usage":{"duration":3}}`, types.RelayFormatOpenAIAudio, 3, 0},
		{"music-seconds", "fun-music-v1", "/v1/audio/speech", "/api/v1/services/audio/music/generation", `"input":"music","metadata":{"seed":0,"volume":0,"enable_aigc_tag":false,"is_instrumental":false}`, `{"output":{"finish_reason":"stop","audio":{"data":"YXVkaW8="}},"usage":{"duration":4}}`, types.RelayFormatOpenAIAudio, 4, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/proxy"+tc.upstream, r.URL.Path)
				require.Equal(t, "Bearer media-key", r.Header.Get("Authorization"))
				data, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, tc.model, gjson.GetBytes(data, "model").String())
				if strings.Contains(tc.name, "image") {
					prefix := ""
					if tc.name == "image-native-edit" {
						prefix = "parameters."
						require.Equal(t, "https://example.com/cat.png", gjson.GetBytes(data, "input.messages.0.content.0.image").String())
					}
					for _, key := range []string{"n", "seed", "prompt_extend", "enable_thinking", "watermark"} {
						value := gjson.GetBytes(data, prefix+key)
						require.True(t, value.Exists(), key)
						require.False(t, value.Bool(), key)
					}
				} else if strings.Contains(tc.name, "rerank") {
					prefix := ""
					if tc.name == "rerank-native" {
						prefix = "parameters."
					}
					require.Equal(t, "0", gjson.GetBytes(data, prefix+"top_n").Raw)
					require.Equal(t, "false", gjson.GetBytes(data, prefix+"return_documents").Raw)
					require.Equal(t, "rank", gjson.GetBytes(data, prefix+"instruct").String())
				} else if tc.name == "embedding" {
					require.Equal(t, "0", gjson.GetBytes(data, "dimensions").Raw)
				} else {
					require.Equal(t, "0", gjson.GetBytes(data, "input.seed").Raw)
					require.Equal(t, "0", gjson.GetBytes(data, "input.volume").Raw)
					require.Equal(t, "false", gjson.GetBytes(data, "input.enable_aigc_tag").Raw)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			db, engine, ch := setupQianwenGateway(t, upstream.URL+"/proxy/compatible-mode/v1", "media-key")
			ch.ModelMapping = common.GetPointer(fmt.Sprintf(`{"qianwen-test":%q}`, tc.model))
			require.NoError(t, ch.Update())
			if tc.units > 0 {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"qianwen-test":0.0001}`))
			}
			if tc.endpoint != "/v1/embeddings" {
				engine.POST(tc.endpoint, func(c *gin.Context) { Relay(c, tc.format) })
			}
			w := requestMiMo(t, engine, tc.endpoint, `{"model":"qianwen-test",`+tc.body+`}`, mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			if strings.Contains(tc.endpoint, "audio") {
				require.Equal(t, "audio", w.Body.String())
			}
			var log model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
			quota := tc.tokenQuota
			if tc.units > 0 {
				quota = int(math.Round(common.QuotaPerUnit * 0.0001 * tc.units))
				require.Zero(t, log.PromptTokens)
				require.Zero(t, log.CompletionTokens)
			}
			require.Equal(t, quota, log.Quota)
			assertMiMoQuota(t, db, quota)
		})
	}
}

func TestQianwenAudioStreamUsageAndInterrupt(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupted), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "enable", r.Header.Get("X-DashScope-SSE"))
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: "+`{"output":{"audio":{"data":"YQ=="}},"usage":{"characters":2}}`+"\n\n")
				io.WriteString(w, "data: "+`{"output":{"audio":{"data":"Yg=="}},"usage":{"characters":5}}`+"\n\n")
				if !interrupted {
					io.WriteString(w, "data: "+`{"output":{"finish_reason":"stop","audio":{"data":""}},"usage":{"characters":5}}`+"\n\n")
				}
			}))
			defer upstream.Close()
			db, engine, ch := setupQianwenGateway(t, upstream.URL, "key")
			ch.ModelMapping = common.GetPointer(`{"qianwen-test":"qwen-audio-3.0-tts-flash"}`)
			require.NoError(t, ch.Update())
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"qianwen-test":0.0001}`))
			engine.POST("/v1/audio/speech", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIAudio) })
			w := requestMiMo(t, engine, "/v1/audio/speech", `{"model":"qianwen-test","input":"hi","voice":"longanhuan_v3.6","stream_format":"sse"}`, mimoGatewayKey)
			require.Contains(t, w.Body.String(), "audio.delta")
			if interrupted {
				require.NotContains(t, w.Body.String(), "audio.done")
				require.Contains(t, w.Body.String(), "error")
				require.True(t, strings.HasSuffix(w.Body.String(), "\n\n"), "stream error must remain a complete SSE frame without trailing JSON")
				assertMiMoQuota(t, db, 0)
			} else {
				require.Contains(t, w.Body.String(), "audio.done")
				assertMiMoQuota(t, db, int(math.Round(common.QuotaPerUnit*0.0001*5)))
			}
		})
	}
}

func TestQianwenVideoLifecycleAndSettlement(t *testing.T) {
	for _, price := range []float64{0.0001, 0, -0.0001, 0.000000001} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("price=%g/failure=%v", price, failure), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, "Bearer video-key", r.Header.Get("Authorization"))
					w.Header().Set("Content-Type", "application/json")
					if r.Method == http.MethodPost {
						require.Equal(t, "/api/v1/services/aigc/video-generation/video-synthesis", r.URL.Path)
						require.Equal(t, "enable", r.Header.Get("X-DashScope-Async"))
						data, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						require.Equal(t, "wan3.0-video", gjson.GetBytes(data, "model").String())
						for _, key := range []string{"audio", "watermark", "prompt_extend"} {
							require.Equal(t, "false", gjson.GetBytes(data, "parameters."+key).Raw)
						}
						require.Equal(t, "0", gjson.GetBytes(data, "parameters.seed").Raw)
						require.Equal(t, "reference_image", gjson.GetBytes(data, "input.media.0.type").String())
						io.WriteString(w, `{"output":{"task_id":"upstream-video","task_status":"PENDING"}}`)
					} else {
						require.Equal(t, "/api/v1/tasks/upstream-video", r.URL.Path)
						if failure {
							io.WriteString(w, `{"output":{"task_id":"upstream-video","task_status":"FAILED","code":"InvalidParameter","message":"bad video"}}`)
						} else {
							io.WriteString(w, `{"output":{"task_id":"upstream-video","task_status":"SUCCEEDED","video_url":"https://example.com/video.mp4"},"usage":{"duration":7.5,"input_video_duration":2.5,"output_video_duration":5}}`)
						}
					}
				}))
				defer upstream.Close()
				db, engine, ch := setupQianwenGateway(t, upstream.URL+"/api/v1", "video-key")
				ch.ModelMapping = common.GetPointer(`{"qianwen-test":"wan3.0-video"}`)
				require.NoError(t, ch.Update())
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(fmt.Sprintf(`{"qianwen-test":%g}`, price)))
				engine.POST("/v1/videos", RelayTask)
				w := requestMiMo(t, engine, "/v1/videos", `{"model":"qianwen-test","prompt":"cat","seconds":"5","input_reference":"https://example.com/cat.png","metadata":{"parameters":{"seed":0,"watermark":false,"audio":false,"prompt_extend":false}}}`, mimoGatewayKey)
				require.Equal(t, 200, w.Code, w.Body.String())
				var task model.Task
				require.NoError(t, db.Where("platform = ?", "77").First(&task).Error)
				require.Equal(t, "upstream-video", task.GetUpstreamTaskID())
				require.Equal(t, "video-key", task.PrivateData.Key)
				require.Equal(t, upstream.URL+"/api/v1", task.PrivateData.QianwenBaseURL)
				require.False(t, task.PrivateData.BillingContext.PerCallBilling)
				ch.BaseURL, ch.Key = common.GetPointer("https://changed.example"), "changed-key"
				a := &qianwen.TaskAdaptor{}
				require.NoError(t, service.RefreshVideoTask(context.Background(), a, ch, &task))
				want := int(math.Round(common.QuotaPerUnit * price * 7.5))
				if failure {
					want = 0
					require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.Status)
				} else {
					require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
				}
				assertMiMoQuota(t, db, want)
				// Polling a terminal resource again cannot charge/refund twice.
				require.NoError(t, service.RefreshVideoTask(context.Background(), a, ch, &task))
				assertMiMoQuota(t, db, want)
			})
		}
	}
}
