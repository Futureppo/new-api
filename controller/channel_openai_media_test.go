package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIChannelMediaAndModerationTests(t *testing.T) {
	audioBytes := []byte{'I', 'D', '3', 0, 0, 0, 0xff, 0x10}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/v1/audio/speech":
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, "alloy", gjson.GetBytes(body, "voice").String())
			require.False(t, gjson.GetBytes(body, "stream").Bool())
			require.Equal(t, "mp3", gjson.GetBytes(body, "response_format").String())
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write(audioBytes)
		case "/v1/audio/transcriptions":
			require.NoError(t, r.ParseMultipartForm(1<<20))
			defer r.MultipartForm.RemoveAll()
			require.Equal(t, "whisper-1", r.FormValue("model"))
			require.Empty(t, r.FormValue("stream"))
			file, _, err := r.FormFile("file")
			require.NoError(t, err)
			defer file.Close()
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, "RIFF", string(data[:4]))
			require.Equal(t, 64044, len(data))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"text":"","model":"whisper-1","usage":{"input_tokens":32,"output_tokens":0,"total_tokens":32}}`)
		case "/v1/moderations":
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, "Have a nice day.", gjson.GetBytes(body, "input").String())
			require.False(t, gjson.GetBytes(body, "stream").Bool())
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"mod-test","model":"omni-moderation-latest","results":[{"flagged":false,"categories":{},"category_scores":{}}]}`)
		default:
			t.Errorf("unexpected test endpoint: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	_, _, channel := setupDecisionsGateway(t, upstream.URL)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tts-1":0.1,"whisper-1":0.1,"omni-moderation-latest":0}`))
	result := testChannel(channel, "tts-1", "audio-speech", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.NotNil(t, result.usage)
	require.Equal(t, "tts-1", result.upstreamModel)
	require.Equal(t, &channelTestResponseSummary{ContentType: "audio/mpeg", Bytes: len(audioBytes)}, result.responseSummary)
	// JSON overrides must not be applied to a multipart body.
	channel.ParamOverride = common.GetPointer(`{"irrelevant":false}`)
	result = testChannel(channel, "whisper-1", "audio-transcription", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.Equal(t, "whisper-1", result.upstreamModel)
	require.Equal(t, 32, result.usage.PromptTokens)
	channel.ParamOverride = nil
	result = testChannel(channel, "omni-moderation-latest", "moderations", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.Equal(t, "omni-moderation-latest", result.upstreamModel)
}

func TestSynchronousChannelTestsRejectStreamingOverrides(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	_, _, channel := setupDecisionsGateway(t, upstream.URL)
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tts-1":0.1,"omni-moderation-latest":0}`))
	for _, tc := range []struct{ endpoint, model, override string }{
		{"audio-speech", "tts-1", `{"stream":true}`},
		{"audio-speech", "tts-1", `{"stream_format":"sse"}`},
		{"moderations", "omni-moderation-latest", `{"stream":true}`},
	} {
		t.Run(tc.endpoint+tc.override, func(t *testing.T) {
			channel.ParamOverride = &tc.override
			result := testChannel(channel, tc.model, tc.endpoint, true)
			require.ErrorContains(t, result.localErr, "do not support streaming")
			require.NotNil(t, result.newAPIError)
			require.Equal(t, http.StatusBadRequest, result.newAPIError.StatusCode)
		})
	}
	require.Zero(t, calls.Load(), "invalid test requests must never reach upstream")
}

func TestChannelAudioSamplesRetainProviderFormats(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeGCP, constant.ChannelTypeMistral} {
		channel := &model.Channel{Type: channelType}
		speech := buildTestRequest("tts", "audio-speech", channel, true).(*dto.AudioRequest)
		transcription := buildTestRequest("stt", "audio-transcription", channel, true).(*dto.AudioRequest)
		if channelType == constant.ChannelTypeGCP {
			require.Equal(t, "cmn-CN-Wavenet-A", speech.Voice.Name)
			require.Contains(t, string(transcription.Metadata), `"gcp"`)
		} else {
			require.Equal(t, "alloy", speech.Voice.Name)
			require.Empty(t, transcription.Metadata)
		}
		require.False(t, speech.IsStream(nil))
		require.False(t, transcription.IsStream(nil))
	}
}
