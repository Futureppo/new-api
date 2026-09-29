package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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

// The provider's schema error enumerates exactly these nine dimensions. This
// opt-in test checks every supported value via the full gateway, including the
// omitted default and rejection/refund of unsupported values. No live calls run
// without explicit opt-in. All prices and balances belong to an in-memory DB.
func TestXfyunMaasEmbeddingDimensionsLive(t *testing.T) {
	if os.Getenv("XFYUN_MAAS_LIVE_TEST") != "1" {
		t.Skip("set XFYUN_MAAS_LIVE_TEST=1 and XFYUN_MAAS_API_KEY to run")
	}
	key := os.Getenv("XFYUN_MAAS_API_KEY")
	require.NotEmpty(t, key)
	// Compare each result to its own upstream response, not a second model
	// inference: identical requests can produce slightly different vectors.
	var captureMu sync.Mutex
	var captured, capturedRequest []byte
	client := &http.Client{Timeout: 45 * time.Second}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestData, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read request failed", 502)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, xfyun_maas.DefaultBaseURL+"/v2/embeddings", bytes.NewReader(requestData))
		if err != nil {
			http.Error(w, "create upstream request failed", 502)
			return
		}
		req.Header.Set("Authorization", r.Header.Get("Authorization"))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "upstream request failed", 502)
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "read upstream response failed", 502)
			return
		}
		captureMu.Lock()
		captured = data
		capturedRequest = requestData
		captureMu.Unlock()
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(data)
	}))
	defer proxy.Close()
	db, engine, ch := setupMiMoGateway(t, proxy.URL, key)
	ch.Type, ch.Models = constant.ChannelTypeXunfeiMaas, "maas-embedding"
	ch.ModelMapping = common.GetPointer(`{"maas-embedding":"xop3qwen8bembedding"}`)
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"maas-embedding":1}`))
	engine.POST("/v1/embeddings", func(c *gin.Context) { Relay(c, types.RelayFormatEmbedding) })
	successes := 0
	for _, dimensions := range []int{32, 64, 128, 256, 512, 768, 1024, 2048, 4096, 0} {
		name := fmt.Sprint(dimensions)
		expected := dimensions
		if dimensions == 0 {
			name, expected = "omitted-default", 768
		}
		t.Run(name, func(t *testing.T) {
			for _, batch := range []bool{false, true} {
				t.Run(fmt.Sprintf("batch=%t", batch), func(t *testing.T) {
					var referenceTokens int
					var firstRequest []byte
					var firstVectors [][]float32
					for _, encoding := range []string{"float", "base64"} {
						input := any("hello world")
						count := 1
						if batch {
							input, count = []string{"hello world", "向量维度测试"}, 2
						}
						body := map[string]any{"model": "maas-embedding", "input": input, "encoding_format": encoding}
						if dimensions != 0 {
							body["dimensions"] = dimensions
						}
						data, err := common.Marshal(body)
						require.NoError(t, err)
						time.Sleep(600 * time.Millisecond)
						w := requestMiMo(t, engine, "/v1/embeddings", string(data), mimoGatewayKey)
						require.Equal(t, http.StatusOK, w.Code, "response: %.1000s", w.Body.String())
						var original struct {
							Data []struct {
								Embedding []float32 `json:"embedding"`
							} `json:"data"`
						}
						captureMu.Lock()
						upstreamData := append([]byte(nil), captured...)
						upstreamRequest := append([]byte(nil), capturedRequest...)
						captureMu.Unlock()
						require.NoError(t, common.Unmarshal(upstreamData, &original))
						require.Len(t, original.Data, count)
						var out struct {
							Object string `json:"object"`
							Data   []struct {
								Index     int             `json:"index"`
								Embedding json.RawMessage `json:"embedding"`
							} `json:"data"`
							Usage dto.Usage `json:"usage"`
						}
						require.NoError(t, common.Unmarshal(w.Body.Bytes(), &out))
						require.Equal(t, "list", out.Object)
						require.Len(t, out.Data, count)
						require.Positive(t, out.Usage.PromptTokens)
						if encoding == "float" {
							firstRequest = upstreamRequest
						} else {
							require.JSONEq(t, string(firstRequest), string(upstreamRequest), "both encoding modes request identical float vectors upstream")
						}
						maxRepeatDelta := float64(0)
						for i, item := range out.Data {
							require.Equal(t, i, item.Index)
							var vector []float32
							if encoding == "float" {
								require.NoError(t, common.Unmarshal(item.Embedding, &vector))
							} else {
								var encoded string
								require.NoError(t, common.Unmarshal(item.Embedding, &encoded))
								decoded, err := base64.StdEncoding.DecodeString(encoded)
								require.NoError(t, err)
								require.Len(t, decoded, expected*4)
								vector = make([]float32, expected)
								for j := range vector {
									vector[j] = math.Float32frombits(binary.LittleEndian.Uint32(decoded[j*4:]))
								}
							}
							require.Equal(t, original.Data[i].Embedding, vector, "gateway must preserve every upstream float32 value")
							require.Len(t, vector, expected)
							if encoding == "float" {
								firstVectors = append(firstVectors, vector)
							} else {
								for j, value := range vector {
									maxRepeatDelta = math.Max(maxRepeatDelta, math.Abs(float64(value)-float64(firstVectors[i][j])))
								}
							}
							norm := float64(0)
							for _, v := range vector {
								require.False(t, math.IsNaN(float64(v)) || math.IsInf(float64(v), 0))
								norm += float64(v) * float64(v)
							}
							require.Positive(t, norm, "vector must not be all zeros")
						}
						if encoding == "float" {
							referenceTokens = out.Usage.PromptTokens
						} else {
							require.Equal(t, referenceTokens, out.Usage.PromptTokens)
						}
						successes++
						t.Logf("dimension=%s actual=%d encoding=%s count=%d tokens=%d", name, expected, encoding, count, out.Usage.PromptTokens)
						if encoding == "base64" {
							t.Logf("identical upstream requests: dimension=%s batch=%t max_repeat_delta=%.9g (gateway conversion matches its own response exactly)", name, batch, maxRepeatDelta)
						}
					}
				})
			}
		})
	}
	for _, dimensions := range []int{-1, 0, 1, 31, 33, 96, 1536, 3072, 4097} {
		t.Run(fmt.Sprintf("invalid=%d", dimensions), func(t *testing.T) {
			time.Sleep(600 * time.Millisecond)
			w := requestMiMo(t, engine, "/v1/embeddings", fmt.Sprintf(`{"model":"maas-embedding","input":"hello world","dimensions":%d}`, dimensions), mimoGatewayKey)
			require.Equal(t, http.StatusBadRequest, w.Code, "response: %.1000s", w.Body.String())
			require.Contains(t, w.Body.String(), `"error"`)
			t.Logf("dimension=%d rejected with HTTP %d", dimensions, w.Code)
		})
	}
	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, successes, "invalid dimensions must not create successful consumption records")
	used := 0
	for _, log := range logs {
		used += log.Quota
	}
	assertMiMoQuota(t, db, used)
}
