package bilibili_web

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type textResult struct {
	text   strings.Builder
	finish string
	usage  *dto.Usage
}

type webFrame struct {
	Error   json.RawMessage `json:"error"`
	Usage   *dto.Usage      `json:"usage"`
	Choices []struct {
		Index        int         `json:"index"`
		Delta        dto.Message `json:"delta"`
		Message      dto.Message `json:"message"`
		FinishReason *string     `json:"finish_reason"`
	} `json:"choices"`
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, upstreamError("empty Bilibili Web response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, upstreamError(fmt.Sprintf("Bilibili Web returned HTTP %d", resp.StatusCode))
	}
	stop := context.AfterFunc(c.Request.Context(), func() { _ = resp.Body.Close() })
	defer stop()
	info.IsStream = a.clientStream
	if a.clientStream && info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	if a.echoCompleted {
		body, err := readLimited(resp.Body, 16<<20)
		if err != nil {
			return nil, upstreamError(err.Error())
		}
		info.NonTokenUsage = true
		info.SetFirstResponseTime()
		c.Data(http.StatusOK, "application/json", body)
		return &dto.Usage{UsageSource: "bilibili_web_per_request"}, nil
	}

	result := &textResult{}
	writer := &outputWriter{a: a, c: c, created: time.Now().Unix()}
	consume := func(data string) error {
		var frame webFrame
		if err := common.UnmarshalJsonStr(data, &frame); err != nil {
			return fmt.Errorf("invalid upstream JSON: %w", err)
		}
		if hasValue(frame.Error) {
			return fmt.Errorf("Bilibili Web upstream error: %s", frame.Error)
		}
		if frame.Usage != nil {
			result.usage = frame.Usage
		}
		for _, choice := range frame.Choices {
			if choice.Index != 0 {
				return fmt.Errorf("unexpected upstream choice index")
			}
			message := choice.Delta
			if !a.upstreamSSE {
				message = choice.Message
			}
			if hasItems(message.ToolCalls) || message.Refusal != nil || hasValue(message.Audio) || message.GetReasoningContent() != "" || message.Reasoning != "" {
				return fmt.Errorf("upstream returned an unsupported non-text result")
			}
			if message.Content != nil {
				text, ok := message.Content.(string)
				if !ok {
					return fmt.Errorf("upstream returned non-text content")
				}
				if result.text.Len()+len(text) > 8<<20 {
					return fmt.Errorf("translated text exceeds response limit")
				}
				result.text.WriteString(text)
				if a.clientStream && text != "" {
					info.SetFirstResponseTime()
					if err := writer.delta(text); err != nil {
						return err
					}
				}
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				result.finish = *choice.FinishReason
			}
		}
		return nil
	}

	var err error
	if a.upstreamSSE {
		err = scanSSE(resp.Body, consume)
	} else {
		var body []byte
		body, err = readLimited(resp.Body, 8<<20)
		if err == nil {
			err = consume(string(body))
		}
	}
	if err == nil && result.finish != "stop" && result.finish != "length" {
		err = fmt.Errorf("upstream response has no supported finish reason")
	}
	if err == nil && result.text.Len() == 0 && result.finish != "length" {
		err = fmt.Errorf("upstream returned no translation")
	}
	if err != nil {
		apiErr := upstreamError(err.Error())
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
		if writer.started {
			info.StreamStatus.RecordError(err.Error())
			_ = writer.failure(apiErr)
			c.Set("bilibili_web_response_committed", true)
		}
		return nil, apiErr
	}
	if result.usage == nil {
		result.usage = service.ResponseText2Usage(c, result.text.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		result.usage.UsageSource = "bilibili_web_estimated"
	}
	info.SetFirstResponseTime()
	if a.clientStream {
		if err = writer.complete(result, info.ShouldIncludeUsage); err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			c.Set("bilibili_web_response_committed", true)
			return nil, upstreamError(err.Error())
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	} else {
		var response any
		if a.responses {
			response = writer.response(result)
		} else {
			response = writer.chat(result)
		}
		data, marshalErr := common.Marshal(response)
		if marshalErr != nil {
			return nil, upstreamError(marshalErr.Error())
		}
		c.Data(http.StatusOK, "application/json", data)
	}
	return result.usage, nil
}

// Require the actual terminal marker, including when the final SSE frame has
// no trailing blank line. An HTTP 200 or EOF alone is never a successful stream.
func scanSSE(body io.Reader, consume func(string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var parts []string
	size := 0
	dispatch := func() (bool, error) {
		data := strings.Join(parts, "\n")
		parts, size = nil, 0
		if data == "" {
			return false, nil
		}
		if data == "[DONE]" {
			return true, nil
		}
		return false, consume(data)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			done, err := dispatch()
			if err != nil || done {
				return err
			}
		} else if strings.HasPrefix(line, "data:") {
			size += len(line)
			if size > 1<<20 {
				return fmt.Errorf("upstream SSE frame exceeds limit")
			}
			parts = append(parts, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	done, err := dispatch()
	if err != nil {
		return err
	}
	if !done {
		return fmt.Errorf("upstream stream ended without [DONE]")
	}
	return nil
}

type outputWriter struct {
	a        *Adaptor
	c        *gin.Context
	created  int64
	sequence int
	started  bool
}

func chatUsage(usage *dto.Usage) map[string]any {
	return map[string]any{
		"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens,
		"total_tokens": usage.TotalTokens, "prompt_tokens_details": usage.PromptTokensDetails,
		"completion_tokens_details": usage.CompletionTokenDetails,
	}
}

func (w *outputWriter) chat(r *textResult) map[string]any {
	return map[string]any{
		"id": "chatcmpl-" + w.a.responseID, "object": "chat.completion", "created": w.created, "model": w.a.responseModel,
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": r.text.String()}, "finish_reason": r.finish}},
		"usage":   chatUsage(r.usage),
	}
}

func (w *outputWriter) item(text, status string) map[string]any {
	content := []any{}
	if status != "in_progress" || text != "" {
		content = append(content, map[string]any{"type": "output_text", "text": text, "annotations": []any{}})
	}
	return map[string]any{"id": "msg_" + w.a.responseID, "type": "message", "role": "assistant", "status": status, "content": content}
}

func (w *outputWriter) response(r *textResult) map[string]any {
	status := "in_progress"
	output := []any{}
	var usage, incomplete any
	if r != nil {
		status = "completed"
		if r.finish == "length" {
			status = "incomplete"
			incomplete = map[string]any{"reason": "max_output_tokens"}
		}
		output = append(output, w.item(r.text.String(), status))
		usage = map[string]any{
			"input_tokens": r.usage.PromptTokens, "output_tokens": r.usage.CompletionTokens, "total_tokens": r.usage.TotalTokens,
			"input_tokens_details":  map[string]int{"cached_tokens": r.usage.PromptTokensDetails.CachedTokens},
			"output_tokens_details": map[string]int{"reasoning_tokens": r.usage.CompletionTokenDetails.ReasoningTokens},
		}
	}
	return map[string]any{
		"id": w.a.responseID, "object": "response", "created_at": w.created, "status": status, "model": w.a.responseModel,
		"output": output, "error": nil, "incomplete_details": incomplete, "usage": usage,
		"store": false, "background": false, "tools": []any{}, "tool_choice": "none",
		"parallel_tool_calls": false, "metadata": w.a.responseMeta,
	}
}

func (w *outputWriter) emit(event string, value map[string]any) error {
	if !w.started {
		helper.SetEventStreamHeaders(w.c)
		w.started = true
	}
	if event != "" {
		value["type"] = event
		value["sequence_number"] = w.sequence
		w.sequence++
	}
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	prefix := ""
	if event != "" {
		prefix = "event: " + event + "\n"
	}
	_, err = fmt.Fprintf(w.c.Writer, "%sdata: %s\n\n", prefix, data)
	w.c.Writer.Flush()
	return err
}

func (w *outputWriter) chunk(delta map[string]any, finish any) map[string]any {
	return map[string]any{
		"id": "chatcmpl-" + w.a.responseID, "object": "chat.completion.chunk", "created": w.created, "model": w.a.responseModel,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
	}
}

func (w *outputWriter) start() error {
	if w.started {
		return nil
	}
	if !w.a.responses {
		return w.emit("", w.chunk(map[string]any{"role": "assistant", "content": ""}, nil))
	}
	for _, event := range []string{"response.created", "response.in_progress"} {
		if err := w.emit(event, map[string]any{"response": w.response(nil)}); err != nil {
			return err
		}
	}
	if err := w.emit("response.output_item.added", map[string]any{"output_index": 0, "item": w.item("", "in_progress")}); err != nil {
		return err
	}
	return w.emit("response.content_part.added", map[string]any{"item_id": "msg_" + w.a.responseID, "output_index": 0, "content_index": 0,
		"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
}

func (w *outputWriter) delta(text string) error {
	if err := w.start(); err != nil {
		return err
	}
	if w.a.responses {
		return w.emit("response.output_text.delta", map[string]any{"item_id": "msg_" + w.a.responseID, "output_index": 0, "content_index": 0, "delta": text})
	}
	return w.emit("", w.chunk(map[string]any{"content": text}, nil))
}

func (w *outputWriter) complete(r *textResult, includeUsage bool) error {
	if err := w.start(); err != nil {
		return err
	}
	if !w.a.responses {
		if err := w.emit("", w.chunk(map[string]any{}, r.finish)); err != nil {
			return err
		}
		if includeUsage {
			event := w.chunk(nil, nil)
			event["choices"], event["usage"] = []any{}, chatUsage(r.usage)
			if err := w.emit("", event); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w.c.Writer, "data: [DONE]\n\n")
		w.c.Writer.Flush()
		return err
	}
	base := func() map[string]any {
		return map[string]any{"item_id": "msg_" + w.a.responseID, "output_index": 0, "content_index": 0}
	}
	event := base()
	event["text"] = r.text.String()
	if err := w.emit("response.output_text.done", event); err != nil {
		return err
	}
	event = base()
	event["part"] = map[string]any{"type": "output_text", "text": r.text.String(), "annotations": []any{}}
	if err := w.emit("response.content_part.done", event); err != nil {
		return err
	}
	response := w.response(r)
	if err := w.emit("response.output_item.done", map[string]any{"output_index": 0, "item": w.item(r.text.String(), response["status"].(string))}); err != nil {
		return err
	}
	return w.emit("response."+response["status"].(string), map[string]any{"response": response})
}

func (w *outputWriter) failure(err *types.NewAPIError) error {
	if w.a.responses {
		response := w.response(nil)
		response["status"], response["error"] = "failed", err.ToOpenAIError()
		return w.emit("response.failed", map[string]any{"response": response})
	}
	return w.emit("", map[string]any{"error": err.ToOpenAIError()})
}
