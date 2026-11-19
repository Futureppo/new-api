package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	taskgmicloud "github.com/QuantumNous/new-api/relay/channel/task/gmicloud"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func playgroundTestRouter() *gin.Engine {
	engine := gin.New()
	engine.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) { c.Set("id", 1) })
	engine.GET("/pg/endpoints", PlaygroundEndpoints)
	engine.POST("/pg/protocol/:endpoint", PreparePlaygroundProtocol, middleware.Distribute(), PlaygroundProtocol)
	engine.GET("/pg/tasks/:task_id", PlaygroundTask)
	return engine
}

func initPlaygroundTestColumns(t *testing.T) {
	t.Helper()
	saved := model.DB
	initModelListColumnNames(t)
	model.DB = saved
}

func playgroundTestPNG(t *testing.T, pixel color.Color) []byte {
	t.Helper()
	bmp := image.NewRGBA(image.Rect(0, 0, 1, 1))
	bmp.Set(0, 0, pixel)
	var body bytes.Buffer
	require.NoError(t, png.Encode(&body, bmp))
	return body.Bytes()
}

func TestPlaygroundNativeProtocols(t *testing.T) {
	initPlaygroundTestColumns(t)
	for _, tc := range []struct {
		endpoint                           constant.EndpointType
		channelType                        int
		model, path, field, body, response string
	}{
		{constant.EndpointTypeOpenAI, constant.ChannelTypeOpenAI, "gpt-4o", "/v1/chat/completions", "messages",
			`{"messages":[{"role":"user","content":"hi"}],"temperature":0}`,
			`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`},
		{constant.EndpointTypeOpenAIResponse, constant.ChannelTypeOpenAI, "gpt-4o", "/v1/responses", "input",
			`{"input":"hi","temperature":0}`,
			`{"id":"resp_pg","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`},
		{constant.EndpointTypeOpenAIResponseCompact, constant.ChannelTypeOpenAI, "gpt-4o", "/v1/responses/compact", "input",
			`{"input":[{"role":"user","content":"hi"}]}`,
			`{"id":"resp_compact","object":"response.compaction","output":[{"type":"compaction","encrypted_content":"encoded"}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`},
		{constant.EndpointTypeAnthropic, constant.ChannelTypeAnthropic, "claude-sonnet-4-5", "/v1/messages", "messages",
			`{"messages":[{"role":"user","content":"hi"}],"max_tokens":32,"temperature":0}`,
			`{"id":"msg_pg","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":1,"cache_read_input_tokens":0}}`},
		{constant.EndpointTypeGemini, constant.ChannelTypeGemini, "gemini-2.5-flash", "/v1beta/models/gemini-2.5-flash:generateContent", "contents",
			`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}`},
		{constant.EndpointTypeVertexNative, constant.ChannelTypeVertexAi, "gemini-2.5-flash", "/v1/publishers/google/models/gemini-2.5-flash:generateContent", "contents",
			`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}`},
		{constant.EndpointTypeVertexNative, constant.ChannelTypeVertexAi, "gemini-embedding-2", "/v1/projects/mock-project/locations/global/publishers/google/models/gemini-embedding-2:embedContent", "content",
			`{"content":{"parts":[{"text":"hi"}]}}`, `{"embedding":{"values":[1,2]}}`},
		{constant.EndpointTypeCohereChat, constant.ChannelTypeCohere, "command-a-03-2025", "/v2/chat", "messages",
			`{"messages":[{"role":"user","content":"hi"}]}`,
			`{"id":"cohere_pg","finish_reason":"COMPLETE","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"usage":{"billed_units":{"input_tokens":3,"output_tokens":1}}}`},
		{constant.EndpointTypeTypeSafeSystemOne, constant.ChannelTypeTypeSafe, "jev-latest", "/v1/systemone", "state",
			`{"state":"Hello","questions":{"greeting":{"type":"noul","instructions":"Is this a greeting?"}}}`, typeSafeTestResponse},
		{constant.EndpointTypeOpenAIDecisions, constant.ChannelTypeOpenAI, "gpt-6-luna", "/v1/decisions", "questions",
			`{"input":"Hello","questions":[{"type":"predicate","name":"greeting","instructions":"Is this a greeting?"}]}`,
			decisionsGatewayResponse},
		{constant.EndpointTypeEmbeddings, constant.ChannelTypeOpenAI, "text-embedding-3-small", "/v1/embeddings", "input",
			`{"input":["hi","hello"]}`,
			`{"data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`},
		{constant.EndpointTypeCohereEmbeddings, constant.ChannelTypeCohere, "embed-v4.0", "/v2/embed", "texts",
			`{"texts":["hi"],"input_type":"search_query","embedding_types":["float"]}`,
			`{"id":"embed_pg","embeddings":{"float":[[0.1,0.2]]},"meta":{"billed_units":{"input_tokens":3}}}`},
		{constant.EndpointTypeCohereRerank, constant.ChannelTypeCohere, "rerank-v3.5", "/v2/rerank", "documents",
			`{"query":"hi","documents":["hi","bye"],"top_n":1}`,
			`{"id":"rerank_pg","results":[{"index":0,"relevance_score":0.9}],"meta":{"billed_units":{"search_units":1}}}`},
		{constant.EndpointTypeJinaRerank, constant.ChannelTypeJina, "jina-reranker-v2-base-multilingual", "/v1/rerank", "documents",
			`{"query":"hi","documents":["hi","bye"],"top_n":1}`,
			`{"results":[{"index":0,"relevance_score":0.9}],"usage":{"total_tokens":2}}`},
		{constant.EndpointTypeModerations, constant.ChannelTypeOpenAI, "omni-moderation-latest", "/v1/moderations", "input",
			`{"input":"hello"}`, `{"id":"mod_pg","results":[{"flagged":false,"categories":{"violence":false},"category_scores":{"violence":0.01}}]}`},
		{constant.EndpointTypeImageGeneration, constant.ChannelTypeOpenAI, "gpt-image-1", "/v1/images/generations", "prompt",
			`{"prompt":"A mountain","n":1,"size":"1024x1024"}`,
			`{"created":1,"data":[{"url":"https://example.test/image.png"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`},
	} {
		t.Run(string(tc.endpoint)+"/"+tc.model, func(t *testing.T) {
			var images []string
			if tc.endpoint == constant.EndpointTypeVertexNative {
				for _, pixel := range []color.Color{color.Black, color.White} {
					images = append(images, base64.StdEncoding.EncodeToString(playgroundTestPNG(t, pixel)))
				}
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, tc.path, r.URL.Path)
				var body map[string]json.RawMessage
				require.NoError(t, common.DecodeJson(r.Body, &body))
				require.Contains(t, body, tc.field)
				require.NotContains(t, body, "group")
				if tc.endpoint != constant.EndpointTypeGemini && tc.endpoint != constant.EndpointTypeVertexNative {
					require.JSONEq(t, `"`+tc.model+`"`, string(body["model"]))
				}
				if tc.endpoint == constant.EndpointTypeCohereEmbeddings {
					require.JSONEq(t, `"search_query"`, string(body["input_type"]))
				}
				if len(images) > 0 {
					partsPath := "parts"
					if tc.field == "contents" {
						partsPath = "0.parts"
					}
					parts := gjson.GetBytes(body[tc.field], partsPath).Array()
					require.Len(t, parts, 3)
					assert.Equal(t, "hi", parts[0].Get("text").String())
					for i, image := range images {
						assert.Equal(t, "image/png", parts[i+1].Get("inlineData.mimeType").String())
						assert.Equal(t, image, parts[i+1].Get("inlineData.data").String())
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			db, _, ch := setupMiMoGateway(t, upstream.URL, "test-upstream-key")
			name := "pg-alias"
			if tc.endpoint == constant.EndpointTypeOpenAIResponseCompact {
				name += ratio_setting.CompactModelSuffix
			}
			ch.Type, ch.Models = tc.channelType, name
			ch.ModelMapping = common.GetPointer(`{"pg-alias":"` + tc.model + `"}`)
			ch.SetOtherSettings(dto.ChannelOtherSettings{ConversationLogEnabled: true})
			require.NoError(t, ch.Update())
			if tc.endpoint == constant.EndpointTypeVertexNative {
				keyType := dto.VertexKeyTypeAPIKey
				if tc.field == "content" {
					keyType = dto.VertexKeyTypeJSON
					ch.Key = mockVertexServiceAccount(t)
				}
				configureVertexTestChannel(t, ch, name, keyType)
			}
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pg-alias":1,"pg-alias-openai-decisions":1,"pg-alias-openai-compact":1,"gpt-4o-openai-compact":1}`))
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			engine := playgroundTestRouter()
			res := httptest.NewRecorder()
			engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/pg/endpoints?model="+name+"&group=default", nil))
			require.Equal(t, 200, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), `"`+string(tc.endpoint)+`"`)
			require.NotContains(t, res.Body.String(), "test-upstream-key")
			body := `{"model":"` + name + `","group":"default","native":true,"body":` + tc.body + `}`
			if len(images) > 0 {
				body = strings.TrimSuffix(body, "}") + `,"input_images":["data:image/png;base64,` + images[0] + `","data:image/png;base64,` + images[1] + `"]}`
			}
			res = requestMiMo(t, engine, "/pg/protocol/"+string(tc.endpoint), body, "")
			require.Equal(t, 200, res.Code, res.Body.String())
			require.EqualValues(t, 1, calls.Load())
			var logs int64
			require.NoError(t, db.Model(&model.ConversationLog{}).Count(&logs).Error)
			require.Zero(t, logs)
		})
	}
}

func TestPlaygroundRetriesOnlyMatchingProtocols(t *testing.T) {
	initPlaygroundTestColumns(t)
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") == "Bearer first" {
			requests.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"unavailable"}}`)
			return
		}
		require.Equal(t, "Bearer second", r.Header.Get("Authorization"))
		requests.Add(1)
		var fields map[string]json.RawMessage
		require.NoError(t, common.DecodeJson(r.Body, &fields))
		require.Contains(t, fields, "input")
		require.NotContains(t, fields, "messages")
		_, _ = io.WriteString(w, `{"id":"resp_pg","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}`)
	}))
	defer upstream.Close()
	_, _, fixture := setupMiMoGateway(t, upstream.URL, "fixture")
	common.RetryTimes = 1
	for _, spec := range []struct {
		kind     int
		key      string
		priority int64
	}{
		{constant.ChannelTypeAnthropic, "must-not-call", 20},
		{constant.ChannelTypeOpenAI, "first", 10},
		{constant.ChannelTypeOpenAI, "second", 5},
	} {
		ch := model.Channel{Type: spec.kind, Name: spec.key, Key: spec.key, BaseURL: fixture.BaseURL, Group: "default", Models: "pg-route", Status: common.ChannelStatusEnabled, Priority: &spec.priority, AutoBan: common.GetPointer(0)}
		require.NoError(t, ch.Insert())
	}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pg-route":1}`))
	res := requestMiMo(t, playgroundTestRouter(), "/pg/protocol/openai-response", `{"model":"pg-route","group":"default","body":{"messages":[{"role":"user","content":"hi"}]}}`, "")
	require.Equal(t, 200, res.Code, res.Body.String())
	require.EqualValues(t, 2, requests.Load())
}

func TestPlaygroundImageEmbeddings(t *testing.T) {
	initPlaygroundTestColumns(t)
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aASsAAAAASUVORK5CYII="
	const dataURL = "data:image/png;base64," + png
	const openAIResult = `{"object":"list","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`
	for _, tc := range []struct {
		kind                             int
		name, endpoint, path             string
		imageField, imageValue, response string
	}{
		{constant.ChannelTypeJina, "jina-clip-v2", "embeddings", "/v1/embeddings", "input.0.image", png, openAIResult},
		{constant.ChannelTypeVolcEngine, "doubao-embedding-vision-251215", "embeddings", "/api/v3/embeddings/multimodal", "input.0.image_url.url", dataURL,
			`{"object":"list","data":{"embedding":[0.1,0.2]},"usage":{"prompt_tokens":2,"total_tokens":2}}`},
		{constant.ChannelTypeOpenRouter, "nvidia/llama-nemotron-embed-vl-1b-v2", "embeddings", "/api/v1/embeddings", "input.0.content.0.image_url.url", dataURL, openAIResult},
		{constant.ChannelTypeCohere, "embed-v4.0", "cohere-embeddings", "/v2/embed", "inputs.0.content.0.image_url.url", dataURL,
			`{"id":"embed_pg","embeddings":{"float":[[0.1,0.2]]},"meta":{"billed_units":{"input_tokens":2}}}`},
		{constant.ChannelTypeGemini, "gemini-embedding-2", "embeddings", "/v1beta/models/gemini-embedding-2:batchEmbedContents", "requests.0.content.parts.0.inlineData.data", png,
			`{"embeddings":[{"values":[0.1,0.2]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				assert.Equal(t, tc.path, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				imageField := tc.imageField
				if call == 2 {
					last := strings.LastIndex(imageField, ".0.")
					imageField = imageField[:last] + ".1." + imageField[last+3:]
					assert.Contains(t, string(body), `"text":"a mountain"`)
				}
				assert.Equal(t, tc.imageValue, gjson.GetBytes(body, imageField).String())
				assert.False(t, gjson.GetBytes(body, "embedding_image").Exists())
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			baseURL := upstream.URL
			if tc.kind == constant.ChannelTypeOpenRouter {
				baseURL += "/api/v1"
			}
			_, _, ch := setupMiMoGateway(t, baseURL, "embedding-key")
			ch.Type, ch.Models = tc.kind, "pg-embed"
			ch.ModelMapping = common.GetPointer(`{"pg-embed":"` + tc.name + `"}`)
			require.NoError(t, ch.Update())
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pg-embed":1}`))
			engine := playgroundTestRouter()
			res := httptest.NewRecorder()
			engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/pg/endpoints?model=pg-embed&group=default", nil))
			require.Equal(t, 200, res.Code, res.Body.String())
			require.True(t, gjson.Get(res.Body.String(), "data."+tc.endpoint+".embedding_image").Bool())
			path := "/pg/protocol/" + tc.endpoint
			body := `{"model":"pg-embed","embedding_image":"` + dataURL + `","body":{}}`
			initialDiskHits := common.GetDiskCacheStats().DiskCacheHits
			res = requestMiMo(t, engine, path, body, "")
			require.Equal(t, 200, res.Code, res.Body.String())
			require.Equal(t, float64(0.1), gjson.Get(res.Body.String(), "data.0.embedding.0").Float())
			res = requestMiMo(t, engine, path, strings.Replace(body, `"body":{}`, `"body":{"input":["a mountain"]}`, 1), "")
			require.Equal(t, 200, res.Code, res.Body.String())
			res = requestMiMo(t, engine, path, strings.Replace(body, png, "bm90IGFuIGltYWdl", 1), "")
			require.Equal(t, 400, res.Code, res.Body.String())
			require.EqualValues(t, 2, calls.Load())
			require.Equal(t, initialDiskHits, common.GetDiskCacheStats().DiskCacheHits)
		})
	}
	_, _, ch := setupMiMoGateway(t, "", "text-key")
	ch.Type, ch.Models = constant.ChannelTypeOpenAI, "text-embedding-3-small"
	require.NoError(t, ch.Update())
	res := requestMiMo(t, playgroundTestRouter(), "/pg/protocol/embeddings",
		`{"model":"text-embedding-3-small","embedding_image":"`+dataURL+`","body":{}}`, "")
	require.Equal(t, 400, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "当前模型没有支持图片嵌入的可用渠道")
}

func TestPlaygroundMultipleEmbeddingImages(t *testing.T) {
	initPlaygroundTestColumns(t)
	const prefix = "data:image/png;base64,"
	encoded := []string{
		base64.StdEncoding.EncodeToString(playgroundTestPNG(t, color.RGBA{R: 250, A: 255})),
		base64.StdEncoding.EncodeToString(playgroundTestPNG(t, color.RGBA{B: 250, A: 255})),
	}
	const openaiVectors = `{"data":[{"index":0,"embedding":[0.1]},{"index":1,"embedding":[0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`
	for _, tc := range []struct {
		kind                    int
		model, mode             string
		first, second, response string
		usesDataURL             bool
		count                   int
	}{
		{constant.ChannelTypeJina, "jina-clip-v2", "separate", "input.0.image", "input.1.image", openaiVectors, false, 2},
		{constant.ChannelTypeOpenRouter, "nvidia/llama-nemotron-embed-vl-1b-v2", "separate", "input.0.content.0.image_url.url", "input.1.content.0.image_url.url", openaiVectors, true, 2},
		{constant.ChannelTypeVolcEngine, "doubao-embedding-vision-250615", "joint", "input.0.image_url.url", "input.1.image_url.url", `{"data":{"embedding":[0.1]},"usage":{"prompt_tokens":2,"total_tokens":2}}`, true, 1},
		{constant.ChannelTypeCohere, "embed-v4.0", "joint", "inputs.0.content.0.image_url.url", "inputs.0.content.1.image_url.url", `{"embeddings":{"float":[[0.1]]},"meta":{"billed_units":{"input_tokens":2}}}`, true, 1},
		{constant.ChannelTypeCohere, "embed-v4.0", "separate", "inputs.0.content.0.image_url.url", "inputs.1.content.0.image_url.url", `{"embeddings":{"float":[[0.1],[0.2]]},"meta":{"billed_units":{"input_tokens":2}}}`, true, 2},
		{constant.ChannelTypeGemini, "gemini-embedding-2", "joint", "requests.0.content.parts.0.inlineData.data", "requests.0.content.parts.1.inlineData.data", `{"embeddings":[{"values":[0.1]}]}`, false, 1},
		{constant.ChannelTypeGemini, "gemini-embedding-2", "separate", "requests.0.content.parts.0.inlineData.data", "requests.1.content.parts.0.inlineData.data", `{"embeddings":[{"values":[0.1]},{"values":[0.2]}]}`, false, 2},
		{constant.ChannelTypeQianwenAI, "qwen3-vl-embedding", "joint", "input.contents.0.image", "input.contents.1.image", `{"output":{"embeddings":[{"index":0,"embedding":[0.1]}]},"usage":{"input_tokens":2,"image_tokens":20,"total_tokens":22}}`, true, 1},
		{constant.ChannelTypeQianwenAI, "qwen3-vl-embedding", "separate", "input.contents.0.image", "input.contents.1.image", `{"output":{"embeddings":[{"index":0,"embedding":[0.1]},{"index":1,"embedding":[0.2]}]},"usage":{"input_tokens":2,"image_tokens":20,"total_tokens":22}}`, true, 2},
	} {
		t.Run(tc.model+"/"+tc.mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				if tc.kind == constant.ChannelTypeQianwenAI {
					assert.Equal(t, "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding", r.URL.Path)
					fusion := gjson.GetBytes(body, "parameters.enable_fusion")
					assert.True(t, fusion.Exists())
					assert.Equal(t, tc.mode == "joint", fusion.Bool())
				}
				for index, path := range []string{tc.first, tc.second} {
					value := encoded[index]
					if tc.usesDataURL {
						value = prefix + value
					}
					assert.Equal(t, value, gjson.GetBytes(body, path).String())
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			_, _, ch := setupMiMoGateway(t, upstream.URL, "embedding-key")
			ch.Type, ch.Models = tc.kind, "pg-image-embed"
			ch.ModelMapping = common.GetPointer(`{"pg-image-embed":"` + tc.model + `"}`)
			require.NoError(t, ch.Update())
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pg-image-embed":1}`))
			endpoint := "embeddings"
			if tc.kind == constant.ChannelTypeCohere {
				endpoint = "cohere-embeddings"
			}
			body, err := common.Marshal(playgroundProtocolRequest{Model: "pg-image-embed", EmbeddingImages: []string{prefix + encoded[0], prefix + encoded[1]}, EmbeddingMode: tc.mode, Body: json.RawMessage(`{}`)})
			require.NoError(t, err)
			res := requestMiMo(t, playgroundTestRouter(), "/pg/protocol/"+endpoint, string(body), "")
			require.Equal(t, 200, res.Code, res.Body.String())
			require.Len(t, gjson.Get(res.Body.String(), "data").Array(), tc.count)
		})
	}
}

func TestPlaygroundModerationAndRerankImageInputs(t *testing.T) {
	initPlaygroundTestColumns(t)
	const image = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aASsAAAAASUVORK5CYII="
	second := "data:image/png;base64," + base64.StdEncoding.EncodeToString(playgroundTestPNG(t, color.RGBA{G: 250, A: 255}))
	for _, tc := range []struct {
		channelType                                              int
		model, endpoint, path, target, body, imagePath, response string
	}{
		{constant.ChannelTypeOpenAI, "omni-moderation-latest", "moderations", "/v1/moderations", "", `{"input":"check"}`, "input.0.image_url.url", `{"id":"mod_pg","results":[{"flagged":false,"categories":{"violence":false},"category_scores":{"violence":0.01}}]}`},
		{constant.ChannelTypeJina, "jina-reranker-m0", "jina-rerank", "/v1/rerank", "", `{"query":"mountains","documents":["a description"]}`, "documents.1.image", `{"results":[{"index":1,"relevance_score":0.9}],"usage":{"total_tokens":2}}`},
		{constant.ChannelTypeJina, "jina-reranker-m0", "jina-rerank", "/v1/rerank", "query", `{"documents":["a description"]}`, "query.image", `{"results":[{"index":1,"relevance_score":0.9}],"usage":{"total_tokens":2}}`},
		{constant.ChannelTypeQianwenAI, "qwen3-vl-rerank", "jina-rerank", "/api/v1/services/rerank/text-rerank/text-rerank", "query", `{"documents":["a description"]}`, "input.query.image", `{"output":{"results":[{"index":0,"relevance_score":0.9}]},"usage":{"total_tokens":2}}`},
	} {
		t.Run(tc.model+"/"+tc.target, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.path, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				expectedImage := image
				if tc.channelType == constant.ChannelTypeJina {
					expectedImage = strings.TrimPrefix(image, "data:image/png;base64,")
				}
				assert.Equal(t, expectedImage, gjson.GetBytes(body, tc.imagePath).String())
				switch tc.channelType {
				case constant.ChannelTypeOpenAI:
					assert.Equal(t, second, gjson.GetBytes(body, "input.1.image_url.url").String())
				case constant.ChannelTypeJina:
					assert.Equal(t, strings.TrimPrefix(second, "data:image/png;base64,"), gjson.GetBytes(body, "documents.2.image").String())
				case constant.ChannelTypeQianwenAI:
					assert.Equal(t, image, gjson.GetBytes(body, "input.documents.1.image").String())
					assert.Equal(t, second, gjson.GetBytes(body, "input.documents.2.image").String())
				}
				assert.False(t, gjson.GetBytes(body, "input_image").Exists())
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			_, _, ch := setupMiMoGateway(t, upstream.URL, "media-key")
			ch.Type, ch.Models = tc.channelType, tc.model
			require.NoError(t, ch.Update())
			ratios, err := common.Marshal(map[string]float64{tc.model: 1})
			require.NoError(t, err)
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(ratios)))
			req := playgroundProtocolRequest{Model: tc.model, InputImages: []string{image, second}, Body: json.RawMessage(tc.body)}
			if tc.target == "query" {
				req.QueryImage = image
			}
			body, err := common.Marshal(req)
			require.NoError(t, err)
			res := requestMiMo(t, playgroundTestRouter(), "/pg/protocol/"+tc.endpoint, string(body), "")
			require.Equal(t, 200, res.Code, res.Body.String())
			require.True(t, gjson.Get(res.Body.String(), "results.0").Exists())
		})
	}
}

func TestPlaygroundSpeechReturnsAudio(t *testing.T) {
	initPlaygroundTestColumns(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/audio/speech", r.URL.Path)
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		assert.Equal(t, "Hello", body["input"])
		assert.Equal(t, "alloy", body["voice"])
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = io.WriteString(w, "RIFF-audio-sample")
	}))
	defer upstream.Close()
	_, _, ch := setupMiMoGateway(t, upstream.URL, "speech-key")
	ch.Type, ch.Models = constant.ChannelTypeOpenAI, "gpt-4o-mini-tts"
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini-tts":1}`))
	res := requestMiMo(t, playgroundTestRouter(), "/pg/protocol/audio-speech", `{"model":"gpt-4o-mini-tts","body":{"input":"Hello","voice":"alloy","response_format":"wav"}}`, "")
	require.Equal(t, 200, res.Code, res.Body.String())
	require.Equal(t, "audio/wav", res.Header().Get("Content-Type"))
	require.Equal(t, "RIFF-audio-sample", res.Body.String())
}

func TestPlaygroundBatchTaskCanBeQueriedByOwner(t *testing.T) {
	initPlaygroundTestColumns(t)
	const result = "https://example.test/batch-result.jsonl"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			assert.Equal(t, gmicloud.TaskRequestsPath, r.URL.Path)
			var body map[string]json.RawMessage
			require.NoError(t, common.DecodeJson(r.Body, &body))
			assert.Equal(t, "gemini-3-flash-preview", gjson.GetBytes(body["payload"], "model").String())
			assert.True(t, gjson.Valid(gjson.GetBytes(body["payload"], "input_data").String()))
			_, _ = io.WriteString(w, `{"request_id":"batch-fixture","model":"Gemini-batch-inference","status":"queued"}`)
			return
		}
		assert.Equal(t, gmicloud.TaskRequestsPath+"/batch-fixture", r.URL.Path)
		_, _ = io.WriteString(w, `{"request_id":"batch-fixture","model":"Gemini-batch-inference","status":"success","outcome":{"media_urls":[{"url":"`+result+`"}]}}`)
	}))
	defer upstream.Close()
	db, _, ch := setupMiMoGateway(t, upstream.URL, "batch-key")
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	ch.Type, ch.Models = constant.ChannelTypeGMICloud, gmicloud.BatchInferenceModel
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"Gemini-batch-inference":0.002}`))
	engine := playgroundTestRouter()
	res := requestMiMo(t, engine, "/pg/protocol/batch-generation", `{"model":"Gemini-batch-inference","body":{"payload":{"model":"gemini-3-flash-preview","input_data":"{\"request\":{\"contents\":[{\"parts\":[{\"text\":\"hi\"}]}]}}"}}}`, "")
	require.Equal(t, 200, res.Code, res.Body.String())
	id := gjson.Get(res.Body.String(), "task_id").String()
	require.NotEmpty(t, id)
	task, exists, err := model.GetByTaskId(1, id)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, constant.TaskActionBatchInference, task.Action)
	require.NoError(t, service.RefreshVideoTask(context.Background(), &taskgmicloud.TaskAdaptor{}, ch, task))
	res = httptest.NewRecorder()
	engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/pg/tasks/"+id, nil))
	require.Equal(t, 200, res.Code, res.Body.String())
	require.Equal(t, "SUCCESS", gjson.Get(res.Body.String(), "data.status").String())
	require.Contains(t, res.Body.String(), result)
}

func TestPlaygroundVideoMultipleImages(t *testing.T) {
	initPlaygroundTestColumns(t)
	first := "data:image/png;base64," + base64.StdEncoding.EncodeToString(playgroundTestPNG(t, color.RGBA{R: 255, A: 255}))
	second := "data:image/png;base64," + base64.StdEncoding.EncodeToString(playgroundTestPNG(t, color.RGBA{B: 255, A: 255}))
	for _, tc := range []struct {
		kind                                              int
		name, path, mode, firstRole, secondRole, response string
	}{
		{constant.ChannelTypeQianwenAI, "wan3.0-video", "/api/v1/services/aigc/video-generation/video-synthesis", "frames", "first_frame", "last_frame", `{"output":{"task_id":"wan-task","task_status":"PENDING"}}`},
		{constant.ChannelTypeQianwenAI, "wan3.0-video", "/api/v1/services/aigc/video-generation/video-synthesis", "reference", "reference_image", "reference_image", `{"output":{"task_id":"wan-task","task_status":"PENDING"}}`},
		{constant.ChannelTypeVolcEngine, "doubao-seedance-2-0-260128", "/api/v3/contents/generations/tasks", "reference", "reference_image", "reference_image", `{"id":"seedance-task","status":"queued"}`},
		{constant.ChannelTypeXai, "grok-imagine-video-1.5", "/v1/videos/generations", "frames", "image", "last_frame", `{"request_id":"grok-task"}`},
		{constant.ChannelTypeOpenRouter, "google/veo-3.1", "/v1/videos", "frames", "first_frame", "last_frame", `{"id":"router-task","status":"pending"}`},
	} {
		t.Run(tc.name+"/"+tc.mode, func(t *testing.T) {
			values := []string{first, second}
			if tc.kind == constant.ChannelTypeOpenRouter {
				values = []string{"https://example.test/first.png", "https://example.test/last.png"}
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.path, r.URL.Path)
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				for i, src := range values {
					key := strconv.Itoa(i)
					var value, role string
					roles := []string{tc.firstRole, tc.secondRole}
					switch tc.kind {
					case constant.ChannelTypeQianwenAI:
						value = gjson.GetBytes(body, "input.media."+key+".url").String()
						role = gjson.GetBytes(body, "input.media."+key+".type").String()
					case constant.ChannelTypeVolcEngine:
						value = gjson.GetBytes(body, "content."+key+".image_url.url").String()
						role = gjson.GetBytes(body, "content."+key+".role").String()
					case constant.ChannelTypeXai:
						value = gjson.GetBytes(body, roles[i]+".url").String()
						role = roles[i]
					case constant.ChannelTypeOpenRouter:
						value = gjson.GetBytes(body, "frame_images."+key+".image_url.url").String()
						role = gjson.GetBytes(body, "frame_images."+key+".frame_type").String()
					}
					assert.Equal(t, src, value)
					assert.Equal(t, roles[i], role)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer upstream.Close()
			db, _, ch := setupMiMoGateway(t, upstream.URL, "video-key")
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			ch.Type, ch.Models = tc.kind, "pg-multi-video"
			ch.ModelMapping = common.GetPointer(`{"pg-multi-video":"` + tc.name + `"}`)
			require.NoError(t, ch.Update())
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"pg-multi-video":0.002}`))
			body, err := common.Marshal(playgroundProtocolRequest{Model: "pg-multi-video", VideoImages: values, VideoImageMode: tc.mode, Body: json.RawMessage(`{"prompt":"blue and red","seconds":"8"}`)})
			require.NoError(t, err)
			res := requestMiMo(t, playgroundTestRouter(), "/pg/protocol/openai-video", string(body), "")
			require.True(t, res.Code >= 200 && res.Code < 300, res.Body.String())
			require.NotContains(t, res.Body.String(), first)
			require.NotContains(t, res.Body.String(), second)
		})
	}
}

func TestPlaygroundVideoTaskCanBeQueriedByOwner(t *testing.T) {
	initPlaygroundTestColumns(t)
	var submissions atomic.Int32
	var queries atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			submissions.Add(1)
			require.Equal(t, "/v1/videos", r.URL.Path)
			var fields map[string]any
			require.NoError(t, common.DecodeJson(r.Body, &fields))
			require.Equal(t, "runway/gen-4.5", fields["model"])
			require.Equal(t, float64(4), fields["duration"])
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"vendor-job","status":"pending"}`)
			return
		}
		queries.Add(1)
		require.Equal(t, "/v1/videos/vendor-job", r.URL.Path)
		_, _ = io.WriteString(w, `{"id":"vendor-job","status":"completed","unsigned_urls":["https://example.test/video.mp4"]}`)
	}))
	defer upstream.Close()
	db, _, channel := setupMiMoGateway(t, upstream.URL, "video-key")
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	channel.Type, channel.Models = constant.ChannelTypeOpenRouter, "pg-video"
	channel.ModelMapping = common.GetPointer(`{"pg-video":"runway/gen-4.5"}`)
	require.NoError(t, channel.Update())
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"pg-video":0.002}`))
	engine := playgroundTestRouter()
	out := requestMiMo(t, engine, "/pg/protocol/openai-video", `{"model":"pg-video","group":"default","body":{"prompt":"A mountain","seconds":"4"}}`, "")
	require.Equal(t, http.StatusAccepted, out.Code, out.Body.String())
	id := gjson.Get(out.Body.String(), "id").String()
	require.True(t, strings.HasPrefix(id, "task_"))
	require.Zero(t, queries.Load())
	status := httptest.NewRecorder()
	engine.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/pg/tasks/"+id, nil))
	require.Equal(t, 200, status.Code, status.Body.String())
	require.Equal(t, "SUCCESS", gjson.Get(status.Body.String(), "data.status").String())
	require.EqualValues(t, 1, submissions.Load())
	require.EqualValues(t, 1, queries.Load())
}

func TestPlaygroundChecksGroupAndTaskOwnership(t *testing.T) {
	db, _, _ := setupMiMoGateway(t, "", "")
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, db.Create(&model.Task{UserId: 2, TaskID: "task_other", Status: model.TaskStatusSuccess}).Error)
	require.NoError(t, db.Create(&model.Task{UserId: 1, TaskID: "task_own", Status: model.TaskStatusSuccess}).Error)
	engine := playgroundTestRouter()
	for path, status := range map[string]int{"/pg/endpoints?group=unauthorized&model=x": 403, "/pg/tasks/task_other": 404, "/pg/tasks/task_own": 200} {
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, status, res.Code, res.Body.String())
	}
}

func TestPlaygroundTranscriptionUpload(t *testing.T) {
	initPlaygroundTestColumns(t)
	cacheDir := t.TempDir()
	oldCache := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Enabled: true, ThresholdMB: 1, MaxSizeMB: 64, Path: cacheDir})
	t.Cleanup(func() { common.SetDiskCacheConfig(oldCache) })
	multipartDir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, multipartDir)
	}
	oldFileLimit := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = oldFileLimit })
	audio := bytes.Repeat([]byte("mock audio"), 200_000)
	initialDiskHits := common.GetDiskCacheStats().DiskCacheHits
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(int64(len(audio))))
		defer r.MultipartForm.RemoveAll()
		require.Equal(t, "whisper-1", r.FormValue("model"))
		part, _, err := r.FormFile("file")
		require.NoError(t, err)
		defer part.Close()
		contents, err := io.ReadAll(part)
		require.NoError(t, err)
		require.True(t, bytes.Equal(audio, contents), "upstream did not receive the original file bytes")
		for _, dir := range []string{cacheDir, multipartDir} {
			files, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, files, "no server-side files should exist during forwarding")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	defer upstream.Close()
	_, _, ch := setupMiMoGateway(t, upstream.URL, "test-upstream-key")
	ch.Type, ch.Models = constant.ChannelTypeOpenAI, "whisper-1"
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"whisper-1":1}`))
	var payload bytes.Buffer
	form := multipart.NewWriter(&payload)
	require.NoError(t, form.WriteField("model", "whisper-1"))
	part, err := form.CreateFormFile("file", "sample.wav")
	require.NoError(t, err)
	_, err = part.Write(audio)
	require.NoError(t, err)
	require.NoError(t, form.Close())
	req := httptest.NewRequest(http.MethodPost, "/pg/protocol/audio-transcription?model=whisper-1&group=default", &payload)
	req.Header.Set("Content-Type", form.FormDataContentType())
	res := httptest.NewRecorder()
	playgroundTestRouter().ServeHTTP(res, req)
	require.Equal(t, 200, res.Code, strings.TrimSpace(res.Body.String()))
	require.Contains(t, res.Body.String(), `"ok"`)
	require.Equal(t, initialDiskHits, common.GetDiskCacheStats().DiskCacheHits)
}

func TestPlaygroundImageModes(t *testing.T) {
	initPlaygroundTestColumns(t)
	cacheDir := t.TempDir()
	oldCache := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Enabled: true, MaxSizeMB: 64, Path: cacheDir})
	t.Cleanup(func() { common.SetDiskCacheConfig(oldCache) })
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aASsAAAAASUVORK5CYII=")
	require.NoError(t, err)
	secondImage := playgroundTestPNG(t, color.RGBA{R: 250, A: 255})
	var generations, edits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/images/generations":
			generations.Add(1)
			var body map[string]json.RawMessage
			require.NoError(t, common.DecodeJson(r.Body, &body))
			require.NotContains(t, body, "image")
			require.NotContains(t, body, "image_edit")
		case "/v1/images/edits":
			call := edits.Add(1)
			require.NoError(t, r.ParseMultipartForm(1<<20))
			defer r.MultipartForm.RemoveAll()
			require.Equal(t, "gpt-image-1", r.PostForm.Get("model"))
			require.Equal(t, "high", r.PostForm.Get("quality"))
			field := "image"
			wanted := [][]byte{image}
			if call == 2 {
				field = "image[]"
				wanted = append(wanted, secondImage)
			}
			files := r.MultipartForm.File[field]
			require.Len(t, files, len(wanted))
			for index, header := range files {
				part, err := header.Open()
				require.NoError(t, err)
				contents, err := io.ReadAll(part)
				part.Close()
				require.NoError(t, err)
				require.Equal(t, wanted[index], contents)
			}
		default:
			t.Errorf("unexpected upstream path %q", r.URL.Path)
		}
		files, err := os.ReadDir(cacheDir)
		require.NoError(t, err)
		require.Empty(t, files)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://example.test/image.png"}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	_, _, ch := setupMiMoGateway(t, upstream.URL, "image-key")
	ch.Type, ch.Models = constant.ChannelTypeOpenAI, "pg-image,dall-e-2,dall-e-3"
	ch.ModelMapping = common.GetPointer(`{"pg-image":"gpt-image-1"}`)
	ch.ParamOverride = common.GetPointer(`{"quality":"high"}`)
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pg-image":1}`))
	engine := playgroundTestRouter()
	const path = "/pg/protocol/image-generation"
	initialDiskHits := common.GetDiskCacheStats().DiskCacheHits
	for name, wantEdit := range map[string]bool{"pg-image": true, "dall-e-3": false} {
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/pg/endpoints?model="+name+"&group=default", nil))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		require.Equal(t, wantEdit, gjson.Get(res.Body.String(), "data.image-generation.image_edit").Bool())
		require.Equal(t, wantEdit, gjson.Get(res.Body.String(), "data.image-generation.upload").Bool())
	}
	res := requestMiMo(t, engine, path, `{"model":"pg-image","image_edit":false,"body":{"prompt":"draw"}}`, "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	for _, body := range []string{
		`{"model":"pg-image","image_edit":true,"body":{"prompt":"edit"}}`,
		`{"model":"pg-image","image_edit":false,"body":{"prompt":"draw","image":"https://example.test/ref.png"}}`,
	} {
		res := requestMiMo(t, engine, path, body, "")
		require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
	}
	for _, tc := range []struct {
		name    string
		content [][]byte
		status  int
	}{
		{"pg-image", [][]byte{image}, http.StatusOK},
		{"pg-image", [][]byte{image, secondImage}, http.StatusOK},
		{"pg-image", nil, http.StatusBadRequest},
		{"pg-image", [][]byte{image, []byte("not an image")}, http.StatusBadRequest},
		{"dall-e-3", [][]byte{image}, http.StatusBadRequest},
		{"dall-e-2", [][]byte{image, secondImage}, http.StatusBadRequest},
	} {
		var payload bytes.Buffer
		form := multipart.NewWriter(&payload)
		require.NoError(t, form.WriteField("model", tc.name))
		require.NoError(t, form.WriteField("prompt", "change the color"))
		field := "image"
		if len(tc.content) > 1 {
			field = "image[]"
		}
		for index, content := range tc.content {
			part, err := form.CreateFormFile(field, strconv.Itoa(index)+".png")
			require.NoError(t, err)
			_, err = part.Write(content)
			require.NoError(t, err)
		}
		require.NoError(t, form.Close())
		req := httptest.NewRequest(http.MethodPost, path+"?model="+tc.name+"&group=default", &payload)
		req.Header.Set("Content-Type", form.FormDataContentType())
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		require.Equal(t, tc.status, res.Code, res.Body.String())
	}
	require.EqualValues(t, 1, generations.Load())
	require.EqualValues(t, 2, edits.Load())
	require.Equal(t, initialDiskHits, common.GetDiskCacheStats().DiskCacheHits)
}

func TestPlaygroundImageEditProviderFormats(t *testing.T) {
	initPlaygroundTestColumns(t)
	images := [][]byte{
		playgroundTestPNG(t, color.RGBA{R: 255, A: 255}),
		playgroundTestPNG(t, color.RGBA{B: 255, A: 255}),
	}
	for _, tc := range []struct {
		kind                       int
		model, path, first, second string
	}{
		{constant.ChannelTypeVolcEngine, "doubao-seedream-5-5-261028", "/api/v3/images/generations", "image.0", "image.1"},
		{constant.ChannelTypeXai, "grok-imagine-image-2.0", "/v1/images/edits", "images.0.url", "images.1.url"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.path, r.URL.Path)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				for i, path := range []string{tc.first, tc.second} {
					assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(images[i]), gjson.GetBytes(body, path).String())
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"url":"https://example.test/edited.png"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
			}))
			defer upstream.Close()
			_, _, ch := setupMiMoGateway(t, upstream.URL, "image-key")
			ch.Type, ch.Models = tc.kind, "pg-multi-edit"
			ch.ModelMapping = common.GetPointer(`{"pg-multi-edit":"` + tc.model + `"}`)
			require.NoError(t, ch.Update())
			require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pg-multi-edit":1}`))
			var payload bytes.Buffer
			form := multipart.NewWriter(&payload)
			require.NoError(t, form.WriteField("model", "pg-multi-edit"))
			require.NoError(t, form.WriteField("prompt", "combine these two photos"))
			for i, data := range images {
				part, err := form.CreateFormFile("image[]", strconv.Itoa(i)+".png")
				require.NoError(t, err)
				_, err = part.Write(data)
				require.NoError(t, err)
			}
			require.NoError(t, form.Close())
			req := httptest.NewRequest(http.MethodPost, "/pg/protocol/image-generation?group=default&model=pg-multi-edit", &payload)
			req.Header.Set("Content-Type", form.FormDataContentType())
			res := httptest.NewRecorder()
			playgroundTestRouter().ServeHTTP(res, req)
			require.Equal(t, http.StatusOK, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), "edited.png")
		})
	}
}

func TestPlaygroundBase64UsesMemoryEvenWithDiskCacheEnabled(t *testing.T) {
	oldCache := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{Enabled: true, ThresholdMB: 0, MaxSizeMB: 64, Path: t.TempDir()})
	t.Cleanup(func() { common.SetDiskCacheConfig(oldCache) })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions", strings.NewReader(`{"data":"dGVzdA=="}`))
	defer common.CleanupBodyStorage(c)
	defer service.CleanupFileSources(c)
	body, err := common.GetBodyStorage(c)
	require.NoError(t, err)
	require.False(t, body.IsDisk())
	cache, err := service.LoadFileSource(c, types.NewBase64FileSource("dGVzdA==", "application/octet-stream"))
	require.NoError(t, err)
	require.False(t, cache.IsDisk())
}

func TestPlaygroundGMICloudImageNeverPersistsSubmissionBody(t *testing.T) {
	initPlaygroundTestColumns(t)
	const reference = "https://example.test/private-reference-marker.png"
	const anotherReference = "https://example.test/another-reference.png"
	const result = "https://example.test/generated.png"
	started := make(chan struct{})
	finish := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		assert.Equal(t, gmicloud.TaskRequestsPath, r.URL.Path)
		data, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}
		assert.Contains(t, string(data), reference)
		assert.Contains(t, string(data), anotherReference)
		close(started)
		<-finish
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"request_id":"gmi-image","model":"hy-image-v3.5-preview","status":"success","request":{"image":"`+reference+`"},"outcome":{"media_urls":[{"url":"`+result+`","type":"image"}]}}`)
	}))
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
		upstream.Close()
	}()
	db, _, _ := setupGMICloudImageGateway(t, upstream.URL)
	engine := playgroundTestRouter()
	body := `{"model":"hy-alias","group":"default","image_edit":true,"reference_urls":["` + reference + `","` + anotherReference + `"],"body":{"prompt":"draw"}}`
	out := requestMiMo(t, engine, "/pg/protocol/image-generation", body, "")
	require.Equal(t, 200, out.Code, out.Body.String())
	id := gjson.Get(out.Body.String(), "task_id").String()
	require.NotEmpty(t, id)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("GMICloud request was not forwarded from memory")
	}
	queued, exists, err := model.GetByTaskId(1, id)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, constant.TaskActionImageEdit, queued.Action)
	require.Empty(t, queued.PrivateData.GMICloudImageRequest)
	require.Empty(t, queued.PrivateData.UpstreamTaskID)
	require.NotContains(t, string(queued.Data), reference)
	close(finish)
	require.Eventually(t, func() bool {
		var completed model.Task
		if err := db.Where("task_id = ?", id).First(&completed).Error; err != nil || completed.Status != model.TaskStatusSuccess {
			return false
		}
		assert.Empty(t, completed.PrivateData.GMICloudImageRequest)
		assert.NotContains(t, string(completed.Data), reference)
		assert.Contains(t, string(completed.Data), result)
		return true
	}, 3*time.Second, 5*time.Millisecond)
}
