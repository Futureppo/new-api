package xfyun_maas

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Preserve SSE event names and multiline data before handing frames to the
// shared scanner (which consumes data-only lines). No buffering of the whole
// completion or background producer goroutine is required.
type eventReader struct {
	source     io.ReadCloser
	scanner    *bufio.Scanner
	pending    []byte
	event      string
	lines      []string
	frameBytes int
	eof        bool
}

func newEventReader(source io.ReadCloser) *eventReader {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	return &eventReader{source: source, scanner: scanner}
}

func (r *eventReader) Close() error { return r.source.Close() }

func (r *eventReader) frame() error {
	data := strings.Join(r.lines, "\n")
	event := r.event
	r.lines, r.event = nil, ""
	r.frameBytes = 0
	if data == "" {
		return nil
	}
	if data != "[DONE]" {
		var m map[string]json.RawMessage
		if err := common.UnmarshalJsonStr(data, &m); err != nil || m == nil {
			return fmt.Errorf("invalid upstream SSE JSON")
		}
		if len(m["type"]) == 0 && event != "" {
			m["type"], _ = common.Marshal(event)
		}
		// Native protocols finish with a typed terminal event. Stop the reader
		// there instead of racing another network read against scanner cleanup.
		switch rawString(m["type"]) {
		case "message_stop", "response.completed", "response.incomplete":
			r.eof = true
		}
		b, err := common.Marshal(m)
		if err != nil {
			return err
		}
		data = string(b)
	}
	r.pending = []byte("data: " + data + "\n\n")
	return nil
}

func (r *eventReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(r.pending) == 0 {
		if r.eof {
			return 0, io.EOF
		}
		if !r.scanner.Scan() {
			r.eof = true
			if err := r.scanner.Err(); err != nil {
				return 0, err
			}
			if err := r.frame(); err != nil {
				return 0, err
			}
			continue
		}
		line := strings.TrimSuffix(r.scanner.Text(), "\r")
		switch {
		case line == "":
			if err := r.frame(); err != nil {
				return 0, err
			}
		case strings.HasPrefix(line, "event:"):
			r.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			r.frameBytes += len(line)
			if r.frameBytes > 64<<20 {
				return 0, fmt.Errorf("upstream SSE event exceeds 64 MiB")
			}
			r.lines = append(r.lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func normalizeStreamEvent(data []byte, responses bool) (map[string]json.RawMessage, error) {
	data, err := normalizeEnvelope(data)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err = common.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if len(m["type"]) == 0 && len(m["event"]) > 0 {
		m["type"] = m["event"]
	}
	if responses && rawString(m["type"]) == "response.content_part.delta" {
		// This MaaS event is not an OpenAI event. Only translate a text delta
		// with enough addressing information; never invent item IDs/indices.
		for _, key := range []string{"item_id", "output_index", "content_index"} {
			if len(m[key]) == 0 || string(m[key]) == "null" {
				return nil, fmt.Errorf("MaaS content_part.delta lacks %s; cannot normalize it", key)
			}
		}
		var delta string
		if err := common.Unmarshal(m["delta"], &delta); err != nil {
			var part struct {
				Text *string `json:"text"`
				Type string  `json:"type"`
			}
			if err := common.Unmarshal(m["delta"], &part); err != nil || part.Text == nil ||
				(part.Type != "" && part.Type != "text" && part.Type != "output_text") {
				return nil, fmt.Errorf("unsupported MaaS content_part.delta payload")
			}
			delta = *part.Text
		}
		m["type"] = json.RawMessage(`"response.output_text.delta"`)
		m["delta"], _ = common.Marshal(delta)
		delete(m, "event")
	}
	return m, nil
}

func rawString(raw json.RawMessage) string {
	var s string
	_ = common.Unmarshal(raw, &s)
	return s
}

func writeEvent(c *gin.Context, event string, data []byte) error {
	if event != "" {
		header := []byte("event: " + event + "\n")
		relaycommon.AppendConversationClientResponse(c, header)
		if _, err := c.Writer.Write(header); err != nil {
			return err
		}
	}
	return helper.StringData(c, string(data))
}

func streamResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	isClaude := info.RelayFormat == types.RelayFormatClaude
	isResponses := info.RelayMode == relayconstant.RelayModeResponses
	if !isClaude && !isResponses && info.RelayMode != relayconstant.RelayModeChatCompletions {
		service.CloseResponseBodyGracefully(resp)
		return nil, badResponse(fmt.Errorf("unexpected streaming response for this endpoint"))
	}
	resp.Body = newEventReader(resp.Body)
	var terminal, sentUsage bool
	var streamErr *types.NewAPIError
	var text strings.Builder
	var responseID string
	var created json.RawMessage
	usageFields := make(map[string]json.RawMessage)
	collectUsage := func(raw json.RawMessage) {
		var fields map[string]json.RawMessage
		if len(raw) > 0 && common.Unmarshal(raw, &fields) == nil {
			for key, value := range fields {
				usageFields[key] = value
			}
		}
	}
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}
		if err := businessError([]byte(data), http.StatusOK); err != nil {
			streamErr = err
			sr.Stop(err)
			return
		}
		m, err := normalizeStreamEvent([]byte(data), isResponses)
		if err != nil {
			streamErr = badResponse(err)
			sr.Stop(err)
			return
		}
		typ := rawString(m["type"])
		responseID = firstNonempty(rawString(m["id"]), responseID)
		if len(m["created"]) > 0 {
			created = m["created"]
		}
		collectUsage(m["usage"])
		var nested map[string]json.RawMessage
		if isClaude {
			_ = common.Unmarshal(m["message"], &nested)
		} else if isResponses {
			_ = common.Unmarshal(m["response"], &nested)
		}
		collectUsage(nested["usage"])
		if apiErr := businessError(m["response"], http.StatusOK); apiErr != nil {
			streamErr = apiErr
			sr.Stop(apiErr)
			return
		}
		switch typ {
		case "error", "response.error", "response.failed":
			streamErr = badResponse(fmt.Errorf("upstream %s", typ))
			sr.Stop(streamErr)
			return
		}
		if isClaude {
			var delta struct {
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
			}
			_ = common.Unmarshal(m["delta"], &delta)
			text.WriteString(delta.Text + delta.Thinking + delta.PartialJSON)
			if typ == "message_stop" {
				terminal = true
			}
		} else if isResponses {
			if strings.HasSuffix(typ, ".delta") {
				text.WriteString(rawString(m["delta"]))
			}
			if typ == "response.completed" || typ == "response.incomplete" {
				terminal = true
			}
		} else {
			b, _ := common.Marshal(m)
			var body struct {
				Choices []struct {
					FinishReason *string `json:"finish_reason"`
					Delta        struct {
						Content   string `json:"content"`
						Reasoning string `json:"reasoning_content"`
						ToolCalls []struct {
							Function struct {
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := common.Unmarshal(b, &body); err != nil {
				streamErr = badResponse(err)
				sr.Stop(err)
				return
			}
			for _, choice := range body.Choices {
				text.WriteString(choice.Delta.Content + choice.Delta.Reasoning)
				for _, tool := range choice.Delta.ToolCalls {
					text.WriteString(tool.Function.Arguments)
				}
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					terminal = true
				}
			}
			if !info.ShouldIncludeUsage {
				delete(m, "usage")
				if len(body.Choices) == 0 {
					return
				}
			} else if raw := m["usage"]; len(raw) > 0 && string(raw) != "null" {
				sentUsage = true
			}
		}
		b, err := common.Marshal(m)
		if err != nil {
			streamErr = badResponse(err)
			sr.Stop(err)
			return
		}
		event := ""
		if isClaude || isResponses {
			event = typ
			if event == "" {
				streamErr = badResponse(fmt.Errorf("missing upstream SSE event type"))
				sr.Stop(streamErr)
				return
			}
		}
		var writeErr error
		if !isClaude && !isResponses {
			writeErr = openai.HandleStreamFormat(c, info, string(b), info.ChannelSetting.ForceFormat, info.ChannelSetting.ThinkingToContent)
		} else {
			writeErr = writeEvent(c, event, b)
		}
		if err := writeErr; err != nil {
			streamErr = badResponse(err)
			sr.Stop(err)
			return
		}
		if terminal && (isClaude || isResponses) {
			sr.Done()
		}
	})
	if streamErr == nil && (!terminal || info.StreamStatus.HasErrors() || !info.StreamStatus.IsNormalEnd()) {
		streamErr = badResponse(fmt.Errorf("Xunfei MaaS stream ended without successful completion: %s", info.StreamStatus.Summary()))
		info.StreamStatus.RecordError(streamErr.Error())
	}
	usage := usageFromFields(usageFields, isClaude, isResponses)
	if len(usageFields) == 0 {
		usage = service.ResponseText2Usage(c, text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if streamErr != nil {
		if !c.Writer.Written() {
			c.Header("Content-Type", "application/json")
			return nil, streamErr
		}
		// Content may already have consumed tokens. Record and emit failure,
		// but do not return a retryable error after a partial response.
		event := ""
		if isClaude || isResponses {
			event = "error"
		}
		errBody := map[string]any{"error": streamErr.ToOpenAIError()}
		if event != "" {
			errBody["type"] = "error"
		}
		b, _ := common.Marshal(errBody)
		_ = writeEvent(c, event, b)
		return usage, nil
	}
	if !isClaude && !isResponses {
		if info.ShouldIncludeUsage && !sentUsage {
			if len(created) == 0 {
				created, _ = common.Marshal(common.GetTimestamp())
			}
			_ = helper.ObjectData(c, map[string]any{
				"id": responseID, "object": "chat.completion.chunk", "created": created,
				"model": info.UpstreamModelName, "choices": []any{}, "usage": usage,
			})
		}
		helper.Done(c)
	}
	return usage, nil
}

func firstNonempty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func usageFromFields(fields map[string]json.RawMessage, claude, responses bool) *dto.Usage {
	b, _ := common.Marshal(fields)
	usage := &dto.Usage{}
	_ = common.Unmarshal(b, usage)
	if claude || responses {
		usage.PromptTokens, usage.CompletionTokens = usage.InputTokens, usage.OutputTokens
		if usage.InputTokensDetails != nil {
			usage.PromptTokensDetails = *usage.InputTokensDetails
		}
		if usage.OutputTokensDetails != nil {
			usage.CompletionTokenDetails = *usage.OutputTokensDetails
		}
	}
	if claude {
		usage.UsageSemantic = "anthropic"
		var native dto.ClaudeUsage
		_ = common.Unmarshal(b, &native)
		usage.PromptTokensDetails.CachedTokens = max(0, native.CacheReadInputTokens)
		usage.PromptTokensDetails.CachedCreationTokens = max(0, native.CacheCreationInputTokens)
		usage.ClaudeCacheCreation5mTokens = native.GetCacheCreation5mTokens()
		usage.ClaudeCacheCreation1hTokens = native.GetCacheCreation1hTokens()
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}
