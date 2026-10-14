package vertex

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func (a *Adaptor) convertAudio(c *gin.Context, info *relaycommon.RelayInfo, r dto.AudioRequest) (io.Reader, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	config := map[string]any{}
	parts := []any{}
	a.audioFormat = r.ResponseFormat
	a.action = "generateContent"
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		if a.capability.Family != vertexmodel.Speech {
			return nil, errors.New("speech requires a Vertex Gemini TTS model")
		}
		if strings.TrimSpace(r.Input) == "" {
			return nil, errors.New("input is required")
		}
		if r.Speed != nil && *r.Speed != 1 {
			return nil, errors.New("Vertex TTS does not support speed conversion")
		}
		if r.StreamFormat != "" && r.StreamFormat != "audio" {
			return nil, errors.New("Vertex speech streaming uses raw PCM; use stream_format=audio")
		}
		if a.audioFormat == "" {
			a.audioFormat = "wav"
		}
		if a.audioFormat != "wav" && a.audioFormat != "pcm" && a.audioFormat != "native" {
			return nil, errors.New("Vertex speech supports wav, pcm, or native response_format")
		}
		if r.IsStream(c) {
			if a.audioFormat != "pcm" {
				return nil, errors.New("streaming speech requires response_format=pcm")
			}
			a.action = "streamGenerateContent"
			info.IsStream = true
			info.DisablePing = true
		}
		a.responseKind = "speech"
		config["responseModalities"] = []string{"AUDIO"}
		text := r.Input
		if r.Instructions != "" {
			text = r.Instructions + "\n" + text
		}
		parts = append(parts, map[string]any{"text": text})
		voice := r.Voice.Name
		if voice == "" {
			voice = "Kore"
		}
		if strings.HasPrefix(a.capability.Model, "gemini-3.8-") {
			config["speechConfig"] = map[string]any{"voiceConfig": map[string]any{"voice": voice}}
			format := "AUDIO_WAV"
			if a.audioFormat == "pcm" {
				format = "AUDIO_L16"
			}
			config["responseFormat"] = []any{map[string]any{"audio": map[string]any{"mimeType": format}}}
		} else {
			config["speechConfig"] = map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": voice}}}
		}
	} else {
		if info.RelayMode != relayconstant.RelayModeAudioTranscription {
			return nil, errors.New("Vertex only supports synchronous audio transcription")
		}
		if a.capability.Family != vertexmodel.Transcribe && a.capability.Family != vertexmodel.Gemini {
			return nil, errors.New("transcription requires a Gemini audio-input model")
		}
		if r.IsStream(c) {
			return nil, errors.New("use native streamGenerateContent for streaming transcription")
		}
		if a.audioFormat == "" {
			a.audioFormat = "json"
		}
		if a.audioFormat != "json" && a.audioFormat != "text" {
			return nil, errors.New("transcription response_format must be json or text; native transcription preserves real timestamp fields")
		}
		if len(r.TimestampGranularities) > 0 {
			return nil, errors.New("use native audioTranscriptionConfig for timestamp requests")
		}
		a.responseKind = "transcribe"
		uploads, err := mediaUploads(c, "file")
		if err != nil {
			return nil, err
		}
		if r.SourceURL != nil && *r.SourceURL != "" {
			f, err := mediaSources(c, raw(*r.SourceURL))
			if err != nil {
				return nil, err
			}
			uploads = append(uploads, f...)
		}
		if len(r.InputAudio) > 0 {
			var audio struct {
				Data   string `json:"data"`
				Format string `json:"format"`
			}
			if err := common.Unmarshal(r.InputAudio, &audio); err != nil {
				return nil, err
			}
			if audio.Data == "" || audio.Format == "" {
				return nil, errors.New("input_audio requires data and format")
			}
			files, err := mediaSources(c, raw("data:audio/"+audio.Format+";base64,"+audio.Data))
			if err != nil {
				return nil, err
			}
			uploads = append(uploads, files...)
		}
		if len(uploads) != 1 {
			return nil, errors.New("transcription requires exactly one audio file")
		}
		if !strings.HasPrefix(uploads[0].MimeType, "audio/") {
			return nil, errors.New("transcription file must have an audio MIME type")
		}
		parts = append(parts, inlinePart(uploads[0]))
		if a.capability.Family == vertexmodel.Transcribe {
			config["audioTranscriptionConfig"] = map[string]any{}
		} else {
			parts = append(parts, map[string]any{"text": "Transcribe the audio verbatim. Return only the transcription, without commentary."})
		}
		if r.Instructions != "" {
			parts = append(parts, map[string]any{"text": r.Instructions})
		}
		if len(r.Language) > 0 {
			var language string
			if err := common.Unmarshal(r.Language, &language); err != nil {
				return nil, err
			}
			if language != "" {
				parts = append(parts, map[string]any{"text": "The audio language is " + language + "."})
			}
		}
		if r.Temperature != nil {
			config["temperature"] = *r.Temperature
		}
	}
	body, err := common.Marshal(map[string]any{"contents": []any{map[string]any{"role": "user", "parts": parts}}, "generationConfig": config})
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		body, err = relaycommon.ApplyParamOverrideWithRelayInfo(body, info)
		if err != nil {
			return nil, err
		}
	}
	return bytes.NewReader(body), nil
}

type audioEnvelope struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text   string `json:"text"`
				Inline *struct {
					Data string `json:"data"`
					MIME string `json:"mimeType"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func audioParts(data []byte) ([]byte, string, string, error) {
	var envelope audioEnvelope
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, "", "", err
	}
	var audio []byte
	var mime, text string
	// Compatibility endpoints return a single candidate, never concatenate alternatives.
	if len(envelope.Candidates) > 0 {
		for _, part := range envelope.Candidates[0].Content.Parts {
			text += part.Text
			if part.Inline != nil {
				if !strings.HasPrefix(strings.ToLower(part.Inline.MIME), "audio/") {
					continue
				}
				if mime != "" && mime != part.Inline.MIME {
					return nil, "", "", errors.New("inconsistent audio MIME types")
				}
				mime = part.Inline.MIME
				b, err := base64.StdEncoding.DecodeString(part.Inline.Data)
				if err != nil {
					return nil, "", "", err
				}
				audio = append(audio, b...)
			}
		}
	}
	return audio, mime, text, nil
}

func (a *Adaptor) audioResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	usage := dto.Usage{}
	if info.IsStream {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64<<10), 64<<20)
		count := 0
		for scanner.Scan() {
			if err := c.Request.Context().Err(); err != nil {
				return &usage, badResponse(err)
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			u, present, err := responseUsage([]byte(data))
			if err != nil {
				return &usage, badResponse(err)
			}
			if present {
				usage = u
			}
			audio, mime, _, err := audioParts([]byte(data))
			if err != nil {
				return &usage, badResponse(err)
			}
			if len(audio) == 0 {
				continue
			}
			audio, _, err = convertAudioBytes(audio, mime, "pcm")
			if err != nil {
				return &usage, badResponse(err)
			}
			c.Header("Content-Type", "audio/pcm")
			if _, err = c.Writer.Write(audio); err != nil {
				return &usage, badResponse(err)
			}
			c.Writer.Flush()
			count += len(audio)
			info.SendResponseCount++
		}
		if scanner.Err() != nil {
			return &usage, badResponse(scanner.Err())
		}
		if count == 0 {
			return nil, badResponse(errors.New("Vertex returned no audio"))
		}
		return &usage, nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse(err)
	}
	usage, _, err = responseUsage(data)
	if err != nil {
		return nil, badResponse(err)
	}
	audio, mime, text, err := audioParts(data)
	if err != nil {
		return nil, badResponse(err)
	}
	if a.responseKind == "transcribe" {
		if text == "" {
			return nil, badResponse(errors.New("Vertex returned no transcription"))
		}
		if a.audioFormat == "text" {
			c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(text))
		} else {
			c.JSON(http.StatusOK, gin.H{"text": text, "usage": usage})
		}
		return &usage, nil
	}
	if len(audio) == 0 {
		return nil, badResponse(errors.New("Vertex returned no audio (empty or safety-filtered result)"))
	}
	audio, mime, err = convertAudioBytes(audio, mime, a.audioFormat)
	if err != nil {
		return nil, badResponse(err)
	}
	c.Data(http.StatusOK, mime, audio)
	return &usage, nil
}

// PCM container conversion only; there is no codec transcoding or resampling.
func convertAudioBytes(data []byte, mime, format string) ([]byte, string, error) {
	if format == "native" {
		return data, mime, nil
	}
	rate := 24000
	isPCM := strings.HasPrefix(strings.ToLower(mime), "audio/l16") || strings.HasPrefix(strings.ToLower(mime), "audio/pcm")
	for _, parameter := range strings.Split(mime, ";")[1:] {
		if value, ok := strings.CutPrefix(strings.TrimSpace(parameter), "rate="); ok {
			v, err := strconv.Atoi(value)
			if err != nil || v <= 0 {
				return nil, "", errors.New("invalid PCM sample rate")
			}
			rate = v
		}
	}
	isWAV := len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
	if isWAV {
		if format == "wav" {
			return data, "audio/wav", nil
		}
		var pcm []byte
		valid := false
		for pos := 12; pos+8 <= len(data); {
			size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
			start := pos + 8
			if size > len(data)-start {
				return nil, "", errors.New("invalid WAV chunk")
			}
			switch string(data[pos : pos+4]) {
			case "fmt ":
				if size < 16 {
					return nil, "", errors.New("invalid WAV format")
				}
				valid = binary.LittleEndian.Uint16(data[start:]) == 1 && binary.LittleEndian.Uint16(data[start+2:]) == 1 && binary.LittleEndian.Uint16(data[start+14:]) == 16
			case "data":
				pcm = data[start : start+size]
			}
			pos = start + size + (size % 2)
		}
		if !valid || len(pcm) == 0 {
			return nil, "", errors.New("WAV must contain mono 16-bit PCM")
		}
		return pcm, "audio/pcm", nil
	}
	if !isPCM || len(data)%2 != 0 {
		return nil, "", fmt.Errorf("cannot convert Vertex audio MIME %q without transcoding", mime)
	}
	if format == "pcm" {
		return data, "audio/pcm", nil
	}
	if len(data) > int(^uint32(0))-36 {
		return nil, "", errors.New("audio exceeds WAV size limit")
	}
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(len(data)+36))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], uint32(rate))
	binary.LittleEndian.PutUint32(header[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(len(data)))
	return append(header, data...), "audio/wav", nil
}
