package vertex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func inferenceContext(path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}
func mockInferenceInfo(t *testing.T, name, base string) *relaycommon.RelayInfo {
	t.Helper()
	creds, key := adaptorTestCredentialsJSON(t)
	info := vertexRelayInfo(key, name, `{"default":"us-central1"}`)
	info.ChannelBaseUrl = base
	info.ChannelType = constant.ChannelTypeVertexAi
	accessTokenCache.Store(accessTokenCacheKey(creds, info), cachedAccessToken{Token: "test-token", ExpiresAt: time.Now().Add(time.Hour)})
	t.Cleanup(func() { accessTokenCache.Delete(accessTokenCacheKey(creds, info)) })
	service.InitHttpClient()
	return info
}
func TestEmbeddingFanoutOrderingEncodingAndUsage(t *testing.T) {
	for _, name := range []string{"gemini-embedding-001", "gemini-embedding-2"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				w.Header().Set("Content-Type", "application/json")
				if name == "gemini-embedding-001" {
					require.True(t, strings.HasSuffix(r.URL.Path, ":predict"))
					params := body["parameters"].(map[string]any)
					require.Equal(t, false, params["autoTruncate"])
					require.EqualValues(t, 2, params["outputDimensionality"])
					instance := body["instances"].([]any)[0].(map[string]any)
					require.Equal(t, []string{"one", "two"}[calls-1], instance["content"])
					_, _ = io.WriteString(w, `{"predictions":[{"embeddings":{"values":[1.5,-2],"statistics":{"token_count":3,"truncated":false}}}]}`)
				} else {
					require.True(t, strings.HasSuffix(r.URL.Path, ":embedContent"))
					require.Equal(t, false, body["embedContentConfig"].(map[string]any)["autoTruncate"])
					_, _ = io.WriteString(w, `{"embedding":{"values":[1.5,-2]},"truncated":false,"usageMetadata":{"promptTokenCount":3,"totalTokenCount":3}}`)
				}
			}))
			defer server.Close()
			info := mockInferenceInfo(t, name, server.URL+"/proxy")
			a := &Adaptor{}
			a.Init(info)
			c, w := inferenceContext("/v1/embeddings", "")
			body, err := a.ConvertEmbeddingRequest(c, info, dto.EmbeddingRequest{Input: []string{"one", "two"}, Dimensions: common.GetPointer(2), AutoTruncate: common.GetPointer(false), EncodingFormat: "base64", TaskType: "RETRIEVAL_DOCUMENT"})
			require.NoError(t, err)
			data, err := common.Marshal(body)
			require.NoError(t, err)
			resp, err := a.DoRequest(c, info, bytes.NewReader(data))
			require.NoError(t, err)
			u, apiErr := a.DoResponse(c, resp.(*http.Response), info)
			require.Nil(t, apiErr)
			require.Equal(t, 6, u.(*dto.Usage).PromptTokens)
			require.Equal(t, 2, calls)
			require.True(t, c.GetBool("vertex_batch_started"))
			var result struct {
				Data []struct {
					Index     int    `json:"index"`
					Embedding string `json:"embedding"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
			require.Len(t, result.Data, 2)
			require.Equal(t, 1, result.Data[1].Index)
			decoded, err := base64.StdEncoding.DecodeString(result.Data[0].Embedding)
			require.NoError(t, err)
			require.Equal(t, float32(1.5), math.Float32frombits(binary.LittleEndian.Uint32(decoded)))
		})
	}
}

func TestEmbeddingFailureNeverPublishesPartialBatch(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"message":"busy"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"embedding":{"values":[1]},"usageMetadata":{"promptTokenCount":1}}`)
	}))
	defer server.Close()
	info := mockInferenceInfo(t, "gemini-embedding-2", server.URL)
	a := &Adaptor{}
	a.Init(info)
	c, w := inferenceContext("/v1/embeddings", "")
	body, err := a.ConvertEmbeddingRequest(c, info, dto.EmbeddingRequest{Input: []string{"one", "two", "three"}})
	require.NoError(t, err)
	data, _ := common.Marshal(body)
	resp, err := a.DoRequest(c, info, bytes.NewReader(data))
	require.NoError(t, err)
	defer resp.(*http.Response).Body.Close()
	require.Equal(t, 429, resp.(*http.Response).StatusCode)
	require.Equal(t, 2, calls)
	require.Empty(t, w.Body.String())
	require.True(t, c.GetBool("vertex_batch_started"))
	cancelled, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(cancelled)
	_, err = a.DoRequest(c, info, bytes.NewReader(data))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 2, calls)
	for _, input := range []any{[]int{1, 2}, []any{"one", 2}, "", []string{}} {
		_, err := a.ConvertEmbeddingRequest(c, info, dto.EmbeddingRequest{Input: input})
		require.Error(t, err)
	}
}

func TestNativeFieldsDispatchAndExpressRestrictions(t *testing.T) {
	info := mockInferenceInfo(t, "publishers/mistralai/models/codestral-2", "https://gateway.example/prefix")
	a := &Adaptor{}
	a.Init(info)
	c, _ := inferenceContext("/v1/fim/completions", `{"model":"alias","prompt":"def x():","suffix":"end","temperature":0,"stream":false,"vendorExtension":{"enabled":false}}`)
	r, err := dto.ParseVertexRequest(c)
	require.NoError(t, err)
	body, err := a.ConvertNativeRequest(c, info, r)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Contains(t, string(data), `"temperature":0`)
	require.Contains(t, string(data), `"enabled":false`)
	require.Contains(t, string(data), `"suffix":"end"`)
	url, err := a.GetRequestURL(info)
	require.NoError(t, err)
	require.Contains(t, url, "/publishers/mistralai/models/codestral-2:rawPredict")
	r.Action = "delete"
	_, err = a.ConvertNativeRequest(c, info, r)
	require.Error(t, err)
	c, _ = inferenceContext("/v1/models/gemini-2.5-flash:countTokens", "")
	info = vertexRelayInfo("api-key", "gemini-2.5-flash", `{"default":"global"}`)
	info.ChannelOtherSettings.VertexKeyType = dto.VertexKeyTypeAPIKey
	a = &Adaptor{}
	a.Init(info)
	r = &dto.VertexRequest{Model: info.UpstreamModelName, Action: "countTokens", Fields: map[string]json.RawMessage{"contents": raw([]any{})}}
	_, err = a.ConvertNativeRequest(c, info, r)
	require.NoError(t, err)
	url, err = a.GetRequestURL(info)
	require.NoError(t, err)
	require.Contains(t, url, "?key=api-key")
	r.Action = "computeTokens"
	_, err = a.ConvertNativeRequest(c, info, r)
	require.Error(t, err)
}

func TestNativeSSEPreservesEventsAndAnthropicUsage(t *testing.T) {
	info := mockInferenceInfo(t, "anthropic/claude-opus-4-6", "")
	info.IsStream = true
	a := &Adaptor{}
	a.Init(info)
	a.native = true
	stream := "event: message_start\ndata: {\"message\":{\"usage\":{\"input_tokens\":5,\"cache_read_input_tokens\":2}}}\n\nevent: message_delta\ndata: {\"usage\":{\"output_tokens\":7}}\n\n"
	c, w := inferenceContext("/v1/models/claude-opus-4-6:streamRawPredict", "")
	u, err := a.nativeResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(stream))}, info)
	require.Nil(t, err)
	require.Equal(t, stream, w.Body.String())
	require.Equal(t, 5, u.PromptTokens)
	require.Equal(t, 7, u.CompletionTokens)
	require.Equal(t, 2, u.PromptTokensDetails.CachedTokens)
	require.Equal(t, "anthropic", u.UsageSemantic)
}

func TestGrokResponsesIsStatelessAfterOverrides(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.True(t, strings.HasSuffix(r.URL.Path, "/endpoints/openapi/responses"))
		var body map[string]any
		require.NoError(t, common.DecodeJson(r.Body, &body))
		require.Equal(t, false, body["store"])
		require.Equal(t, "xai/grok-4", body["model"])
		_, _ = io.WriteString(w, `{"id":"r","object":"response","output":[],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3}}`)
	}))
	defer server.Close()
	info := mockInferenceInfo(t, "xai/grok-4", server.URL)
	info.RelayMode = relayconstant.RelayModeResponses
	a := &Adaptor{}
	a.Init(info)
	c, _ := inferenceContext("/v1/responses", "")
	resp, err := a.DoRequest(c, info, strings.NewReader(`{"model":"other","input":"hi"}`))
	require.NoError(t, err)
	resp.(*http.Response).Body.Close()
	require.Equal(t, 1, calls)
	for _, body := range []string{`{"store":true}`, `{"background":true}`, `{"previous_response_id":"x"}`, `{"context_management":[{"type":"compaction"}]}`} {
		_, err := a.DoRequest(c, info, strings.NewReader(body))
		require.Error(t, err)
	}
	require.Equal(t, 1, calls)
	info.UpstreamModelName = "gemini-2.5-flash"
	a.Init(info)
	_, err = a.GetRequestURL(info)
	require.Error(t, err)
}

func TestImageConversionAndActualResults(t *testing.T) {
	info := mockInferenceInfo(t, "imagen-3.0-capability-001", "")
	info.PriceData = types.PriceData{UsePrice: true}
	info.RelayMode = relayconstant.RelayModeImagesEdits
	a := &Adaptor{}
	a.Init(info)
	c, w := inferenceContext("/v1/images/edits", "")
	r := dto.ImageRequest{Prompt: "replace", Image: raw("data:image/png;base64,AQID"), Mask: raw("data:image/png;base64,BAUG"), N: common.GetPointer(uint(2)), ExtraFields: raw(map[string]any{"parameters": map[string]any{"seed": 0, "addWatermark": false}}), ResponseFormat: "url"}
	body, err := a.ConvertImageRequest(c, info, r)
	require.NoError(t, err)
	data, _ := common.Marshal(body)
	require.Contains(t, string(data), `"REFERENCE_TYPE_MASK"`)
	require.Contains(t, string(data), `"seed":0`)
	require.Contains(t, string(data), `"addWatermark":false`)
	response := `{"predictions":[{"bytesBase64Encoded":"AQID","mimeType":"image/png"},{"raiFilteredReason":"filtered"}]}`
	u, apiErr := a.imageResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(response))}, info)
	require.Nil(t, apiErr)
	require.Zero(t, u.TotalTokens)
	require.Equal(t, 1.0, info.PriceData.OtherRatios["n"])
	require.True(t, info.NonTokenUsage)
	require.Contains(t, w.Body.String(), "data:image/png;base64,AQID")
	c, _ = inferenceContext("/v1/images/generations", "")
	_, apiErr = a.imageResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(`{"predictions":[{"raiFilteredReason":"filtered"}]}`))}, info)
	require.NotNil(t, apiErr)
	info.UpstreamModelName = "gemini-2.5-flash-image"
	a.Init(info)
	r.Mask = nil
	r.N = nil
	_, err = a.ConvertImageRequest(c, info, r)
	require.NoError(t, err)
}

func TestSpeechFormatsAndStreaming(t *testing.T) {
	pcm := []byte{1, 0, 2, 0}
	wav, mime, err := convertAudioBytes(pcm, "audio/L16;codec=pcm;rate=24000", "wav")
	require.NoError(t, err)
	require.Equal(t, "audio/wav", mime)
	require.Equal(t, "RIFF", string(wav[:4]))
	require.EqualValues(t, 24000, binary.LittleEndian.Uint32(wav[24:28]))
	roundtrip, _, err := convertAudioBytes(wav, mime, "pcm")
	require.NoError(t, err)
	require.Equal(t, pcm, roundtrip)
	info := mockInferenceInfo(t, "gemini-2.5-flash-tts", "")
	info.RelayMode = relayconstant.RelayModeAudioSpeech
	a := &Adaptor{}
	a.Init(info)
	c, w := inferenceContext("/v1/audio/speech", "")
	request := dto.AudioRequest{Input: "hello", Voice: dto.AudioVoice{Name: "Kore"}}
	body, err := a.ConvertAudioRequest(c, info, request)
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	require.Contains(t, string(data), `"responseModalities":["AUDIO"]`)
	require.Equal(t, "wav", a.audioFormat)
	request.ResponseFormat = "mp3"
	_, err = a.ConvertAudioRequest(c, info, request)
	require.Error(t, err)
	request.ResponseFormat = "pcm"
	request.Stream = common.GetPointer(true)
	_, err = a.ConvertAudioRequest(c, info, request)
	require.NoError(t, err)
	stream := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"inlineData\":{\"data\":\"AQACAA==\",\"mimeType\":\"audio/L16;rate=24000\"}}]}}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":4,\"totalTokenCount\":6}}\n\n"
	u, apiErr := a.audioResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(stream))}, info)
	require.Nil(t, apiErr)
	require.Equal(t, pcm, w.Body.Bytes())
	require.Equal(t, 6, u.TotalTokens)
	request.Speed = common.GetPointer(0.0)
	_, err = a.ConvertAudioRequest(c, info, request)
	require.Error(t, err)
}
