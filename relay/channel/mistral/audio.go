package mistral

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	if info.RelayMode != relayconstant.RelayModeAudioTranscription {
		return nil, unsupported()
	}
	if info.IsStream || request.IsStream(c) {
		return nil, invalidRequest("Mistral streaming transcription is not supported")
	}
	a.audioFormat = request.ResponseFormat
	if a.audioFormat == "" {
		a.audioFormat = "json"
	}
	switch a.audioFormat {
	case "json", "text", "verbose_json", "srt", "vtt":
	default:
		return nil, invalidRequest("unsupported transcription response_format")
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, invalidRequest("invalid multipart transcription request")
	}
	defer form.RemoveAll()
	for _, value := range form.Value["prompt"] {
		if strings.TrimSpace(value) != "" {
			return nil, invalidRequest("Mistral transcription does not support prompt")
		}
	}
	for _, value := range form.Value["stream"] {
		stream, err := strconv.ParseBool(value)
		if err != nil || stream {
			return nil, invalidRequest("Mistral streaming transcription is not supported")
		}
	}
	granularities := append(append([]string{}, form.Value["timestamp_granularities[]"]...), form.Value["timestamp_granularities"]...)
	unique := []string{}
	for _, value := range granularities {
		if value != "segment" && value != "word" {
			return nil, invalidRequest("timestamp_granularities must contain segment or word")
		}
		if !containsString(unique, value) {
			unique = append(unique, value)
		}
	}
	if len(unique) == 0 && (a.audioFormat == "verbose_json" || a.audioFormat == "srt" || a.audioFormat == "vtt") {
		unique = []string{"segment"}
	}
	if len(unique) > 1 {
		return nil, invalidRequest("Mistral transcription supports only one timestamp granularity per request: segment or word")
	}
	a.audioGranularity = "segment"
	if len(unique) == 1 && unique[0] == "word" {
		a.audioGranularity = "word"
	}
	if len(form.File["file"]) != 1 {
		return nil, invalidRequest("exactly one audio file is required")
	}
	fileHeader := form.File["file"][0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", request.Model); err != nil {
		return nil, err
	}
	for _, name := range []string{"language", "temperature", "diarize", "context_bias"} {
		for _, value := range form.Value[name] {
			if err := writer.WriteField(name, value); err != nil {
				return nil, err
			}
		}
	}
	for _, value := range unique {
		if err := writer.WriteField("timestamp_granularities", value); err != nil {
			return nil, err
		}
	}
	part, err := writer.CreateFormFile("file", fileHeader.Filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if a.audioFormat == "verbose_json" {
		if _, err := file.Seek(0, io.SeekStart); err == nil {
			if duration, err := common.GetAudioDuration(c.Request.Context(), file, filepath.Ext(fileHeader.Filename)); err == nil {
				a.audioDuration = &duration
			}
		}
	}
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return &body, nil
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func (a *Adaptor) audioResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse(err)
	}
	if apiErr := errorFromBody(data, http.StatusBadGateway); apiErr != nil {
		return nil, apiErr
	}
	var response map[string]any
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, badResponse(err)
	}
	text, ok := response["text"].(string)
	if !ok {
		return nil, badResponse(errors.New("Mistral transcription response has no text"))
	}
	usage, err := readUsage(response)
	if err != nil {
		return nil, badResponse(err)
	}
	contentType := "application/json"
	result := map[string]any{"text": text}
	switch a.audioFormat {
	case "text":
		contentType = "text/plain; charset=utf-8"
		data = []byte(text)
	case "verbose_json", "srt", "vtt":
		segments, _ := response["segments"].([]any)
		entries := make([]map[string]any, 0, len(segments))
		for i, value := range segments {
			segment, _ := value.(map[string]any)
			start, okStart := segment["start"].(float64)
			end, okEnd := segment["end"].(float64)
			segmentText, okText := segment["text"].(string)
			if !okStart || !okEnd || !okText || start < 0 || end < start {
				return nil, badResponse(errors.New("invalid transcription timestamps"))
			}
			entries = append(entries, map[string]any{"id": i, "start": start, "end": end, "text": segmentText})
		}
		if a.audioFormat == "verbose_json" {
			result["task"] = "transcribe"
			if language, ok := response["language"].(string); ok && language != "" {
				result["language"] = language
			}
			if a.audioDuration != nil {
				result["duration"] = *a.audioDuration
			}
			if a.audioGranularity == "word" {
				words := make([]map[string]any, len(entries))
				for i, entry := range entries {
					words[i] = map[string]any{"word": strings.TrimSpace(entry["text"].(string)), "start": entry["start"], "end": entry["end"]}
				}
				result["words"] = words
			} else {
				result["segments"] = entries
			}
		} else {
			if len(entries) == 0 && strings.TrimSpace(text) != "" {
				return nil, badResponse(errors.New("upstream did not return timestamps required for subtitles"))
			}
			var subtitles strings.Builder
			separator := ","
			if a.audioFormat == "vtt" {
				contentType = "text/vtt; charset=utf-8"
				separator = "."
				subtitles.WriteString("WEBVTT\n\n")
			} else {
				contentType = "application/x-subrip; charset=utf-8"
			}
			for i, entry := range entries {
				if a.audioFormat == "srt" {
					fmt.Fprintf(&subtitles, "%d\n", i+1)
				}
				fmt.Fprintf(&subtitles, "%s --> %s\n%s\n\n", subtitleTime(entry["start"].(float64), separator), subtitleTime(entry["end"].(float64), separator), entry["text"])
			}
			data = []byte(subtitles.String())
		}
	}
	if contentType == "application/json" {
		data, err = common.Marshal(result)
		if err != nil {
			return nil, badResponse(err)
		}
	}
	// Converted text/subtitles must not inherit the native JSON content type/length.
	resp.Header.Set("Content-Type", contentType)
	resp.Header.Del("Content-Length")
	service.IOCopyBytesGracefully(c, resp, data)
	return usage, nil
}

func subtitleTime(seconds float64, separator string) string {
	ms := int64(math.Round(seconds * 1000))
	return fmt.Sprintf("%02d:%02d:%02d%s%03d", ms/3600000, ms/60000%60, ms/1000%60, separator, ms%1000)
}
