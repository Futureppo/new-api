package opencode

// Adapted from piexian/new-api (AGPL-3.0), commit afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f.
// https://github.com/piexian/new-api/tree/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f/relay/channel/opencode

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func freeResponseError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) doFreeRequest(c *gin.Context, info *relaycommon.RelayInfo, reader io.Reader) (any, error) {
	limit := int64(constant.MaxRequestBodyMB) << 20
	if limit <= 0 {
		limit = 128 << 20
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("request body exceeds size limit")
	}
	body, injected, err := a.shapeFreeRequest(body)
	if err != nil {
		return nil, err
	}
	relaycommon.SetConversationUpstreamRequest(info, body)
	resp, err := channel.DoApiRequest(a, c, info, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, nil
	}
	if !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		_ = resp.Body.Close()
		return nil, freeResponseError(errors.New("upstream did not return the requested stream"))
	}
	stream := newFreeStream(c.Request.Context(), resp.Body, a.freeRequestMode, injected, time.Duration(constant.StreamingTimeout)*time.Second)
	stream.declared = map[string]bool{}
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		name := tool.Get("name").String()
		if a.freeRequestMode == constant.OpenCodeEndpointChat {
			name = tool.Get("function.name").String()
		}
		if name != "" && !injected[name] {
			stream.declared[name] = true
		}
	}
	if info.IsStream {
		a.freeResponseStream = stream
		resp.Body = stream
		return resp, nil
	}
	defer stream.Close()
	body, err = collapseFreeStream(stream)
	if err != nil {
		return nil, freeResponseError(err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Transfer-Encoding")
	resp.ContentLength = int64(len(body))
	resp.TransferEncoding = nil
	return resp, nil
}

// Suppress legacy handlers' success terminators after a validated upstream failure.
type freeStreamWriter struct {
	gin.ResponseWriter
	stream *freeStream
}

func (w *freeStreamWriter) Write(data []byte) (int, error) {
	if w.stream.Err() != nil {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}
func (w *freeStreamWriter) WriteString(data string) (int, error) {
	if w.stream.Err() != nil {
		return len(data), nil
	}
	return w.ResponseWriter.WriteString(data)
}
func (w *freeStreamWriter) Flush() {
	if w.stream.Err() == nil {
		w.ResponseWriter.Flush()
	}
}
func (w *freeStreamWriter) WriteHeader(code int) {
	if w.stream.Err() == nil {
		w.ResponseWriter.WriteHeader(code)
	}
}

func (a *Adaptor) guardFreeResponse(c *gin.Context, handle func() (any, *types.NewAPIError)) (any, *types.NewAPIError) {
	if a.freeResponseStream == nil {
		return handle()
	}
	stream := a.freeResponseStream
	defer stream.Close()
	original := c.Writer
	guard := &freeStreamWriter{ResponseWriter: original, stream: stream}
	c.Writer = guard
	usage, apiErr := handle()
	if err := stream.Err(); err != nil {
		apiErr = freeResponseError(err)
		if original.Written() {
			payload, _ := common.Marshal(gin.H{"type": "error", "error": gin.H{"type": "upstream_error", "message": err.Error()}})
			_, _ = original.Write(append(append([]byte("event: error\ndata: "), payload...), []byte("\n\n")...))
			original.Flush()
			// Keep the guard until request teardown so the controller cannot append JSON to this SSE error.
			return usage, apiErr
		}
		original.Header().Set("Content-Type", "application/json")
	}
	c.Writer = original
	return usage, apiErr
}
