package common

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type keywordWriterState struct {
	redirector *keywordRedirector
	pending    []byte
	stream     bool
	finished   bool
	jsonSent   bool
}

func keywordRedirectSettings(c *gin.Context) *dto.ChannelKeywordRedirectSettings {
	info := GetRelayInfo(c)
	if info == nil {
		return nil
	}
	switch info.RelayFormat {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini:
	default:
		return nil
	}
	if info.ChannelMeta != nil {
		return info.ChannelSetting.KeywordRedirect
	}
	settings, _ := basecommon.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	return settings.KeywordRedirect
}

func keywordRedirectActive(c *gin.Context) bool {
	return keywordRedirectSettings(c).Active()
}

func keywordResponseContentType(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	return contentType == "" || contentType == "application/json" ||
		strings.HasSuffix(contentType, "+json") || contentType == "text/event-stream"
}

func getRelayResponseWriter(c *gin.Context) *relayResponseWriter {
	if c == nil {
		return nil
	}
	if value, exists := c.Get(relayResponseWriterContextKey); exists {
		writer, _ := value.(*relayResponseWriter)
		return writer
	}
	writer, _ := c.Writer.(*relayResponseWriter)
	return writer
}

// Existing helper-level capture happens before writing. When keyword rewriting
// is active the writer takes ownership, so logs contain the actual emitted
// bytes and the stateful matcher is run exactly once.
func capturesKeywordResponse(c *gin.Context) bool {
	return getRelayResponseWriter(c) != nil && keywordRedirectActive(c)
}

func (w *relayResponseWriter) writeClientBytes(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	n, err := w.ResponseWriter.Write(data)
	if n > 0 && capturesKeywordResponse(w.context) {
		if capture := GetConversationCapture(w.context); capture != nil {
			capture.AppendClientResponseBody(data[:n])
		}
	}
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (w *relayResponseWriter) writeKeywordFrames(frames [][]byte, err error) error {
	if err != nil {
		return err
	}
	for _, frame := range frames {
		if _, err := w.writeClientBytes(frame); err != nil {
			return err
		}
	}
	return nil
}

func (w *relayResponseWriter) writeKeywordResponse(data []byte) error {
	if w.keyword == nil {
		info := GetRelayInfo(w.context)
		contentType := strings.ToLower(strings.TrimSpace(w.Header().Get("Content-Type")))
		w.keyword = &keywordWriterState{
			redirector: newKeywordRedirector(info.RelayFormat, keywordRedirectSettings(w.context).Rules),
			stream:     strings.HasPrefix(contentType, "text/event-stream") || (contentType == "" && info.IsStream),
		}
	}
	state := w.keyword
	if state.finished {
		// Finalization is idempotent, but helper error reporting may still write
		// afterwards. Such bytes retain the existing error/model sanitization.
		_, err := w.writeClientBytes(rewriteClientResponseBytes(w.context, data, &w.sseEventError))
		return err
	}
	if len(state.pending)+len(data)+state.redirector.queued > keywordRedirectBufferLimit {
		return fmt.Errorf("relay keyword redirect exceeds response buffer limit")
	}
	state.pending = append(state.pending, data...)
	if !state.stream {
		if state.jsonSent {
			_, err := w.writeClientBytes(state.pending)
			state.pending = nil
			return err
		}
		if !gjson.ValidBytes(state.pending) {
			return nil
		}
		body := rewriteClientResponseBytes(w.context, state.pending, nil)
		rewritten, err := state.redirector.rewriteJSON(body)
		if err != nil {
			return err
		}
		w.Header().Del("Content-Length")
		if _, err = w.writeClientBytes(rewritten); err != nil {
			return err
		}
		state.pending, state.jsonSent = nil, true
		return nil
	}
	for {
		end := keywordFrameEnd(state.pending)
		if end == 0 {
			break
		}
		raw := bytes.Clone(state.pending[:end])
		state.pending = state.pending[end:]
		raw = rewriteClientResponseBytes(w.context, raw, &w.sseEventError)
		if err := w.writeKeywordFrames(state.redirector.push(raw)); err != nil {
			return err
		}
	}
	state.pending = bytes.Clone(state.pending)
	return nil
}

// FinalizeRelayResponse releases a final unmatched prefix before logging or
// returning from the request, including upstream streams without a terminal
// marker. Signed blocks interrupted before their stop event retain their text.
func FinalizeRelayResponse(c *gin.Context) error {
	w := getRelayResponseWriter(c)
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.keywordErr != nil {
		return w.keywordErr
	}
	var flushErr error
	flushedSSEPending := false
	if len(w.ssePending) > 0 {
		pending := w.ssePending
		w.ssePending = nil
		rewritten := rewriteClientResponseBytes(c, pending, &w.sseEventError)
		if !bytes.Equal(pending, rewritten) {
			w.Header().Del("Content-Length")
		}
		if _, err := w.writeClientBytes(rewritten); err != nil {
			flushErr = err
		}
		flushedSSEPending = true
	}
	state := w.keyword
	if state == nil || state.finished {
		if flushedSSEPending {
			w.removeContentLengthForRewrite(w.Status())
			w.ResponseWriter.Flush()
		}
		return flushErr
	}
	state.finished = true
	if state.stream {
		if len(bytes.TrimSpace(state.pending)) != 0 {
			// A complete last event without a blank delimiter is still emitted
			// as a valid SSE event at EOF.
			raw := append(bytes.Clone(state.pending), '\n', '\n')
			raw = rewriteClientResponseBytes(c, raw, &w.sseEventError)
			w.keywordErr = w.writeKeywordFrames(state.redirector.push(raw))
		}
		if w.keywordErr == nil {
			w.keywordErr = w.writeKeywordFrames(state.redirector.finish())
		}
	} else if len(bytes.TrimSpace(state.pending)) != 0 {
		w.keywordErr = fmt.Errorf("incomplete JSON response during keyword redirect")
	}
	state.pending = nil
	w.removeContentLengthForRewrite(w.Status())
	w.ResponseWriter.Flush()
	if w.keywordErr != nil {
		return w.keywordErr
	}
	return flushErr
}

// InitChannelMeta calls this before each upstream attempt. Buffered data and
// compiled rules from a failed attempt must not follow a request to a new channel.
func resetKeywordResponse(c *gin.Context) {
	if w := getRelayResponseWriter(c); w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.keyword, w.keywordErr = nil, nil
		w.ssePending, w.sseEventError = nil, false
	}
}
