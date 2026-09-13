package mistral

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type NativeResult struct {
	Usage *dto.Usage
	Pages *int
}

// Inspect a copy for billing only; the bytes delivered to the client are never
// normalized, force-formatted or augmented with synthetic completion events.
func (r *NativeResult) observe(data []byte) (terminal, failed bool) {
	var event struct {
		Type      string     `json:"type"`
		Object    string     `json:"object"`
		Usage     *dto.Usage `json:"usage"`
		Error     any        `json:"error"`
		UsageInfo *struct {
			Pages *int `json:"pages_processed"`
		} `json:"usage_info"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if common.Unmarshal(data, &event) != nil {
		return false, false
	}
	if event.Usage != nil {
		normalizeUsage(event.Usage)
		r.Usage = event.Usage
	}
	if event.UsageInfo != nil {
		r.Pages = event.UsageInfo.Pages
	}
	failed = event.Type == "error" || event.Object == "error" || event.Error != nil
	for _, choice := range event.Choices {
		failed = failed || choice.FinishReason == "error"
	}
	return failed || event.Type == "transcription.done" || event.Type == "speech.audio.done", failed
}

func (a *Adaptor) NativeResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*NativeResult, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, badResponse(errors.New("empty Mistral response"))
	}
	defer resp.Body.Close()
	result := &NativeResult{}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		info.IsStream = true
		return a.nativeStream(c, resp, info, result)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse(err)
	}
	_, failed := result.observe(data)
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, resp, data)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || failed {
		apiErr := errorFromBody(data, resp.StatusCode)
		if apiErr == nil {
			apiErr = types.NewOpenAIError(fmt.Errorf("Mistral upstream returned HTTP %d", resp.StatusCode), types.ErrorCodeBadResponse, resp.StatusCode)
		}
		apiErr = types.NewError(apiErr, types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
		return result, apiErr
	}
	return result, nil
}

func (a *Adaptor) nativeStream(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, result *NativeResult) (*NativeResult, *types.NewAPIError) {
	info.StreamStatus = relaycommon.NewStreamStatus()
	stopCancel := context.AfterFunc(c.Request.Context(), func() { _ = resp.Body.Close() })
	defer stopCancel()
	timeout := time.Duration(constant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	timer := time.AfterFunc(timeout, func() { _ = resp.Body.Close() })
	defer timer.Stop()
	for key, values := range resp.Header {
		if len(values) > 0 && key != "Content-Length" && key != "Transfer-Encoding" {
			c.Writer.Header()[key] = values
		}
	}
	helper.SetEventStreamHeaders(c)
	reader := bufio.NewReader(resp.Body)
	var data strings.Builder
	var frameBytes int
	var eventName string
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			timer.Reset(timeout)
			frameBytes += len(line)
			if frameBytes > helper.DefaultMaxScannerBufferSize {
				return result, badResponse(errors.New("Mistral SSE event too large"))
			}
			trimmed := strings.TrimRight(line, "\r\n")
			terminal, failed := false, false
			if strings.HasPrefix(trimmed, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			}
			if strings.HasPrefix(trimmed, "data:") {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.WriteString(strings.TrimPrefix(strings.TrimPrefix(trimmed, "data:"), " "))
			}
			if trimmed == "" {
				if strings.TrimSpace(data.String()) == "[DONE]" {
					terminal = true
				} else {
					terminal, failed = result.observe([]byte(data.String()))
				}
				failed = failed || eventName == "error" || strings.HasSuffix(eventName, ".error")
				eventName = ""
				data.Reset()
				frameBytes = 0
			}
			info.SetFirstResponseTime()
			if _, writeErr := c.Writer.WriteString(line); writeErr != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, writeErr)
				return result, badResponse(writeErr)
			}
			if flushErr := helper.FlushWriter(c); flushErr != nil {
				return result, badResponse(flushErr)
			}
			if failed {
				failure := errors.New("Mistral native stream returned an error event")
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, failure)
				info.StreamStatus.RecordError(failure.Error())
				return result, badResponse(failure)
			}
			if terminal {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				return result, nil
			}
		}
		if err != nil {
			reason := relaycommon.StreamEndReasonScannerErr
			if c.Request.Context().Err() != nil {
				reason = relaycommon.StreamEndReasonClientGone
			}
			info.StreamStatus.SetEndReason(reason, err)
			return result, badResponse(fmt.Errorf("Mistral native stream ended before its terminal event: %w", err))
		}
	}
}
