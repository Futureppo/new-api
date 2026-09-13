package relay

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type mistralBillingRecorder struct{ calls int }

func (b *mistralBillingRecorder) Settle(int) error       { b.calls++; return nil }
func (*mistralBillingRecorder) Refund(*gin.Context)      {}
func (*mistralBillingRecorder) NeedsRefund() bool        { return false }
func (*mistralBillingRecorder) GetPreConsumedQuota() int { return 0 }
func (*mistralBillingRecorder) Reserve(int) error        { return nil }

func setupMistralPipeline(t *testing.T) *gorm.DB {
	t.Helper()
	service.InitHttpClient()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousSQLite, previousBatch, previousLog, previousRedis, previousDebug := common.UsingSQLite, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.RedisEnabled, common.DebugEnabled
	previousTimeout := constant.StreamingTimeout
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	common.RedisEnabled = false
	common.DebugEnabled = false
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.UsingSQLite = previousSQLite
		common.BatchUpdateEnabled = previousBatch
		common.LogConsumeEnabled = previousLog
		common.RedisEnabled = previousRedis
		common.DebugEnabled = previousDebug
		constant.StreamingTimeout = previousTimeout
		sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Log{}))
	require.NoError(t, db.Create(&model.User{Id: 9001, Username: "mistral-test"}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 9001, Name: "mistral-test", Type: constant.ChannelTypeMistral}).Error)
	return db
}

func mistralGatewayRequest(t *testing.T, path, contentType string, body []byte, base, key, upstreamModel string, settings dto.ChannelSettings, overrides map[string]any) (*httptest.ResponseRecorder, *relaycommon.RelayInfo, *types.NewAPIError) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	defer common.CleanupBodyStorage(c)
	c.Request = httptest.NewRequest("POST", path, bytes.NewReader(body))
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	c.Request.Header.Set("Content-Type", contentType)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeMistral)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, base)
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelId, 9001)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "test-alias")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, settings)
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, overrides)
	mapping, err := common.Marshal(map[string]string{"test-alias": upstreamModel})
	require.NoError(t, err)
	c.Set("model_mapping", string(mapping))
	format := types.RelayFormatOpenAI
	mode := relayconstant.Path2RelayMode(path)
	switch mode {
	case relayconstant.RelayModeEmbeddings:
		format = types.RelayFormatEmbedding
	case relayconstant.RelayModeAudioTranscription:
		format = types.RelayFormatOpenAIAudio
	}
	if mistral.UsesNativeProtocol(c) {
		format = types.RelayFormatMistralNative
	}
	request, err := helper.GetAndValidateRequest(c, format)
	require.NoError(t, err)
	billing := &mistralBillingRecorder{}
	info := &relaycommon.RelayInfo{Request: request, OriginModelName: "test-alias", RequestURLPath: path, RelayMode: mode, RelayFormat: format, IsStream: request.IsStream(c), StartTime: time.Now(), DisablePing: true, UserId: 9001, UserQuota: 1000000000, UsingGroup: "default", Billing: billing, PriceData: types.PriceData{ModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	if format == types.RelayFormatMistralNative && mistral.RequiresCallPrice(path) {
		info.PriceData.UsePrice, info.PriceData.ModelPrice = true, 0.001
	}
	var apiErr *types.NewAPIError
	if format == types.RelayFormatMistralNative {
		apiErr = MistralNativeHelper(c, info)
	} else {
		switch mode {
		case relayconstant.RelayModeEmbeddings:
			apiErr = EmbeddingHelper(c, info)
		case relayconstant.RelayModeAudioTranscription:
			apiErr = AudioHelper(c, info)
		default:
			apiErr = TextHelper(c, info)
		}
	}
	if apiErr == nil {
		require.Equal(t, 1, billing.calls, "request must settle exactly once")
	}
	return w, info, apiErr
}

func mistralWAV() []byte {
	const size = 64000
	data := make([]byte, 44+size)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 16000)
	binary.LittleEndian.PutUint32(data[28:], 32000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], size)
	return data
}

func mistralMultipart(t *testing.T, fields map[string]string, audio []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, writer.WriteField(k, v))
	}
	file, err := writer.CreateFormFile("file", "probe.wav")
	require.NoError(t, err)
	_, err = file.Write(audio)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func TestMistralGatewayPipeline(t *testing.T) {
	db := setupMistralPipeline(t)
	received := make(chan map[string]any, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/audio/transcriptions" {
			if r.ParseMultipartForm(1<<20) != nil {
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			received <- map[string]any{"model": r.FormValue("model"), "temperature": r.FormValue("temperature"), "file_count": len(r.MultipartForm.File["file"])}
			_, _ = io.WriteString(w, `{"text":"hello","segments":[{"text":"hello","start":0.1,"end":0.5}],"usage":{"prompt_tokens":6,"completion_tokens":17,"total_tokens":398,"prompt_tokens_details":{"audio_tokens":375}}}`)
			return
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = common.Unmarshal(data, &body)
		received <- body
		switch r.URL.Path {
		case "/v1/embeddings":
			_, _ = io.WriteString(w, `{"object":"list","model":"codestral-embed","data":[{"index":0,"object":"embedding","embedding":[1,2]}],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`)
		default:
			if body["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+`{"id":"c","object":"chat.completion.chunk","created":123,"model":"ministral-3b-latest","choices":[{"index":0,"delta":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`+"\n\ndata: [DONE]\n\n")
			} else {
				_, _ = io.WriteString(w, `{"id":"c","object":"chat.completion","model":"ministral-3b-latest","created":123,"choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"thinking","thinking":"reason"},{"type":"text","text":"hello"}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
			}
		}
	}))
	defer upstream.Close()
	for _, stream := range []bool{false, true} {
		body, err := common.Marshal(map[string]any{"model": "test-alias", "messages": []any{map[string]any{"role": "user", "content": "hi"}}, "seed": 0, "stream": stream, "stream_options": map[string]any{"include_usage": true}})
		require.NoError(t, err)
		w, _, apiErr := mistralGatewayRequest(t, "/v1/chat/completions", "application/json", body, upstream.URL+"/v1/", "test-key", "ministral-3b-latest", dto.ChannelSettings{SystemPrompt: "channel prompt", ForceFormat: true}, map[string]any{"temperature": 0})
		require.Nil(t, apiErr)
		require.Contains(t, w.Body.String(), "hello")
		captured := <-received
		require.Equal(t, "ministral-3b-latest", captured["model"])
		require.Equal(t, float64(0), captured["random_seed"])
		require.Equal(t, float64(0), captured["temperature"])
		require.NotContains(t, captured, "stream_options")
		first := captured["messages"].([]any)[0].(map[string]any)
		require.Equal(t, "system", first["role"])
		require.Equal(t, "channel prompt", first["content"])
		if stream {
			require.Equal(t, 1, strings.Count(w.Body.String(), "[DONE]"))
		} else {
			require.Contains(t, w.Body.String(), "reasoning_content")
		}
	}
	w, _, apiErr := mistralGatewayRequest(t, "/v1/embeddings", "application/json", []byte(`{"model":"test-alias","input":"hello","dimensions":2,"encoding_format":"base64"}`), upstream.URL, "test-key", "codestral-embed", dto.ChannelSettings{ForceFormat: true}, nil)
	require.Nil(t, apiErr)
	require.Contains(t, w.Body.String(), "AACAPwAAAEA=")
	require.Equal(t, float64(2), (<-received)["output_dimension"])
	body, contentType := mistralMultipart(t, map[string]string{"model": "test-alias", "response_format": "srt", "temperature": "0"}, mistralWAV())
	w, _, apiErr = mistralGatewayRequest(t, "/v1/audio/transcriptions", contentType, body, upstream.URL, "test-key", "voxtral-mini-latest", dto.ChannelSettings{}, nil)
	require.Nil(t, apiErr)
	require.Contains(t, w.Body.String(), "00:00:00,100 --> 00:00:00,500")
	captured := <-received
	require.Equal(t, "voxtral-mini-latest", captured["model"])
	require.Equal(t, "0", captured["temperature"])
	require.Equal(t, 1, captured["file_count"])
	var logs []model.Log
	require.NoError(t, db.Order("id").Find(&logs).Error)
	require.Len(t, logs, 4)
	require.Equal(t, 381, logs[3].PromptTokens)
	require.Equal(t, 17, logs[3].CompletionTokens)
}

func TestMistralGatewayUsagePolicyAndErrors(t *testing.T) {
	setupMistralPipeline(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := common.DecodeJson(r.Body, &request); err != nil {
			w.WriteHeader(400)
			return
		}
		if request["model"] == "restricted" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"object":"error","message":"tier restricted","type":"tier_not_allowed","code":"1910"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+`{"id":"c","object":"chat.completion.chunk","created":123,"model":"m","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`+"\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	for _, option := range []*bool{nil, common.GetPointer(true), common.GetPointer(false)} {
		payload := map[string]any{"model": "test-alias", "stream": true, "messages": []any{map[string]any{"role": "user", "content": "hi"}}}
		if option != nil {
			payload["stream_options"] = map[string]any{"include_usage": *option}
		}
		body, err := common.Marshal(payload)
		require.NoError(t, err)
		w, _, apiErr := mistralGatewayRequest(t, "/v1/chat/completions", "application/json", body, upstream.URL, "test-key", "m", dto.ChannelSettings{}, nil)
		require.Nil(t, apiErr)
		require.Equal(t, option == nil || *option, strings.Contains(w.Body.String(), `"usage":`))
		require.Equal(t, 1, strings.Count(w.Body.String(), "[DONE]"))
	}
	w, _, apiErr := mistralGatewayRequest(t, "/v1/chat/completions", "application/json", []byte(`{"model":"test-alias","messages":[{"role":"user","content":"hi"}]}`), upstream.URL, "test-key", "restricted", dto.ChannelSettings{}, nil)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, "tier_not_allowed", apiErr.ToOpenAIError().Type)
	require.Equal(t, "1910", apiErr.ToOpenAIError().Code)
	require.Empty(t, w.Body.String(), "failed upstream must not be emitted as a completion")
}
