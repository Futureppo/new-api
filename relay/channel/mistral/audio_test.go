package mistral

import (
	"bytes"
	"encoding/binary"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testWAV() []byte {
	const size = 32000
	wav := make([]byte, 44+size)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], size)
	return wav
}

func audioContext(t *testing.T, fields map[string]string, withFile bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		require.NoError(t, writer.WriteField(k, v))
	}
	if withFile {
		part, err := writer.CreateFormFile("file", "audio.wav")
		require.NoError(t, err)
		_, err = part.Write(testWAV())
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	c, w := testContext()
	c.Request = httptest.NewRequest("POST", "/v1/audio/transcriptions", bytes.NewReader(body.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return c, w
}

func TestMistralTranscriptionMultipartAndFormats(t *testing.T) {
	for _, format := range []string{"json", "text", "verbose_json", "srt", "vtt"} {
		t.Run(format, func(t *testing.T) {
			c, w := audioContext(t, map[string]string{"model": "client-alias", "temperature": "0", "language": "en", "response_format": format, "timestamp_granularities[]": "segment"}, true)
			info := testInfo(relayconstant.RelayModeAudioTranscription)
			a := &Adaptor{}
			reader, err := a.ConvertAudioRequest(c, info, dto.AudioRequest{Model: "voxtral-mini-latest", ResponseFormat: format})
			require.NoError(t, err)
			req := httptest.NewRequest("POST", "/v1/audio/transcriptions", reader)
			req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
			require.NoError(t, req.ParseMultipartForm(1<<20))
			defer req.MultipartForm.RemoveAll()
			require.Equal(t, "voxtral-mini-latest", req.FormValue("model"))
			require.Equal(t, "0", req.FormValue("temperature"))
			require.Equal(t, "en", req.FormValue("language"))
			require.Equal(t, []string{"segment"}, req.MultipartForm.Value["timestamp_granularities"])
			require.NotContains(t, req.MultipartForm.Value, "response_format")
			file, _, err := req.FormFile("file")
			require.NoError(t, err)
			raw, err := io.ReadAll(file)
			file.Close()
			require.NoError(t, err)
			require.Equal(t, testWAV(), raw)
			result, apiErr := a.DoResponse(c, mockResponse(`{"model":"voxtral-mini-latest","text":"Hello world.","language":"en","segments":[{"text":"Hello world.","start":0.125,"end":0.875}],"usage":{"prompt_tokens":6,"completion_tokens":3,"total_tokens":384,"prompt_tokens_details":{"audio_tokens":375}}}`), info)
			require.Nil(t, apiErr)
			require.Equal(t, 381, result.(*dto.Usage).PromptTokens)
			switch format {
			case "json":
				require.JSONEq(t, `{"text":"Hello world."}`, w.Body.String())
			case "text":
				require.Equal(t, "Hello world.", w.Body.String())
				require.Contains(t, w.Header().Get("Content-Type"), "text/plain")
			case "verbose_json":
				var body map[string]any
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
				require.Equal(t, float64(1), body["duration"])
				require.Equal(t, "transcribe", body["task"])
				segment := body["segments"].([]any)[0].(map[string]any)
				require.Equal(t, 0.125, segment["start"])
				require.NotContains(t, segment, "tokens")
				require.NotContains(t, segment, "avg_logprob")
			case "srt":
				require.Equal(t, "1\n00:00:00,125 --> 00:00:00,875\nHello world.\n\n", w.Body.String())
			case "vtt":
				require.Equal(t, "WEBVTT\n\n00:00:00.125 --> 00:00:00.875\nHello world.\n\n", w.Body.String())
			}
		})
	}
}

func TestMistralTranscriptionRejectsUnsupportedRequests(t *testing.T) {
	for _, fields := range []map[string]string{{"prompt": "some instructions"}, {"stream": "true"}, {"timestamp_granularities[]": "sentence"}, {"timestamp_granularities[]": "word", "timestamp_granularities": "segment"}} {
		c, _ := audioContext(t, fields, true)
		_, err := (&Adaptor{}).ConvertAudioRequest(c, testInfo(relayconstant.RelayModeAudioTranscription), dto.AudioRequest{Model: "voxtral-mini-latest"})
		require.Error(t, err)
	}
	c, _ := audioContext(t, nil, false)
	_, err := (&Adaptor{}).ConvertAudioRequest(c, testInfo(relayconstant.RelayModeAudioTranscription), dto.AudioRequest{Model: "voxtral-mini-latest"})
	require.Error(t, err)
}

func TestMistralTranscriptionWordsAndMissingTimestamps(t *testing.T) {
	a := &Adaptor{audioFormat: "verbose_json", audioGranularity: "word"}
	c, w := testContext()
	_, apiErr := a.DoResponse(c, mockResponse(`{"text":"hello","segments":[{"text":"hello","start":0.1,"end":0.5}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), testInfo(relayconstant.RelayModeAudioTranscription))
	require.Nil(t, apiErr)
	require.Contains(t, w.Body.String(), `"words":[{"end":0.5,"start":0.1,"word":"hello"}]`)
	require.NotContains(t, w.Body.String(), "language")
	a.audioFormat = "srt"
	c, _ = testContext()
	_, apiErr = a.DoResponse(c, mockResponse(`{"text":"hello","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`), testInfo(relayconstant.RelayModeAudioTranscription))
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
}
