package relay

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func TestMistralNativeLiveGateway(t *testing.T) {
	if os.Getenv("MISTRAL_LIVE_TEST") != "1" {
		t.Skip("set MISTRAL_LIVE_TEST=1 and MISTRAL_API_KEY to run")
	}
	key := os.Getenv("MISTRAL_API_KEY")
	require.NotEmpty(t, key)
	setupMistralPipeline(t)
	base := "https://api.mistral.ai/v1/"
	run := func(t *testing.T, path, model string, payload any) *httptest.ResponseRecorder {
		t.Helper()
		body, err := common.Marshal(payload)
		require.NoError(t, err)
		w, _, apiErr := mistralGatewayRequest(t, path, "application/json", body, base, key, model, dto.ChannelSettings{ForceFormat: true}, nil)
		require.Nil(t, apiErr)
		require.Equal(t, 200, w.Code)
		return w
	}
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "fim", true: "fim_stream"}[stream], func(t *testing.T) {
			w := run(t, "/v1/fim/completions", "codestral-latest", map[string]any{"model": "test-alias", "prompt": "def add(a, b):\n    ", "suffix": "\nprint(add(1, 2))", "max_tokens": 32, "temperature": 0, "stream": stream})
			if stream {
				require.Equal(t, 1, strings.Count(w.Body.String(), "[DONE]"))
			} else {
				require.Contains(t, w.Body.String(), `"choices"`)
			}
		})
	}
	t.Run("ocr", func(t *testing.T) {
		im := image.NewRGBA(image.Rect(0, 0, 512, 128))
		draw.Draw(im, im.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		d := font.Drawer{Dst: im, Src: image.Black, Face: basicfont.Face7x13, Dot: fixed.P(32, 60)}
		d.DrawString("Mistral native OCR test. Paris.")
		var buffer bytes.Buffer
		require.NoError(t, png.Encode(&buffer, im))
		w := run(t, "/v1/ocr", "mistral-ocr-latest", map[string]any{"model": "test-alias", "document": map[string]any{"type": "image_url", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())}, "include_image_base64": false})
		var result map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.NotNil(t, result["usage_info"])
		require.NotEmpty(t, result["pages"])
		require.NotContains(t, result, "choices")
	})
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "speech", true: "speech_stream"}[stream], func(t *testing.T) {
			w := run(t, "/v1/audio/speech", "voxtral-mini-tts-latest", map[string]any{"model": "test-alias", "input": "Hello, Paris.", "voice_id": "en_paul_neutral", "response_format": "wav", "stream": stream})
			require.NotEmpty(t, w.Body.Bytes())
			if stream {
				require.Contains(t, w.Body.String(), "speech.audio.done")
				require.NotContains(t, w.Body.String(), "[DONE]")
			}
		})
	}
	t.Run("transcription_stream", func(t *testing.T) {
		body, ct := mistralMultipart(t, map[string]string{"model": "test-alias", "stream": "true", "language": "en"}, mistralWAV())
		w, _, apiErr := mistralGatewayRequest(t, "/v1/audio/transcriptions", ct, body, base, key, "voxtral-mini-latest", dto.ChannelSettings{ForceFormat: true}, nil)
		require.Nil(t, apiErr)
		require.Contains(t, w.Body.String(), "transcription.done")
		require.NotContains(t, w.Body.String(), "[DONE]")
	})
	t.Run("realtime", func(t *testing.T) {
		errCh := make(chan *types.NewAPIError, 1)
		engine := gin.New()
		engine.GET(mistral.RealtimePath, func(c *gin.Context) {
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeMistral)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, base)
			common.SetContextKey(c, constant.ContextKeyChannelKey, key)
			common.SetContextKey(c, constant.ContextKeyChannelId, 9001)
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "voxtral-mini-transcribe-realtime-2602")
			request, err := dto.ParseMistralNativeRequest(c)
			if err != nil {
				c.Status(400)
				return
			}
			info := &relaycommon.RelayInfo{Request: request, OriginModelName: request.Model, RequestURLPath: c.Request.URL.String(), RelayFormat: types.RelayFormatMistralRealtime, IsStream: true, DisablePing: true, StartTime: time.Now(), UserId: 9001, UserQuota: 1000000000, UsingGroup: "default", Billing: &mistralBillingRecorder{}, PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
			errCh <- MistralNativeHelper(c, info)
		})
		server := httptest.NewServer(engine)
		defer server.Close()
		conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+mistral.RealtimePath+"?model=voxtral-mini-transcribe-realtime-2602", nil)
		if err != nil && resp != nil {
			t.Logf("handshake HTTP %d", resp.StatusCode)
		}
		require.NoError(t, err)
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(40 * time.Second))
		_, event, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Contains(t, string(event), "session.created")
		for _, message := range []any{
			map[string]any{"type": "session.update", "session": map[string]any{"audio_format": map[string]any{"encoding": "pcm_s16le", "sample_rate": 16000}}},
			map[string]any{"type": "input_audio.append", "audio": base64.StdEncoding.EncodeToString(mistralWAV()[44:])},
			map[string]any{"type": "input_audio.flush"},
			map[string]any{"type": "input_audio.end"},
		} {
			data, err := common.Marshal(message)
			require.NoError(t, err)
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))
		}
		for {
			_, event, err := conn.ReadMessage()
			require.NoError(t, err)
			var result struct {
				Type string `json:"type"`
			}
			require.NoError(t, common.Unmarshal(event, &result))
			require.NotEqual(t, "error", result.Type, string(event))
			if result.Type == "transcription.done" {
				break
			}
		}
		select {
		case apiErr := <-errCh:
			require.Nil(t, apiErr)
		case <-time.After(3 * time.Second):
			t.Fatal("realtime settlement did not finish")
		}
	})
}
