package gcp

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rootcommon "github.com/QuantumNous/new-api/common"
	rootconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

func TestGCPRegistrationAndModels(t *testing.T) {
	apiType, ok := rootcommon.ChannelType2APIType(rootconstant.ChannelTypeGCP)
	if !ok {
		t.Fatal("expected GCP channel type to map to an API type")
	}
	if apiType != rootconstant.APITypeGCP {
		t.Fatalf("unexpected API type: got %d want %d", apiType, rootconstant.APITypeGCP)
	}

	for _, model := range []string{"gcp-text-to-speech", "gcp-speech-to-text", "tts-1", "tts-1-hd", "whisper-1"} {
		models := (&Adaptor{}).GetModelList()
		if !containsString(models, model) {
			t.Fatalf("expected model list to contain %q, got %#v", model, models)
		}
	}
}

func TestAccessTokenCacheKey(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 42},
	}
	if got, want := buildAccessTokenCacheKey(info), "gcp-access-token-42"; got != want {
		t.Fatalf("unexpected cache key: got %q want %q", got, want)
	}

	info.ChannelIsMultiKey = true
	info.ChannelMultiKeyIndex = 3
	if got, want := buildAccessTokenCacheKey(info), "gcp-access-token-42-3"; got != want {
		t.Fatalf("unexpected multi-key cache key: got %q want %q", got, want)
	}
}

func TestSetupRequestHeaderDoesNotForceUserProject(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 91,
			ApiKey:    `{"project_id":"chat-gpt-android","client_email":"svc@example.iam.gserviceaccount.com"}`,
		},
	}
	accessTokenCache.Store(buildAccessTokenCacheKey(info), cachedAccessToken{
		Token:     "cached-token",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	defer accessTokenCache.Delete(buildAccessTokenCacheKey(info))

	c, _ := newGCPTestContext(http.MethodPost, "/v1/audio/transcriptions", nil, gin.MIMEJSON)
	headers := http.Header{}
	if err := (&Adaptor{}).SetupRequestHeader(c, &headers, info); err != nil {
		t.Fatalf("SetupRequestHeader returned error: %v", err)
	}
	if got := headers.Get("Authorization"); got != "Bearer cached-token" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	if got := headers.Get("x-goog-user-project"); got != "" {
		t.Fatalf("x-goog-user-project should be opt-in, got %q", got)
	}
}

func TestConvertSpeechRequestDefaults(t *testing.T) {
	c, _ := newGCPTestContext(http.MethodPost, "/v1/audio/speech", nil, gin.MIMEJSON)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech}
	speed := 1.25

	reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{
		Model: "gcp-text-to-speech",
		Input: "你好",
		Voice: "alloy",
		Speed: &speed,
	})
	if err != nil {
		t.Fatalf("ConvertAudioRequest returned error: %v", err)
	}

	body := readMap(t, reader)
	if got := nestedString(body, "input", "text"); got != "你好" {
		t.Fatalf("unexpected input text: %q", got)
	}
	if got := nestedString(body, "voice", "name"); got != defaultTTSVoiceName {
		t.Fatalf("unexpected voice name: %q", got)
	}
	if got := nestedString(body, "voice", "languageCode"); got != defaultTTSLanguageCode {
		t.Fatalf("unexpected voice language: %q", got)
	}
	if got := nestedString(body, "audioConfig", "audioEncoding"); got != "MP3" {
		t.Fatalf("unexpected audio encoding: %q", got)
	}
	if got := nestedFloat(body, "audioConfig", "speakingRate"); got != speed {
		t.Fatalf("unexpected speaking rate: %v", got)
	}
}

func TestConvertSpeechRequestMetadataOverrides(t *testing.T) {
	c, _ := newGCPTestContext(http.MethodPost, "/v1/audio/speech", nil, gin.MIMEJSON)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech}

	reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{
		Model:    "tts-1",
		Input:    "ignored",
		Metadata: []byte(`{"gcp":{"native_response":true,"input":{"ssml":"<speak>hi</speak>"},"voice":{"name":"en-US-Neural2-A"},"audio_config":{"audioEncoding":"LINEAR16"},"advanced_voice_options":{"lowLatencyJourneySynthesis":true}}}`),
	})
	if err != nil {
		t.Fatalf("ConvertAudioRequest returned error: %v", err)
	}

	body := readMap(t, reader)
	if _, ok := body["native_response"]; ok {
		t.Fatal("native_response should not be forwarded to GCP")
	}
	if !c.GetBool(contextKeyGCPNativeResponse) {
		t.Fatal("expected native response flag to be stored in context")
	}
	if got := nestedString(body, "input", "ssml"); got != "<speak>hi</speak>" {
		t.Fatalf("unexpected ssml: %q", got)
	}
	if got := nestedString(body, "voice", "languageCode"); got != "en-US" {
		t.Fatalf("unexpected voice language inferred from name: %q", got)
	}
	if got := nestedString(body, "audioConfig", "audioEncoding"); got != "LINEAR16" {
		t.Fatalf("unexpected audio encoding override: %q", got)
	}
	if got := nestedBool(body, "advancedVoiceOptions", "lowLatencyJourneySynthesis"); !got {
		t.Fatal("expected advanced voice options to be forwarded")
	}
}

func TestConvertTranscriptionRequestNativeAudio(t *testing.T) {
	c, _ := newGCPTestContext(http.MethodPost, "/v1/audio/transcriptions", nil, gin.MIMEJSON)
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioTranscription}, dto.AudioRequest{
		Model:    "gcp-speech-to-text",
		Metadata: []byte(`{"gcp":{"audio":{"uri":"gs://bucket/sample.flac"},"config":{"encoding":"FLAC","languageCode":"en-US"}}}`),
	})
	if err != nil {
		t.Fatalf("ConvertAudioRequest returned error: %v", err)
	}

	body := readMap(t, reader)
	if got := nestedString(body, "audio", "uri"); got != "gs://bucket/sample.flac" {
		t.Fatalf("unexpected audio uri: %q", got)
	}
	if got := nestedString(body, "config", "encoding"); got != "FLAC" {
		t.Fatalf("unexpected encoding: %q", got)
	}
	if got := nestedString(body, "config", "languageCode"); got != "en-US" {
		t.Fatalf("unexpected language code: %q", got)
	}
}

func TestConvertTranscriptionRequestMultipartAudio(t *testing.T) {
	audioBytes := []byte{0x52, 0x49, 0x46, 0x46}
	body, contentType := multipartAudioBody(t, "sample.wav", audioBytes)
	c, _ := newGCPTestContext(http.MethodPost, "/v1/audio/transcriptions", body, contentType)

	reader, err := (&Adaptor{}).ConvertAudioRequest(c, &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioTranscription}, dto.AudioRequest{
		Model:          "whisper-1",
		Language:       []byte(`"zh-CN"`),
		ResponseFormat: "verbose_json",
	})
	if err != nil {
		t.Fatalf("ConvertAudioRequest returned error: %v", err)
	}

	converted := readMap(t, reader)
	if got := nestedString(converted, "audio", "content"); got != base64.StdEncoding.EncodeToString(audioBytes) {
		t.Fatalf("unexpected audio content: %q", got)
	}
	if got := nestedString(converted, "config", "encoding"); got != "LINEAR16" {
		t.Fatalf("unexpected inferred encoding: %q", got)
	}
	if got := nestedString(converted, "config", "languageCode"); got != defaultSTTLanguageCode {
		t.Fatalf("unexpected language code: %q", got)
	}
	if got := c.GetString(contextKeyGCPResponseFormat); got != "verbose_json" {
		t.Fatalf("unexpected response format context: %q", got)
	}
}

func TestHandleTTSResponseDecodesAudio(t *testing.T) {
	c, recorder := newGCPTestContext(http.MethodPost, "/v1/audio/speech", nil, gin.MIMEJSON)
	c.Set(contextKeyGCPResponseFormat, "mp3")

	usage, apiErr := handleTTSResponse(c, jsonResponse(`{"audioContent":"AQID"}`), &relaycommon.RelayInfo{})
	if apiErr != nil {
		t.Fatalf("handleTTSResponse returned error: %v", apiErr)
	}
	if usage == nil {
		t.Fatal("expected usage")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "audio/mpeg") {
		t.Fatalf("unexpected content type: %q", got)
	}
	if got := recorder.Body.Bytes(); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("unexpected audio bytes: %#v", got)
	}
}

func TestHandleSTTResponseFormats(t *testing.T) {
	body := `{"results":[{"alternatives":[{"transcript":"你好","confidence":0.9}],"languageCode":"cmn-Hans-CN"}]}`

	t.Run("json", func(t *testing.T) {
		c, recorder := newGCPTestContext(http.MethodPost, "/v1/audio/transcriptions", nil, gin.MIMEJSON)
		c.Set(contextKeyGCPResponseFormat, "json")
		if _, apiErr := handleSTTResponse(c, jsonResponse(body), &relaycommon.RelayInfo{}); apiErr != nil {
			t.Fatalf("handleSTTResponse returned error: %v", apiErr)
		}
		var response dto.AudioResponse
		if err := rootcommon.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if response.Text != "你好" {
			t.Fatalf("unexpected text: %q", response.Text)
		}
	})

	t.Run("text", func(t *testing.T) {
		c, recorder := newGCPTestContext(http.MethodPost, "/v1/audio/transcriptions", nil, gin.MIMEJSON)
		c.Set(contextKeyGCPResponseFormat, "text")
		if _, apiErr := handleSTTResponse(c, jsonResponse(body), &relaycommon.RelayInfo{}); apiErr != nil {
			t.Fatalf("handleSTTResponse returned error: %v", apiErr)
		}
		if got := strings.TrimSpace(recorder.Body.String()); got != "你好" {
			t.Fatalf("unexpected text response: %q", got)
		}
	})

	t.Run("verbose_json", func(t *testing.T) {
		c, recorder := newGCPTestContext(http.MethodPost, "/v1/audio/transcriptions", nil, gin.MIMEJSON)
		c.Set(contextKeyGCPResponseFormat, "verbose_json")
		if _, apiErr := handleSTTResponse(c, jsonResponse(body), &relaycommon.RelayInfo{}); apiErr != nil {
			t.Fatalf("handleSTTResponse returned error: %v", apiErr)
		}
		var response dto.WhisperVerboseJSONResponse
		if err := rootcommon.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("failed to unmarshal response: %v", err)
		}
		if response.Task != "transcribe" || response.Language != "cmn-Hans-CN" || response.Text != "你好" {
			t.Fatalf("unexpected verbose response: %#v", response)
		}
	})
}

func TestGCPErrorPayload(t *testing.T) {
	apiErr := gcpErrorFromBody([]byte(`{"error":{"code":403,"message":"permission denied","status":"PERMISSION_DENIED"}}`))
	if apiErr == nil {
		t.Fatal("expected GCP error payload to convert to API error")
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected status code: %d", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "permission denied") {
		t.Fatalf("unexpected error message: %v", apiErr)
	}
}

func newGCPTestContext(method, path string, body io.Reader, contentType string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	if body == nil {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.Request = req
	return c, recorder
}

func multipartAudioBody(t *testing.T, filename string, audioBytes []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile failed: %v", err)
	}
	if _, err = part.Write(audioBytes); err != nil {
		t.Fatalf("writing multipart file failed: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("closing multipart writer failed: %v", err)
	}
	return bytes.NewReader(body.Bytes()), writer.FormDataContentType()
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{gin.MIMEJSON}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func readMap(t *testing.T, reader io.Reader) map[string]any {
	t.Helper()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read request body: %v", err)
	}
	var body map[string]any
	if err = rootcommon.Unmarshal(data, &body); err != nil {
		t.Fatalf("failed to unmarshal body %s: %v", string(data), err)
	}
	return body
}

func nestedMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("expected %q to be object, got %#v", key, m[key])
	}
	return nested
}

func nestedString(m map[string]any, first, second string) string {
	nested, ok := m[first].(map[string]any)
	if !ok {
		return ""
	}
	value, _ := nested[second].(string)
	return value
}

func nestedFloat(m map[string]any, first, second string) float64 {
	nested, ok := m[first].(map[string]any)
	if !ok {
		return 0
	}
	value, _ := nested[second].(float64)
	return value
}

func nestedBool(m map[string]any, first, second string) bool {
	nested, ok := m[first].(map[string]any)
	if !ok {
		return false
	}
	value, _ := nested[second].(bool)
	return value
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}
