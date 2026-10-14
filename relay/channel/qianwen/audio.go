package qianwen

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func audioPath(model string) string {
	if strings.HasPrefix(model, "fun-music-") {
		return musicPath
	}
	if strings.HasPrefix(model, "qwen3-tts-") || strings.HasPrefix(model, "qwen-tts") {
		return multimodalPath
	}
	return speechPath
}

func audioUnit(model string) string {
	if model == "qwen-audio-3.1-tts-next" || strings.HasPrefix(model, "fun-music-") {
		return "seconds"
	}
	if strings.HasPrefix(model, "qwen3-tts-") || strings.HasPrefix(model, "qwen-audio-3.0-tts-") || strings.HasPrefix(model, "cosyvoice") {
		return "characters"
	}
	return ""
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	if info.RelayMode != relayconstant.RelayModeAudioSpeech {
		return nil, unsupportedRequest()
	}
	if strings.TrimSpace(request.Input) == "" {
		return nil, invalid("audio input is required")
	}
	if unit := audioUnit(request.Model); unit != "" && !info.PriceData.UsePrice {
		return nil, invalid("configure QianwenAI audio unit price per " + unit + "; this model does not report token usage")
	}
	if request.Model == "qwen-audio-3.1-tts-next" && info.IsStream {
		return nil, invalid("qwen-audio-3.1-tts-next does not document streaming output")
	}
	info.DisablePing = true
	input := map[string]any{}
	// Metadata is the provider's input object, including optional zero/false values.
	if len(request.Metadata) > 0 {
		if err := common.Unmarshal(request.Metadata, &input); err != nil || input == nil {
			return nil, invalid("audio metadata must be an object of QianwenAI input parameters")
		}
	}
	switch {
	case request.Model == "qwen-audio-3.1-tts-next":
		input["text_prompt"] = request.Input
		if request.Voice.String() != "" {
			return nil, invalid("qwen-audio-3.1-tts-next uses references, not voice")
		}
	case strings.HasPrefix(request.Model, "fun-music-"):
		input["prompt"] = request.Input
	default:
		input["text"] = request.Input
		if request.Voice.String() != "" {
			input["voice"] = request.Voice.String()
		}
	}
	format := request.ResponseFormat
	if format == "" {
		format, _ = input["format"].(string)
		if format == "" {
			format = "mp3"
			if audioPath(request.Model) == multimodalPath || request.Model == "qwen-audio-3.1-tts-next" {
				format = "wav"
			}
		}
	}
	if audioPath(request.Model) == multimodalPath {
		if info.IsStream {
			if request.ResponseFormat != "" && request.ResponseFormat != "pcm" {
				return nil, invalid("Qwen3-TTS streaming output is PCM")
			}
			format = "pcm"
		} else if format != "wav" {
			return nil, invalid("Qwen3-TTS non-streaming output is WAV")
		}
	} else {
		input["format"] = format
	}
	if request.Speed != nil {
		input["rate"] = request.Speed
	}
	if request.Instructions != "" {
		key := "instruction"
		if audioPath(request.Model) == multimodalPath {
			key = "instructions"
		}
		input[key] = request.Instructions
	}
	if len(request.InputReferences) > 0 {
		input["references"] = request.InputReferences
	}
	c.Set("qianwen_audio_format", format)
	c.Set("qianwen_audio_sse", request.StreamFormat == "sse")
	data, err := common.Marshal(map[string]any{"model": request.Model, "input": input})
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
	}
	if err != nil {
		return nil, err
	}
	var final struct {
		Model string `json:"model"`
	}
	if err := common.Unmarshal(data, &final); err != nil || final.Model != request.Model {
		return nil, invalid("use model mapping to select the audio model; parameter overrides cannot change model")
	}
	return bytes.NewReader(data), nil
}

type audioResult struct {
	nativeError
	Output struct {
		FinishReason string `json:"finish_reason"`
		Audio        struct {
			Data string `json:"data"`
			URL  string `json:"url"`
		} `json:"audio"`
	} `json:"output"`
	Usage *struct {
		dto.Usage
		Characters *int     `json:"characters"`
		Duration   *float64 `json:"duration"`
	} `json:"usage"`
}

func audioUsage(result audioResult, info *relaycommon.RelayInfo) (*dto.Usage, error) {
	if result.Usage == nil {
		return nil, fmt.Errorf("QianwenAI audio response is missing usage")
	}
	usage := result.Usage.Usage
	if usage.PromptTokens == 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails = *usage.InputTokensDetails
	}
	if usage.OutputTokensDetails != nil {
		usage.CompletionTokenDetails = *usage.OutputTokensDetails
	}
	unit := audioUnit(info.UpstreamModelName)
	if unit != "" {
		if !info.PriceData.UsePrice {
			return nil, fmt.Errorf("QianwenAI audio requires unit pricing")
		}
		var count float64
		if unit == "characters" && result.Usage.Characters != nil {
			count = float64(*result.Usage.Characters)
		} else if unit == "seconds" && result.Usage.Duration != nil {
			count = *result.Usage.Duration
		} else {
			return nil, fmt.Errorf("QianwenAI audio response is missing %s usage", unit)
		}
		if count < 0 {
			return nil, fmt.Errorf("negative QianwenAI audio usage")
		}
		info.PriceData.AddOtherRatio(unit, count)
		info.NonTokenUsage = true
	}
	return &usage, nil
}

func audioContentType(format string) string {
	switch format {
	case "wav":
		return "audio/wav"
	case "pcm":
		return "audio/pcm"
	case "opus":
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}

func audioResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return audioStream(c, resp, info)
	}
	var result audioResult
	_, apiErr := readJSONResponse(resp, &result)
	if apiErr != nil {
		return nil, apiErr
	}
	usage, err := audioUsage(result, info)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	data, err := audioData(c, result)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if c.GetBool("qianwen_audio_sse") {
		c.Header("Content-Type", "text/event-stream")
		writeAudioEvent(c, "audio.delta", map[string]any{"audio": base64.StdEncoding.EncodeToString(data)})
		writeAudioEvent(c, "audio.done", map[string]any{"usage": result.Usage})
	} else {
		c.Data(http.StatusOK, audioContentType(c.GetString("qianwen_audio_format")), data)
	}
	return usage, nil
}

func audioData(c *gin.Context, result audioResult) ([]byte, error) {
	encoded := result.Output.Audio.Data
	if encoded == "" && result.Output.Audio.URL != "" {
		var err error
		encoded, _, err = service.GetBase64Data(c, types.NewURLFileSource(result.Output.Audio.URL), "QianwenAI generated audio")
		if err != nil {
			return nil, err
		}
	}
	if encoded == "" {
		return nil, fmt.Errorf("QianwenAI returned no generated audio")
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func writeAudioEvent(c *gin.Context, event string, payload map[string]any) {
	payload["type"] = event
	data, _ := common.Marshal(payload)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data)
	c.Writer.Flush()
}

func audioStream(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var final audioResult
	var failure error
	var complete bool
	var emitted bool
	sse := c.GetBool("qianwen_audio_sse")
	if sse {
		c.Header("Content-Type", "text/event-stream")
	} else {
		c.Header("Content-Type", audioContentType(c.GetString("qianwen_audio_format")))
	}
	helper.ScanSSE(c, resp, info, func(data string, sr *helper.StreamResult) {
		var result audioResult
		if err := common.UnmarshalJsonStr(data, &result); err != nil {
			failure = err
			sr.Stop(err)
			return
		}
		if apiErr := result.apiError(); apiErr != nil {
			failure = apiErr
			sr.Stop(apiErr)
			return
		}
		if result.Usage != nil {
			final.Usage = result.Usage
		} // cumulative, never add chunks
		if result.Output.Audio.URL != "" {
			final.Output.Audio.URL = result.Output.Audio.URL
		}
		if result.Output.Audio.Data != "" {
			decoded, err := base64.StdEncoding.DecodeString(result.Output.Audio.Data)
			if err != nil {
				failure = err
				sr.Stop(err)
				return
			}
			if sse {
				writeAudioEvent(c, "audio.delta", map[string]any{"audio": result.Output.Audio.Data})
			} else if _, err = c.Writer.Write(decoded); err != nil {
				failure = err
				sr.Stop(err)
				return
			} else {
				c.Writer.Flush()
			}
			emitted = true
			c.Set("qianwen_audio_committed", true)
		}
		if result.Output.FinishReason == "stop" {
			complete = true
			sr.Done()
		}
	})
	if failure == nil && !complete {
		failure = fmt.Errorf("QianwenAI audio stream ended before finish_reason=stop")
	}
	var usage *dto.Usage
	if failure == nil {
		usage, failure = audioUsage(final, info)
	}
	// Some upstreams send only the completed file URL. Download once, and never
	// append the complete file after audio chunks that were already forwarded.
	if failure == nil && !emitted {
		var data []byte
		data, failure = audioData(c, final)
		if failure == nil {
			if sse {
				writeAudioEvent(c, "audio.delta", map[string]any{"audio": base64.StdEncoding.EncodeToString(data)})
			} else {
				_, failure = c.Writer.Write(data)
				c.Writer.Flush()
			}
			c.Set("qianwen_audio_committed", true)
		}
	}
	if failure != nil {
		if sse && c.Writer.Written() {
			writeAudioEvent(c, "error", map[string]any{"error": map[string]string{"message": failure.Error(), "type": "upstream_error"}})
		}
		return nil, types.NewErrorWithStatusCode(failure, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if sse {
		writeAudioEvent(c, "audio.done", map[string]any{"usage": final.Usage})
	}
	return usage, nil
}
