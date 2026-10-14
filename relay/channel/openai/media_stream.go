package openai

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// relayMediaSSE forwards complete frames (including event/id fields and multi-line
// data), normalizing line endings to LF. Only the observer parses JSON.
// After headers are committed, failures are recorded instead of returned for retry.
func relayMediaSSE(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, observe func(string, []byte) (bool, error), emit ...func(string, []byte) error) {
	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	defer resp.Body.Close()
	stopCancel := context.AfterFunc(c.Request.Context(), func() { _ = resp.Body.Close() })
	defer stopCancel()
	timeout := time.Duration(constant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	timer := time.AfterFunc(timeout, func() { _ = resp.Body.Close() })
	defer timer.Stop()
	helper.SetEventStreamHeaders(c)
	c.Writer.WriteHeader(resp.StatusCode)
	c.Writer.Flush()
	scanner := bufio.NewScanner(resp.Body)
	maxSize := helper.DefaultMaxScannerBufferSize
	if constant.StreamScannerMaxBufferMB > 0 {
		maxSize = constant.StreamScannerMaxBufferMB << 20
	}
	scanner.Buffer(make([]byte, helper.InitialScannerBufferSize), maxSize)
	var frame strings.Builder
	var data []string
	var event string
	terminal := false
	fail := func(err error) { info.StreamStatus.RecordError(err.Error()); logger.LogWarn(c, err.Error()) }
	for scanner.Scan() {
		timer.Reset(timeout)
		if err := c.Request.Context().Err(); err != nil {
			fail(err)
			return
		}
		line := scanner.Text()
		if frame.Len()+len(line)+1 > maxSize {
			fail(fmt.Errorf("media SSE frame exceeds configured limit"))
			return
		}
		frame.WriteString(line)
		frame.WriteByte('\n')
		if line != "" {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
			if strings.HasPrefix(line, "event:") {
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			}
			continue
		}
		// Account for a received final usage even if the client has disconnected.
		var observeErr error
		if len(data) > 0 {
			terminal, observeErr = observe(event, []byte(strings.Join(data, "\n")))
		}
		var writeErr error
		if len(emit) > 0 && emit[0] != nil && observeErr == nil && len(data) > 0 {
			writeErr = emit[0](frame.String(), []byte(strings.Join(data, "\n")))
		} else {
			_, writeErr = c.Writer.Write([]byte(frame.String()))
		}
		if writeErr != nil {
			fail(writeErr)
			return
		}
		c.Writer.Flush()
		info.SetFirstResponseTime()
		if observeErr != nil {
			fail(observeErr)
			return
		}
		if terminal {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			return
		}
		frame.Reset()
		data = nil
		event = ""
	}
	if err := c.Request.Context().Err(); err != nil {
		fail(err)
	} else if err := scanner.Err(); err != nil {
		fail(err)
	} else if !terminal {
		fail(fmt.Errorf("media SSE stream ended without a terminal event"))
	}
}

func openAIMediaJSONHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
		Error any             `json:"error"`
		Text  string          `json:"text"`
	}
	// Plain text, SRT and VTT transcription responses are valid too.
	if err := common.Unmarshal(body, &envelope); err != nil &&
		(info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits || strings.Contains(resp.Header.Get("Content-Type"), "json")) {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if apiErr := dto.GetOpenAIError(envelope.Error); apiErr != nil {
		return nil, types.WithOpenAIError(*apiErr, http.StatusBadGateway)
	}
	c.Set("openai_response_body", body)
	service.IOCopyBytesGracefully(c, resp, body)
	if usage := parseMediaUsage(envelope.Usage); usage != nil {
		c.Set("openai_media_usage_reported", true)
		completeMediaTokenDetails(usage, info.RelayMode)
		return usage, nil
	}
	logger.LogWarn(c, "upstream media response did not report usage; using local estimate")
	usage := &dto.Usage{PromptTokens: info.GetEstimatePromptTokens()}
	usage.TotalTokens = usage.PromptTokens
	return usage, nil
}

func parseMediaUsage(raw json.RawMessage) *dto.Usage {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || common.Unmarshal(raw, &fields) != nil {
		return nil
	}
	// Some transcription models report duration instead of token counts. Keep
	// the existing local estimate in that case instead of treating it as zero.
	for _, key := range []string{"total_tokens", "input_tokens", "output_tokens", "prompt_tokens", "completion_tokens"} {
		if _, ok := fields[key]; ok {
			var usage dto.Usage
			if common.Unmarshal(raw, &usage) != nil {
				return nil
			}
			// Transcriptions use the singular spelling; images/Responses use plural.
			var details struct {
				Input  *dto.InputTokenDetails  `json:"input_token_details"`
				Output *dto.OutputTokenDetails `json:"output_token_details"`
			}
			if common.Unmarshal(raw, &details) != nil {
				return nil
			}
			if usage.InputTokensDetails == nil {
				usage.InputTokensDetails = details.Input
			}
			if usage.OutputTokensDetails == nil {
				usage.OutputTokensDetails = details.Output
			}
			return normalizeMediaUsage(&usage)
		}
	}
	return nil
}

func normalizeMediaUsage(upstream *dto.Usage) *dto.Usage {
	usage := *upstream
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails = *usage.InputTokensDetails
	}
	if usage.OutputTokensDetails != nil {
		usage.CompletionTokenDetails = *usage.OutputTokensDetails
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return &usage
}

func completeMediaTokenDetails(usage *dto.Usage, mode int) {
	if usage.OutputTokensDetails == nil {
		switch mode {
		case relayconstant.RelayModeAudioSpeech:
			usage.CompletionTokenDetails.AudioTokens = usage.CompletionTokens
		case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
			usage.CompletionTokenDetails.ImageTokens = usage.CompletionTokens
		}
	}
	if usage.PromptTokensDetails.TextTokens == 0 {
		usage.PromptTokensDetails.TextTokens = max(0, usage.PromptTokens-usage.PromptTokensDetails.AudioTokens-usage.PromptTokensDetails.ImageTokens)
	}
	if usage.CompletionTokenDetails.TextTokens == 0 {
		usage.CompletionTokenDetails.TextTokens = max(0, usage.CompletionTokens-usage.CompletionTokenDetails.AudioTokens-usage.CompletionTokenDetails.ImageTokens)
	}
}

func openAIMediaStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) *dto.Usage {
	var usage *dto.Usage
	var output strings.Builder
	var speechBytes int
	relayMediaSSE(c, resp, info, func(event string, data []byte) (bool, error) {
		if string(data) == "[DONE]" {
			return true, nil
		}
		var chunk struct {
			Type  string          `json:"type"`
			Usage json.RawMessage `json:"usage"`
			Delta string          `json:"delta"`
			Audio string          `json:"audio"`
			Text  string          `json:"text"`
			Error any             `json:"error"`
		}
		if err := common.Unmarshal(data, &chunk); err != nil {
			return false, fmt.Errorf("invalid media SSE event: %w", err)
		}
		if chunk.Type != "" {
			event = chunk.Type
		}
		if reported := parseMediaUsage(chunk.Usage); reported != nil {
			usage = reported
			c.Set("openai_media_usage_reported", true)
		}
		if strings.HasSuffix(event, "text.delta") {
			output.WriteString(chunk.Delta)
		}
		if event == "speech.audio.delta" {
			speechBytes += base64.StdEncoding.DecodedLen(len(chunk.Audio)) - (len(chunk.Audio) - len(strings.TrimRight(chunk.Audio, "=")))
		}
		if event == "transcript.text.done" && chunk.Text != "" {
			output.Reset()
			output.WriteString(chunk.Text)
		}
		if event == "error" || strings.HasSuffix(event, ".failed") || chunk.Error != nil {
			return true, fmt.Errorf("upstream media stream error (%s)", event)
		}
		switch event {
		case "image_generation.completed", "image_edit.completed", "speech.audio.done", "transcript.text.done":
			return true, nil
		}
		return false, nil
	})
	if usage == nil {
		logger.LogWarn(c, "upstream media stream did not report usage; using local estimate")
		usage = &dto.Usage{PromptTokens: info.GetEstimatePromptTokens()}
		if output.Len() > 0 {
			usage.CompletionTokens = service.CountTextToken(output.String(), info.UpstreamModelName)
		}
		if speechBytes > 0 {
			// Match the existing TTS size estimate without buffering the audio.
			usage.CompletionTokens = int(math.Ceil(float64(speechBytes) / 1000))
			if req, ok := info.Request.(*dto.AudioRequest); ok && req.ResponseFormat == "pcm" {
				usage.CompletionTokens = int(math.Round(math.Ceil(float64(speechBytes)/48000) / 60 * 1000))
			}
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	completeMediaTokenDetails(usage, info.RelayMode)
	return usage
}
