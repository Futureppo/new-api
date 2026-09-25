package openai

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/cline"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func clineStreamToJSON(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	badResponse := func(err error) (*dto.Usage, *types.NewAPIError) {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if resp.Body == nil {
		return badResponse(fmt.Errorf("empty Cline response body"))
	}
	// Non-stream requests can retry after an upstream stream failed. Each
	// attempt needs its own terminal status and error list.
	info.StreamStatus = relaycommon.NewStreamStatus()
	var accumulator cline.ChatAccumulator
	var streamErr error
	helper.ScanSSE(c, resp, info, func(data string, result *helper.StreamResult) {
		if err := accumulator.Add(data); err != nil {
			streamErr = err
			result.Stop(err)
		}
	})
	if streamErr != nil {
		return badResponse(streamErr)
	}
	if err := c.Request.Context().Err(); err != nil {
		return badResponse(err)
	}
	status := info.StreamStatus
	if status == nil || status.HasErrors() || (status.EndReason != relaycommon.StreamEndReasonDone && status.EndReason != relaycommon.StreamEndReasonEOF) {
		return badResponse(fmt.Errorf("Cline stream failed: %s", status.Summary()))
	}
	body, err := accumulator.JSON()
	if err != nil {
		return badResponse(err)
	}
	// Reuse ordinary response conversion and accounting without exposing SSE
	// headers or partial content to a non-streaming client.
	converted := *resp
	converted.Header = resp.Header.Clone()
	converted.Header.Set("Content-Type", "application/json")
	for _, name := range []string{"Content-Length", "Transfer-Encoding", "Content-Encoding", "Connection", "X-Accel-Buffering"} {
		converted.Header.Del(name)
	}
	converted.Body = io.NopCloser(bytes.NewReader(body))
	converted.ContentLength = int64(len(body))
	return OpenaiHandler(c, info, &converted)
}
