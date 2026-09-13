package mistral

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestMistralNativeRequestPreservesUnknownFieldsAndAgentID(t *testing.T) {
	for _, tc := range []struct{ path, body, field string }{
		{"/v1/ocr", `{"model":"alias","document":{"type":"document_url","document_url":"https://example.com/a.pdf"},"include_image_base64":false,"image_limit":0,"future":{"integer":9007199254740993}}`, "model"},
		{"/v1/fim/completions", `{"model":"alias","prompt":"def f():","suffix":"return 1","min_tokens":0,"random_seed":0,"stream":false}`, "model"},
		{"/v1/agents/completions", `{"agent_id":"alias","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"web_search"}],"parallel_tool_calls":false}`, "agent_id"},
		{"/v1/audio/speech", `{"model":"alias","input":"hi","voice_id":"voice-1","ref_audio":"ref","stream":false}`, "model"},
		{"/v1/audio/transcriptions", `{"model":"alias","file_url":"https://example.com/a.wav","stream":true,"context_bias":["Paris"]}`, "model"},
	} {
		c, _ := testContext()
		defer common.CleanupBodyStorage(c)
		c.Request = httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		request, err := dto.ParseMistralNativeRequest(c)
		require.NoError(t, err)
		a := &Adaptor{}
		info := testInfo(relayconstant.RelayModeUnknown)
		reader, err := a.ConvertNativeRequest(c, info, request)
		require.NoError(t, err)
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, tc.body, string(body), "unmodified native body must be byte-identical")
		request.SetModelName("mapped")
		reader, err = a.ConvertNativeRequest(c, info, request)
		require.NoError(t, err)
		body, err = io.ReadAll(reader)
		require.NoError(t, err)
		require.JSONEq(t, strings.Replace(tc.body, `"alias"`, `"mapped"`, 1), string(body))
		if strings.Contains(tc.body, "9007199254740993") {
			require.Contains(t, string(body), "9007199254740993")
		}
		require.Equal(t, tc.field, request.ModelField)
	}
}

func TestMistralNativeMultipartPreservesAllParts(t *testing.T) {
	c, _ := audioContext(t, map[string]string{"model": "alias", "stream": "true", "temperature": "0", "custom_field": "keep", "timestamp_granularities[]": "word"}, true)
	defer common.CleanupBodyStorage(c)
	request, err := dto.ParseMistralNativeRequest(c)
	require.NoError(t, err)
	require.True(t, request.Stream)
	request.SetModelName("voxtral-mini-latest")
	a := &Adaptor{}
	reader, err := a.ConvertNativeRequest(c, testInfo(relayconstant.RelayModeAudioTranscription), request)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/v1/audio/transcriptions", reader)
	req.Header.Set("Content-Type", a.nativeContentType)
	require.NoError(t, req.ParseMultipartForm(1<<20))
	defer req.MultipartForm.RemoveAll()
	require.Equal(t, "voxtral-mini-latest", req.FormValue("model"))
	require.Equal(t, "0", req.FormValue("temperature"))
	require.Equal(t, "keep", req.FormValue("custom_field"))
	require.Equal(t, "word", req.FormValue("timestamp_granularities[]"))
	file, _, err := req.FormFile("file")
	require.NoError(t, err)
	defer file.Close()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, testWAV(), data)
}

func TestMistralNativeResponsesAreVerbatim(t *testing.T) {
	for _, tc := range []struct {
		body, contentType string
		status            int
	}{
		{`{"pages":[{"markdown":"hello","future":false}],"usage_info":{"pages_processed":2}}`, "application/json", 200},
		{"\x00\x01RIFF\xff\x00audio", "audio/wav", 200},
		{`{"audio_data":"AAAA","future":0}`, "application/json", 200},
		{`{"detail":[{"loc":["body"],"msg":"invalid","input":{"custom":false}}]}`, "application/json", 422},
	} {
		c, w := testContext()
		resp := mockResponse(tc.body)
		resp.StatusCode = tc.status
		resp.Header.Set("Content-Type", tc.contentType)
		info := testInfo(relayconstant.RelayModeUnknown)
		info.ChannelSetting.ForceFormat = true
		result, apiErr := (&Adaptor{}).NativeResponse(c, resp, info)
		require.Equal(t, tc.body, w.Body.String())
		require.Equal(t, tc.status, w.Code)
		if tc.status == 422 {
			require.NotNil(t, apiErr)
			require.Equal(t, 422, apiErr.StatusCode)
		} else {
			require.Nil(t, apiErr)
		}
		if strings.Contains(tc.body, "pages_processed") {
			require.Equal(t, 2, *result.Pages)
			require.Nil(t, result.Usage)
		}
	}
}

func TestMistralNativeSSEPreservesFrames(t *testing.T) {
	for _, body := range []string{
		"event: transcription.text.delta\r\nid: 1\r\ndata: {\"type\":\"transcription.text.delta\",\"text\":\"hi\"}\r\n\r\nevent: transcription.done\r\ndata: {\"type\":\"transcription.done\",\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":1,\"total_tokens\":382,\"prompt_tokens_details\":{\"audio_tokens\":375}}}\r\n\r\n",
		"event: speech.audio.delta\ndata: {\"type\":\"speech.audio.delta\",\"audio_data\":\"AAAA\"}\n\nevent: speech.audio.done\ndata: {\"type\":\"speech.audio.done\",\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":1,\"total_tokens\":7}}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n",
	} {
		c, w := testContext()
		resp := mockResponse(body)
		resp.Header.Set("Content-Type", "text/event-stream")
		info := testInfo(relayconstant.RelayModeUnknown)
		info.ShouldIncludeUsage, info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent = false, true, true
		result, apiErr := (&Adaptor{}).NativeResponse(c, resp, info)
		require.Nil(t, apiErr)
		require.Equal(t, body, w.Body.String())
		require.NotNil(t, result.Usage)
		if strings.Contains(body, "audio_tokens") {
			require.Equal(t, 381, result.Usage.PromptTokens)
		}
	}
	for _, body := range []string{"event: error\ndata: {\"message\":\"bad\"}\n\n", "data: {\"type\":\"transcription.text.delta\",\"text\":\"partial\"}\n\n"} {
		c, w := testContext()
		resp := mockResponse(body)
		resp.Header.Set("Content-Type", "text/event-stream")
		_, apiErr := (&Adaptor{}).NativeResponse(c, resp, testInfo(relayconstant.RelayModeUnknown))
		require.NotNil(t, apiErr)
		require.Equal(t, body, w.Body.String())
	}
}

func TestMistralNativeStreamCancellation(t *testing.T) {
	c, _ := testContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	reader, writer := io.Pipe()
	defer writer.Close()
	resp := mockResponse("")
	resp.Body = reader
	resp.Header.Set("Content-Type", "text/event-stream")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = (&Adaptor{}).NativeResponse(c, resp, testInfo(relayconstant.RelayModeUnknown))
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("native upstream did not close")
	}
}

func TestMistralNativeRealtimeRoundTrip(t *testing.T) {
	clientEvent := `{"type":"input_audio.append","audio":"AAAA","extra":false}`
	serverEvents := []string{`{"type":"session.created","session":{"audio_format":{"encoding":"pcm_s16le","sample_rate":16000}}}`, `{"type":"transcription.text.delta","text":"hi"}`, `{"type":"transcription.done","text":"hi","usage":{"prompt_tokens":6,"completion_tokens":1,"total_tokens":7}}`}
	received := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != RealtimePath || r.URL.Query().Get("model") != "voxtral-mini-transcribe-realtime-2602" || r.Header.Get("Authorization") != "Bearer test-upstream-key" {
			w.WriteHeader(400)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(serverEvents[0]))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		received <- string(data)
		for _, event := range serverEvents[1:] {
			_ = conn.WriteMessage(websocket.TextMessage, []byte(event))
		}
	}))
	defer upstream.Close()
	finished := make(chan *NativeResult, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use a real Gin writer so the WebSocket can hijack the HTTP connection.
		engine := newNativeTestRouter(func(c *gin.Context) {
			info := testInfo(relayconstant.RelayModeRealtime)
			info.ChannelBaseUrl, info.ApiKey = upstream.URL+"/v1/", "test-upstream-key"
			info.UpstreamModelName, info.RequestURLPath = "voxtral-mini-transcribe-realtime-2602", r.URL.String()
			a := &Adaptor{nativePath: RealtimePath}
			result, _ := a.NativeRealtime(c, info)
			finished <- result
		})
		engine.ServeHTTP(w, r)
	}))
	defer gateway.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http")+RealtimePath+"?model=alias", nil)
	require.NoError(t, err)
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, serverEvents[0], string(data))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(clientEvent)))
	for _, expected := range serverEvents[1:] {
		_, data, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, expected, string(data))
	}
	require.Equal(t, clientEvent, <-received)
	select {
	case result := <-finished:
		require.Equal(t, 7, result.Usage.TotalTokens)
	case <-time.After(3 * time.Second):
		t.Fatal("native websocket goroutines did not exit")
	}
}

func newNativeTestRouter(handler gin.HandlerFunc) *gin.Engine {
	engine := gin.New()
	engine.GET(RealtimePath, handler)
	return engine
}

func TestMistralNativeRealtimeHandshakeError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"message":"tier restricted","type":"tier_not_allowed"}`)
	}))
	defer upstream.Close()
	c, w := testContext()
	c.Request = httptest.NewRequest("GET", RealtimePath+"?model=m", bytes.NewReader(nil))
	info := testInfo(relayconstant.RelayModeRealtime)
	info.ChannelBaseUrl, info.RequestURLPath = upstream.URL, c.Request.URL.String()
	_, apiErr := (&Adaptor{nativePath: RealtimePath}).NativeRealtime(c, info)
	require.NotNil(t, apiErr)
	require.Equal(t, 403, w.Code)
	require.Equal(t, `{"message":"tier restricted","type":"tier_not_allowed"}`, w.Body.String())
}
