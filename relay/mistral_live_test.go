package relay

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// Opt in explicitly. Credentials stay in the environment, and this suite only
// uses a temporary database. It exercises the production relay helpers,
// conversion, transport, response formatting, model mapping and settlement.
func TestMistralLiveGateway(t *testing.T) {
	if os.Getenv("MISTRAL_LIVE_TEST") != "1" {
		t.Skip("set MISTRAL_LIVE_TEST=1 and MISTRAL_API_KEY to run")
	}
	key := os.Getenv("MISTRAL_API_KEY")
	require.NotEmpty(t, key)
	setupMistralPipeline(t)
	base := "https://api.mistral.ai/v1/"
	run := func(t *testing.T, model, path string, payload any) map[string]any {
		t.Helper()
		body, err := common.Marshal(payload)
		require.NoError(t, err)
		w, _, apiErr := mistralGatewayRequest(t, path, "application/json", body, base, key, model, dto.ChannelSettings{ForceFormat: true}, nil)
		require.Nil(t, apiErr)
		require.Equal(t, 200, w.Code)
		var result map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.Nil(t, result["error"])
		return result
	}
	message := func(result map[string]any) map[string]any {
		return result["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	}
	payload := func() map[string]any {
		return map[string]any{"model": "test-alias", "messages": []any{map[string]any{"role": "developer", "content": "Follow the user."}, map[string]any{"role": "user", "content": "Reply with exactly OK."}}, "max_completion_tokens": 64, "temperature": 0, "seed": 0, "presence_penalty": 0, "frequency_penalty": 0}
	}
	for _, model := range []string{"ministral-3b-latest", "codestral-latest"} {
		t.Run(model, func(t *testing.T) {
			result := run(t, model, "/v1/chat/completions", payload())
			require.NotEmpty(t, message(result)["content"])
		})
	}
	t.Run("json_schema", func(t *testing.T) {
		p := payload()
		p["messages"] = []any{map[string]any{"role": "user", "content": "Return JSON with ok true."}}
		p["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "strict": true, "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}}}
		result := run(t, "ministral-3b-latest", "/v1/chat/completions", p)
		require.JSONEq(t, `{"ok":true}`, message(result)["content"].(string))
	})
	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "get_weather", "parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}}}}}
	toolPayload := func() map[string]any {
		p := payload()
		p["messages"] = []any{map[string]any{"role": "user", "content": "Call get_weather for Paris."}}
		p["tools"] = tools
		p["tool_choice"] = "required"
		p["parallel_tool_calls"] = false
		return p
	}
	t.Run("tool_round_trip", func(t *testing.T) {
		p := toolPayload()
		result := run(t, "ministral-3b-latest", "/v1/chat/completions", p)
		assistant := message(result)
		calls := assistant["tool_calls"].([]any)
		require.Len(t, calls, 1)
		call := calls[0].(map[string]any)
		require.IsType(t, "", call["function"].(map[string]any)["arguments"])
		// Exercise translation of an OpenAI client's longer ID in both history legs.
		call["id"] = "call_openai_client_history_id"
		p["messages"] = []any{map[string]any{"role": "user", "content": "Weather in Paris?"}, assistant, map[string]any{"role": "tool", "tool_call_id": call["id"], "name": "get_weather", "content": "Sunny, 20 C."}}
		p["tool_choice"] = "auto"
		result = run(t, "ministral-3b-latest", "/v1/chat/completions", p)
		require.NotEmpty(t, message(result)["content"])
	})
	for _, tool := range []bool{false, true} {
		t.Run(map[bool]string{false: "stream", true: "tool_stream"}[tool], func(t *testing.T) {
			p := payload()
			if tool {
				p = toolPayload()
			}
			p["stream"] = true
			p["stream_options"] = map[string]any{"include_usage": true}
			body, err := common.Marshal(p)
			require.NoError(t, err)
			w, _, apiErr := mistralGatewayRequest(t, "/v1/chat/completions", "application/json", body, base, key, "ministral-3b-latest", dto.ChannelSettings{ForceFormat: true}, nil)
			require.Nil(t, apiErr)
			require.NotContains(t, w.Body.String(), `"error":`)
			require.Equal(t, 1, strings.Count(w.Body.String(), "[DONE]"))
			frames := []map[string]any{}
			for _, line := range strings.Split(w.Body.String(), "\n") {
				if !strings.HasPrefix(line, "data: {") {
					continue
				}
				var frame map[string]any
				require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &frame))
				frames = append(frames, frame)
			}
			require.Greater(t, len(frames), 1)
			last := frames[len(frames)-1]
			require.Empty(t, last["choices"])
			require.NotNil(t, last["usage"])
			if tool {
				require.Contains(t, w.Body.String(), "get_weather")
				require.Contains(t, w.Body.String(), `"finish_reason":"tool_calls"`)
			}
		})
	}
	t.Run("vision", func(t *testing.T) {
		im := image.NewRGBA(image.Rect(0, 0, 128, 128))
		for y := 0; y < 128; y++ {
			for x := 0; x < 128; x++ {
				im.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			}
		}
		var buf bytes.Buffer
		require.NoError(t, png.Encode(&buf, im))
		p := payload()
		p["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "What color is this image? Answer one word in English."}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), "detail": "low"}}}}}
		result := run(t, "ministral-3b-latest", "/v1/chat/completions", p)
		require.Contains(t, strings.ToLower(message(result)["content"].(string)), "red")
	})
	for _, model := range []string{"mistral-embed", "codestral-embed"} {
		t.Run(model, func(t *testing.T) {
			for _, format := range []string{"float", "base64"} {
				p := map[string]any{"model": "test-alias", "input": []string{"hello", "world"}, "encoding_format": format}
				size := 1024
				if model == "codestral-embed" {
					p["dimensions"] = 256
					size = 256
				}
				result := run(t, model, "/v1/embeddings", p)
				items := result["data"].([]any)
				require.Len(t, items, 2)
				vector := items[0].(map[string]any)["embedding"]
				if format == "float" {
					require.Len(t, vector, size)
				} else {
					decoded, err := base64.StdEncoding.DecodeString(vector.(string))
					require.NoError(t, err)
					require.Len(t, decoded, size*4)
				}
			}
		})
	}
	t.Run("unsupported_embedding_dimensions", func(t *testing.T) {
		body := []byte(`{"model":"test-alias","input":"hello","dimensions":256}`)
		_, _, apiErr := mistralGatewayRequest(t, "/v1/embeddings", "application/json", body, base, key, "mistral-embed", dto.ChannelSettings{}, nil)
		require.NotNil(t, apiErr)
		require.Equal(t, 400, apiErr.StatusCode)
	})
	audio := mistralWAV()
	spoken := false
	if path := os.Getenv("MISTRAL_TEST_WAV"); path != "" {
		var err error
		audio, err = os.ReadFile(path)
		require.NoError(t, err)
		spoken = true
	}
	t.Run("audio_chat", func(t *testing.T) {
		p := payload()
		p["messages"] = []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": base64.StdEncoding.EncodeToString(audio), "format": "wav"}}, map[string]any{"type": "text", "text": "Transcribe this audio."}}}}
		result := run(t, "voxtral-small-latest", "/v1/chat/completions", p)
		if spoken {
			require.Contains(t, strings.ToLower(message(result)["content"].(string)), "paris")
		}
		usage := result["usage"].(map[string]any)
		require.Equal(t, usage["total_tokens"], usage["prompt_tokens"].(float64)+usage["completion_tokens"].(float64))
	})
	for _, format := range []string{"json", "text", "verbose_json", "srt", "vtt"} {
		t.Run("transcription_"+format, func(t *testing.T) {
			body, ct := mistralMultipart(t, map[string]string{"model": "test-alias", "response_format": format, "language": "en", "temperature": "0"}, audio)
			w, _, apiErr := mistralGatewayRequest(t, "/v1/audio/transcriptions", ct, body, base, key, "voxtral-mini-latest", dto.ChannelSettings{}, nil)
			require.Nil(t, apiErr)
			require.Equal(t, 200, w.Code)
			if spoken {
				require.Contains(t, strings.ToLower(w.Body.String()), "paris")
			}
			if format == "srt" && spoken {
				require.Contains(t, w.Body.String(), " --> ")
			}
			if format == "vtt" {
				require.True(t, strings.HasPrefix(w.Body.String(), "WEBVTT"))
			}
		})
	}
	t.Run("transcription_words", func(t *testing.T) {
		body, ct := mistralMultipart(t, map[string]string{"model": "test-alias", "response_format": "verbose_json", "timestamp_granularities[]": "word", "language": "en"}, audio)
		w, _, apiErr := mistralGatewayRequest(t, "/v1/audio/transcriptions", ct, body, base, key, "voxtral-mini-latest", dto.ChannelSettings{}, nil)
		require.Nil(t, apiErr)
		var result map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.NotContains(t, result, "segments")
		words, ok := result["words"].([]any)
		require.True(t, ok)
		if spoken {
			require.NotEmpty(t, words)
		}
		for _, value := range words {
			word := value.(map[string]any)
			require.Equal(t, strings.TrimSpace(word["word"].(string)), word["word"])
			require.GreaterOrEqual(t, word["end"].(float64), word["start"].(float64))
		}
	})
}
