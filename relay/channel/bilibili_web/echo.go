package bilibili_web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type echoOptions struct {
	Source *string `json:"source_language,omitempty"`
	Target *string `json:"target_language,omitempty"`
}

type echoInput struct {
	audio                         []byte
	format, source, target, model string
}

func prepareEcho(c *gin.Context, info *relaycommon.RelayInfo, req *dto.GeneralOpenAIRequest) (*echoInput, error) {
	if err := validateCommon(req); err != nil {
		return nil, err
	}
	if req.Stream != nil && *req.Stream || info.IsStream {
		return nil, invalid("Echo supports non-streaming audio Chat only; set stream=false")
	}
	if !info.PriceData.UsePrice && !info.IsChannelTest {
		return nil, invalid("Echo does not report token usage; configure per-request pricing for this model")
	}
	// Audio task fields have no equivalent for text generation settings. Reject
	// all recognized extras instead of accepting parameters that would be lost.
	encoded, err := common.Marshal(req)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = common.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	allowed := map[string]bool{
		"model": true, "messages": true, "stream": true, "stream_options": true,
		"modalities": true, "audio": true, "bilibili_web": true,
		"user": true, "metadata": true, "store": true, "n": true,
		"tools": true, "tool_choice": true, "parallel_tool_calls": true,
	}
	for key, value := range fields {
		if !allowed[key] && hasValue(value) {
			return nil, invalid("Echo does not support " + key)
		}
	}
	var modalities []string
	if common.Unmarshal(req.Modalities, &modalities) != nil || len(modalities) == 0 {
		return nil, invalid("Echo requires modalities containing audio")
	}
	hasAudio := false
	for _, modality := range modalities {
		if modality != "text" && modality != "audio" {
			return nil, invalid("Echo modalities must be text and/or audio")
		}
		hasAudio = hasAudio || modality == "audio"
	}
	if !hasAudio {
		return nil, invalid("this Echo model requires audio output")
	}
	var output struct {
		Format string `json:"format"`
		Voice  string `json:"voice"`
	}
	if hasValue(req.Audio) {
		var fields map[string]json.RawMessage
		if common.Unmarshal(req.Audio, &fields) != nil || fields == nil {
			return nil, invalid("audio must be an object")
		}
		for key := range fields {
			if key != "format" && key != "voice" {
				return nil, invalid("unsupported Echo audio option: " + key)
			}
		}
		if err := common.Unmarshal(req.Audio, &output); err != nil {
			return nil, invalid("invalid audio output settings")
		}
	}
	if output.Format != "" && output.Format != "wav" || output.Voice != "" && output.Voice != "source" {
		return nil, invalid("Echo supports WAV output with the source voice only; omit voice or use source")
	}
	input := &echoInput{source: info.ChannelOtherSettings.BilibiliWebSourceLanguage, target: info.ChannelOtherSettings.BilibiliWebTargetLanguage}
	if input.source == "" {
		input.source = "zh"
	}
	if input.target == "" {
		input.target = "en"
	}
	var options echoOptions
	if hasValue(req.BilibiliWeb) {
		var fields map[string]json.RawMessage
		if common.Unmarshal(req.BilibiliWeb, &fields) != nil || fields == nil {
			return nil, invalid("bilibili_web must be an object")
		}
		for key := range fields {
			if key != "source_language" && key != "target_language" {
				return nil, invalid("unsupported bilibili_web option: " + key)
			}
		}
		if err := common.Unmarshal(req.BilibiliWeb, &options); err != nil {
			return nil, invalid("invalid bilibili_web language settings")
		}
	}
	if options.Source != nil {
		input.source = *options.Source
	}
	if options.Target != nil {
		input.target = *options.Target
	}
	if input.source == "" || input.target == "" {
		return nil, invalid("Echo source_language and target_language must not be empty")
	}
	if err := ValidateLanguages(input.source, input.target); err != nil {
		return nil, err
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		return nil, invalid("Echo accepts one user message containing one audio input")
	}
	raw, _ := common.Marshal(req.Messages[0].Content)
	var parts []struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Audio *struct {
			Data   string `json:"data"`
			Format string `json:"format"`
		} `json:"input_audio"`
	}
	if common.Unmarshal(raw, &parts) != nil {
		return nil, invalid("Echo requires input_audio content")
	}
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) == "" {
			continue
		}
		if part.Type != "input_audio" || part.Audio == nil || input.audio != nil {
			return nil, invalid("Echo accepts one input_audio only; set translation direction in bilibili_web, not message text")
		}
		if part.Audio.Format != "wav" && part.Audio.Format != "mp3" {
			return nil, invalid("input_audio.format must be wav or mp3")
		}
		if len(part.Audio.Data) > 8<<20 {
			return nil, invalid("input audio exceeds 6 MiB")
		}
		var err error
		input.audio, err = base64.StdEncoding.Strict().DecodeString(part.Audio.Data)
		if err != nil || len(input.audio) == 0 {
			return nil, invalid("input_audio.data must be valid non-empty base64")
		}
		input.format = part.Audio.Format
	}
	if len(input.audio) == 0 {
		return nil, invalid("input_audio is required")
	}
	duration, err := common.GetAudioDuration(c.Request.Context(), bytes.NewReader(input.audio), "."+input.format)
	if err != nil || duration <= 0 || duration > 10 {
		return nil, invalid("Echo input must be a valid audio clip no longer than 10 seconds")
	}
	input.model = "2b"
	if req.Model == "index-echo-9b-s2st" {
		input.model = "9b"
	}
	return input, nil
}

func ValidateLanguages(source, target string) error {
	if source == "" {
		source = "zh"
	}
	if target == "" {
		target = "en"
	}
	if source != "zh" && source != "en" && source != "ja" {
		return invalid("Echo source_language must be zh, en or ja")
	}
	if target != "zh" && target != "en" && target != "ja" && target != "es" && target != "ko" {
		return invalid("Echo target_language must be zh, en, ja, es or ko")
	}
	if source == target {
		return invalid("Echo requires different source and target languages")
	}
	return nil
}

type echoResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Text  string `json:"text"`
	Audio string `json:"audio_b64"`
}

type echoEnvelope struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error"`
	JobID  string      `json:"job_id"`
	Status string      `json:"status"`
	Echo   *echoResult `json:"echo"`
}

func (a *Adaptor) echoRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.GeneralOpenAIRequest) (*http.Response, error) {
	input, err := prepareEcho(c, info, req)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("file", "audio."+input.format)
	if err != nil {
		return nil, err
	}
	if _, err = file.Write(input.audio); err != nil {
		return nil, err
	}
	for name, value := range map[string]string{"model": input.model, "src": input.source, "lang": input.target} {
		if err = form.WriteField(name, value); err != nil {
			return nil, err
		}
	}
	if err = form.Close(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), a.requestTimeout)
	defer cancel()
	resp, err := a.send(ctx, c, info, http.MethodPost, "/api/echo/s2st", form.FormDataContentType(), &body)
	if err != nil {
		return nil, upstreamError("Echo submission failed: " + err.Error())
	}
	submitted, err := decodeEcho(resp)
	if err != nil {
		return nil, err
	}
	if submitted.JobID == "" {
		return nil, upstreamError("Echo did not return a job_id")
	}
	timer := time.NewTicker(a.pollInterval)
	defer timer.Stop()
	notFound := 0
	for {
		select {
		case <-ctx.Done():
			return nil, upstreamError("Echo polling stopped: " + ctx.Err().Error())
		case <-timer.C:
		}
		path := "/api/echo/result?job_id=" + url.QueryEscape(submitted.JobID)
		resp, err = a.send(ctx, c, info, http.MethodGet, path, "application/json", nil)
		if err != nil {
			return nil, upstreamError("Echo polling failed: " + err.Error())
		}
		// The website allows a short propagation delay after job submission.
		if resp.StatusCode == http.StatusNotFound && notFound < 3 {
			resp.Body.Close()
			notFound++
			continue
		}
		result, err := decodeEcho(resp)
		if err != nil {
			return nil, err
		}
		notFound = 0
		if result.Status == "failed" || result.Status == "error" {
			return nil, upstreamError("Echo task failed: " + result.Error)
		}
		if result.Status != "done" {
			continue
		}
		if result.Echo == nil || !result.Echo.OK {
			message := "missing result"
			if result.Echo != nil {
				message = result.Echo.Error
			}
			return nil, upstreamError("Echo task failed: " + message)
		}
		audio, err := base64.StdEncoding.Strict().DecodeString(result.Echo.Audio)
		if err != nil || len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" || strings.TrimSpace(result.Echo.Text) == "" {
			return nil, upstreamError("Echo returned invalid WAV audio or an empty translation")
		}
		duration, err := common.GetAudioDuration(ctx, bytes.NewReader(audio), ".wav")
		if err != nil || duration <= 0 {
			return nil, upstreamError("Echo returned an invalid WAV recording")
		}
		now := time.Now().Unix()
		response := map[string]any{
			"id": "chatcmpl-" + common.GetUUID(), "object": "chat.completion", "created": now, "model": a.responseModel,
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{
				"role": "assistant", "content": result.Echo.Text,
				"audio": map[string]any{"id": "audio_" + common.GetUUID(), "data": result.Echo.Audio, "transcript": result.Echo.Text, "expires_at": now},
			}}},
		}
		data, err := common.Marshal(response)
		if err != nil {
			return nil, err
		}
		a.echoCompleted = true
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(data))}, nil
	}
}

func decodeEcho(resp *http.Response) (*echoEnvelope, error) {
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, 16<<20)
	if err != nil {
		return nil, upstreamError(err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		return nil, upstreamError(fmt.Sprintf("Echo returned HTTP %d", resp.StatusCode))
	}
	var result echoEnvelope
	if common.Unmarshal(data, &result) != nil {
		return nil, upstreamError("Echo returned invalid JSON")
	}
	if !result.OK {
		return nil, upstreamError("Echo request failed: " + result.Error)
	}
	return &result, nil
}
