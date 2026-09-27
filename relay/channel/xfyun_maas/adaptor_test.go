package xfyun_maas

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func testInfo(t *testing.T, format types.RelayFormat, mode int, stream bool) *relaycommon.RelayInfo {
	t.Helper()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 5
	t.Cleanup(func() { constant.StreamingTimeout = old })
	return &relaycommon.RelayInfo{
		RelayFormat: format, RelayMode: mode, IsStream: stream, DisablePing: true,
		OriginModelName: "public-model", StartTime: time.Now(), ShouldIncludeUsage: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeXunfeiMaas, ApiType: constant.APITypeXunfeiMaas,
			ChannelBaseUrl: DefaultBaseURL, ApiKey: "fixture-key", UpstreamModelName: "xopglm53",
			SupportStreamOptions: true,
		},
	}
}

func testContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c, w
}

func marshal(t *testing.T, value any) string {
	t.Helper()
	b, err := common.Marshal(value)
	require.NoError(t, err)
	return string(b)
}

func response(body string, stream bool) *http.Response {
	contentType := "application/json"
	if stream {
		contentType = "text/event-stream"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEndpointsAndAuthentication(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name   string
		format types.RelayFormat
		mode   int
		path   string
	}{
		{"chat", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, "/v2/chat/completions"},
		{"messages", types.RelayFormatClaude, relayconstant.RelayModeUnknown, "/anthropic/v1/messages"},
		{"responses", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, "/v1/responses"},
		{"images", types.RelayFormatOpenAIImage, relayconstant.RelayModeImagesGenerations, "/v2.1/tti"},
		{"embedding", types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings, "/v2/embeddings"},
		{"rerank", types.RelayFormatRerank, relayconstant.RelayModeRerank, "/v2/rerank"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var path, auth, key, version string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, auth, key, version = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"code":11200,"message":"fixture denied"}`))
			}))
			defer server.Close()
			info := testInfo(t, tc.format, tc.mode, false)
			info.ChannelBaseUrl = server.URL + "/proxy/v2/"
			c, _ := testContext()
			a := &Adaptor{}
			res, err := a.DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			require.Equal(t, "/proxy"+tc.path, path)
			if tc.format == types.RelayFormatClaude {
				require.Empty(t, auth)
				require.Equal(t, "fixture-key", key)
				require.Equal(t, "2023-06-01", version)
			} else {
				require.Equal(t, "Bearer fixture-key", auth)
			}
			resp := res.(*http.Response)
			require.Equal(t, 403, resp.StatusCode)
			body, err := readBody(resp)
			require.NoError(t, err)
			require.Contains(t, string(body), "fixture denied")
			require.Contains(t, string(body), `"error"`)
		})
	}
	info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeImagesEdits, false)
	_, err := (&Adaptor{}).GetRequestURL(info)
	require.Error(t, err)
}

func TestChatVisionAndZeroValues(t *testing.T) {
	a := &Adaptor{}
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"xopglm53","temperature":0,"top_p":0,"max_tokens":0,"stream":false,"enable_thinking":false,"stream_options":{"include_usage":false},"messages":[{"role":"user","content":"hi"}]}`, &req))
	v, err := a.ConvertOpenAIRequest(nil, nil, &req)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"xopglm53","temperature":0,"top_p":0,"max_completion_tokens":0,"stream":false,"enable_thinking":false,"stream_options":{"include_usage":false},"messages":[{"role":"user","content":"hi"}]}`, marshal(t, v))
	for _, image := range []string{"https://example.com/image.png", "data:image/png;base64,aQ=="} {
		req.Model = "opaque-vision-id"
		req.Messages = []dto.Message{{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": map[string]string{"url": image}}}}}
		v, err = a.ConvertOpenAIRequest(nil, nil, &req)
		require.NoError(t, err)
		got := marshal(t, v)
		require.Contains(t, got, `"model":"opaque-vision-id"`)
		require.Contains(t, got, image)
		require.Contains(t, got, `"include_usage":false`)
		require.Contains(t, got, `"max_tokens":0`)
	}
}

func TestNativeRequests(t *testing.T) {
	a := &Adaptor{}
	var req dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"xopglm53","max_tokens":0,"temperature":0,"stream":false,"thinking":{"type":"disabled","display":"omitted"},"messages":[{"role":"user","content":"hi"}]}`, &req))
	got, err := a.ConvertClaudeRequest(nil, nil, &req)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"xopglm53","max_tokens":0,"temperature":0,"stream":false,"enable_thinking":false,"clear_thinking":true,"messages":[{"role":"user","content":"hi"}]}`, marshal(t, got))
	req.Thinking.BudgetTokens = common.GetPointer(1024)
	_, err = a.ConvertClaudeRequest(nil, nil, &req)
	require.ErrorContains(t, err, "budget_tokens")
	var rr dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"xopglm53","input":"hi","temperature":0,"stream":false,"tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`, &rr))
	got, err = a.ConvertOpenAIResponsesRequest(nil, nil, rr)
	require.NoError(t, err)
	require.Contains(t, marshal(t, got), `"model":"xopglm53"`)
	rr.PreviousResponseID = "previous"
	_, err = a.ConvertOpenAIResponsesRequest(nil, nil, rr)
	require.Error(t, err)
}

func TestImages(t *testing.T) {
	info := testInfo(t, types.RelayFormatOpenAIImage, relayconstant.RelayModeImagesGenerations, false)
	var request dto.ImageRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"image-id","prompt":"画一只猫","seed":0,"num_inference_steps":0,"guidance_scale":0,"negative_prompt":"模糊","response_format":"b64_json"}`, &request))
	a := &Adaptor{}
	converted, err := a.ConvertImageRequest(nil, info, request)
	require.NoError(t, err)
	body := marshal(t, converted)
	for _, fragment := range []string{`"patch_id":["0"]`, `"domain":"image-id"`, `"width":768`, `"seed":0`, `"num_inference_steps":0`, `"guidance_scale":0`, `"negative_prompts":{"text":"模糊"}`} {
		require.Contains(t, body, fragment)
	}
	for _, change := range []func(*dto.ImageRequest){
		func(r *dto.ImageRequest) { r.N = common.GetPointer(uint(0)) },
		func(r *dto.ImageRequest) { r.N = common.GetPointer(uint(2)) },
		func(r *dto.ImageRequest) { r.ResponseFormat = "url" },
		func(r *dto.ImageRequest) { r.Stream = common.GetPointer(true) },
		func(r *dto.ImageRequest) { r.Size = "512x512" },
		func(r *dto.ImageRequest) { r.Prompt = strings.Repeat("猫", 1025) },
	} {
		copy := request
		change(&copy)
		_, err := a.ConvertImageRequest(nil, info, copy)
		require.Error(t, err)
	}
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jZ9kAAAAASUVORK5CYII="
	c, w := testContext()
	upstream := `{"header":{"code":0},"payload":{"choices":{"text":[{"content":"` + png + `"}]}}}`
	_, apiErr := a.DoResponse(c, response(upstream, false), info)
	require.Nil(t, apiErr)
	require.Contains(t, w.Body.String(), `"b64_json":"`+png+`"`)
	for _, bad := range []string{`{"header":{"code":10003,"message":"bad","sid":"s1"}}`, `{"header":{"code":0},"payload":{}}`, `{"payload":{"choices":{"text":[{"content":"bm90LWFuLWltYWdl"}]}}}`} {
		c, w := testContext()
		_, apiErr := a.DoResponse(c, response(bad, false), info)
		require.NotNil(t, apiErr)
		require.Empty(t, w.Body.String())
	}
}

func TestEmbeddingsAndRerank(t *testing.T) {
	a := &Adaptor{}
	_, err := a.ConvertEmbeddingRequest(nil, nil, dto.EmbeddingRequest{Model: "embed", Input: []any{"a", "b"}, EncodingFormat: "base64"})
	require.NoError(t, err)
	c, w := testContext()
	info := testInfo(t, types.RelayFormatEmbedding, relayconstant.RelayModeEmbeddings, false)
	u, e := a.DoResponse(c, response(`{"model":"embed","data":[{"index":0,"embedding":[0,1.5]},{"index":1,"embedding":[-2,0.5]}],"usage":{"prompt_tokens":4,"total_tokens":4}}`, false), info)
	require.Nil(t, e)
	require.Equal(t, 4, u.(*dto.Usage).PromptTokens)
	var parsed struct {
		Data []struct {
			Embedding string `json:"embedding"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &parsed))
	b, err := base64.StdEncoding.DecodeString(parsed.Data[0].Embedding)
	require.NoError(t, err)
	require.Equal(t, float32(1.5), math.Float32frombits(binary.LittleEndian.Uint32(b[4:])))
	for _, input := range []any{[]any{1, 2}, []any{"a", 3}, []string{}, ""} {
		_, err = a.ConvertEmbeddingRequest(nil, nil, dto.EmbeddingRequest{Input: input})
		require.Error(t, err)
	}
	req := dto.RerankRequest{Model: "rank", Query: "q", Documents: []any{"a", "b", "c"}, TopN: common.GetPointer(2), ReturnDocuments: common.GetPointer(true)}
	converted, err := a.ConvertRerankRequest(nil, 0, req)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"rank","query":"q","documents":["a","b","c"]}`, marshal(t, converted))
	c, w = testContext()
	info = testInfo(t, types.RelayFormatRerank, relayconstant.RelayModeRerank, false)
	u, e = a.DoResponse(c, response(`{"results":[{"index":0,"relevance_score":0},{"index":2,"relevance_score":0.8},{"index":1,"relevance_score":0.8}],"usage":{"prompt_tokens":100,"total_tokens":100}}`, false), info)
	require.Nil(t, e)
	require.Equal(t, 100, u.(*dto.Usage).TotalTokens)
	var ranked dto.RerankResponse
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &ranked))
	require.Equal(t, []int{2, 1}, []int{ranked.Results[0].Index, ranked.Results[1].Index})
	require.Contains(t, w.Body.String(), `"document":{"text":"c"}`)
	for _, results := range []string{
		`[{"index":5,"relevance_score":1},{"index":1,"relevance_score":0},{"index":2,"relevance_score":0}]`,
		`[{"index":0},{"index":1,"relevance_score":0},{"index":2,"relevance_score":0}]`,
	} {
		c, _ := testContext()
		_, e = a.DoResponse(c, response(`{"results":`+results+`,"usage":{"total_tokens":1}}`, false), info)
		require.NotNil(t, e)
	}
}

func TestNativeResponsesAndErrors(t *testing.T) {
	for _, tc := range []struct {
		format types.RelayFormat
		mode   int
		body   string
	}{
		{types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, `{"model_id":"xopglm53","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`},
		{types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, `{"id":"r1","model_id":"xopglm53","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`},
		{types.RelayFormatClaude, relayconstant.RelayModeUnknown, `{"id":"m1","model_id":"xopglm53","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":-1}}`},
	} {
		c, w := testContext()
		u, e := (&Adaptor{}).DoResponse(c, response(tc.body, false), testInfo(t, tc.format, tc.mode, false))
		require.Nil(t, e)
		require.Contains(t, w.Body.String(), `"model":"xopglm53"`)
		require.NotContains(t, w.Body.String(), "model_id")
		require.Equal(t, 3, u.(*dto.Usage).TotalTokens)
		require.GreaterOrEqual(t, u.(*dto.Usage).PromptTokensDetails.CachedTokens, 0)
	}
	for _, stream := range []bool{false, true} {
		c, w := testContext()
		_, err := (&Adaptor{}).DoResponse(c, response(`{"code":11202,"message":"rate limit"}`, false), testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, stream))
		require.NotNil(t, err)
		require.Equal(t, 429, err.StatusCode)
		require.Empty(t, w.Body.String())
	}
	data, err := normalizeEnvelope([]byte(`{"model_id":"x","choices":[{"message":{"tool_calls":[{"function":{"arguments":"{\"model_id\":\"keep\"}"}}]}}]}`))
	require.NoError(t, err)
	require.Contains(t, string(data), `model_id\":\"keep`)
}

func TestStreams(t *testing.T) {
	chat := "data: " + `{"id":"c1","model_id":"x","choices":[{"index":0,"delta":{"content":"hello","reasoning_content":"think"},"finish_reason":null}]}` + "\n\n" +
		"data: " + `{"id":"c1","model_id":"x","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}` + "\n\ndata: [DONE]\n\n"
	responses := "event: response.output_text.delta\ndata: " + `{"model_id":"x","delta":"hello","item_id":"i1","output_index":0,"content_index":0}` + "\n\n" +
		"event: response.function_call_arguments.delta\ndata: " + `{"delta":"{}","item_id":"i2","output_index":1}` + "\n\n" +
		"event: response.completed\ndata: " + `{"response":{"model_id":"x","status":"completed","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}` + "\n\n"
	claude := "event: message_start\ndata: " + `{"message":{"model_id":"x","usage":{"input_tokens":2,"output_tokens":0,"cache_read_input_tokens":-1}}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"index":0,"delta":{"type":"thinking_delta","thinking":"think"}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}` + "\n\n" +
		"event: message_delta\ndata: " + `{"delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":3}}` + "\n\nevent: message_stop\ndata: {}\n\n"
	for _, tc := range []struct {
		name, body, expected string
		format               types.RelayFormat
		mode                 int
	}{
		{"chat", chat, "[DONE]", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions},
		{"responses", responses, "event: response.completed", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses},
		{"claude", claude, "event: message_stop", types.RelayFormatClaude, relayconstant.RelayModeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext()
			u, e := (&Adaptor{}).DoResponse(c, response(tc.body, true), testInfo(t, tc.format, tc.mode, true))
			require.Nil(t, e)
			require.Equal(t, 5, u.(*dto.Usage).TotalTokens)
			require.Contains(t, w.Body.String(), tc.expected)
			require.Contains(t, w.Body.String(), `"model":"x"`)
			require.NotContains(t, w.Body.String(), "model_id")
			require.NotContains(t, w.Body.String(), `"error"`)
		})
	}
	c, w := testContext()
	info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, true)
	info.ShouldIncludeUsage = false
	_, e := (&Adaptor{}).DoResponse(c, response(chat, true), info)
	require.Nil(t, e)
	require.NotContains(t, w.Body.String(), `"usage"`)
	require.Contains(t, w.Body.String(), "[DONE]")
}

func TestStreamFailuresAndSSEFraming(t *testing.T) {
	start := "data: " + `{"id":"c1","model_id":"x","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n"
	for _, tail := range []string{"", "data: " + `{"code":10042,"message":"upstream failed"}` + "\n\n", "data: {invalid}\n\n"} {
		c, w := testContext()
		info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, true)
		_, e := (&Adaptor{}).DoResponse(c, response(start+tail, true), info)
		require.Nil(t, e, "already emitted content must not trigger retry")
		require.True(t, info.StreamStatus.HasErrors())
		require.Contains(t, w.Body.String(), `"error"`)
		require.NotContains(t, w.Body.String(), "[DONE]")
	}
	r := newEventReader(io.NopCloser(strings.NewReader("event: response.output_text.delta\r\ndata: {\"delta\":\r\ndata: \"hi\"}\r\n\r\n")))
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Contains(t, string(b), `"type":"response.output_text.delta"`)
	m, err := normalizeStreamEvent([]byte(`{"type":"response.content_part.delta","item_id":"i","output_index":0,"content_index":0,"delta":{"type":"text","text":"hi"}}`), true)
	require.NoError(t, err)
	require.Equal(t, "response.output_text.delta", rawString(m["type"]))
	require.Equal(t, "hi", rawString(m["delta"]))
	_, err = normalizeStreamEvent([]byte(`{"type":"response.content_part.delta","delta":"hi"}`), true)
	require.Error(t, err)
}

type observedWriter struct {
	*httptest.ResponseRecorder
	wrote chan struct{}
	once  sync.Once
}

func (w *observedWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(data)
	w.once.Do(func() { close(w.wrote) })
	return n, err
}

func TestStreamCancellationClosesBlockedUpstream(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	w := &observedWriter{ResponseRecorder: httptest.NewRecorder(), wrote: make(chan struct{})}
	c, _ := gin.CreateTestContext(w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	info := testInfo(t, types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions, true)
	resp := response("", true)
	resp.Body = reader
	done := make(chan *types.NewAPIError, 1)
	go func() { _, err := (&Adaptor{}).DoResponse(c, resp, info); done <- err }()
	_, err := writer.Write([]byte("data: " + `{"choices":[{"delta":{"content":"partial"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n"))
	require.NoError(t, err)
	select {
	case <-w.wrote:
	case <-time.After(time.Second):
		t.Fatal("no downstream frame")
	}
	cancel()
	select {
	case err := <-done:
		require.Nil(t, err, "partial output must not retry")
	case <-time.After(2 * time.Second):
		t.Fatal("upstream read did not stop after cancellation")
	}
	require.True(t, info.StreamStatus.HasErrors())
	require.NotContains(t, w.Body.String(), "[DONE]")
}
