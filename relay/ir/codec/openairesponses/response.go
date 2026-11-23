package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/relay/ir"
)

type responsesIncompleteDetailsWire struct {
	Reason    string `json:"reason,omitempty"`
	Reasoning string `json:"reasoning,omitempty"` // defensive alias
}

func (d *responsesIncompleteDetailsWire) EffectiveReason() string {
	if d == nil {
		return ""
	}
	if d.Reason != "" {
		return d.Reason
	}
	return d.Reasoning
}

type responsesInputTokenDetailsWire struct {
	CachedTokens             int  `json:"cached_tokens"`
	CacheWriteTokens         *int `json:"cache_write_tokens,omitempty"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	CachedCreationTokens     int  `json:"cached_creation_tokens,omitempty"`
	TextTokens               int  `json:"text_tokens,omitempty"`
	AudioTokens              int  `json:"audio_tokens,omitempty"`
	ImageTokens              int  `json:"image_tokens,omitempty"`
}

func (d *responsesInputTokenDetailsWire) EffectiveCacheWriteTokens() int {
	if d == nil {
		return 0
	}
	if d.CacheWriteTokens != nil {
		return *d.CacheWriteTokens
	}
	if d.CacheCreationInputTokens != nil {
		return *d.CacheCreationInputTokens
	}
	return d.CachedCreationTokens
}

type responsesOutputTokenDetailsWire struct {
	ReasoningTokens int `json:"reasoning_tokens"`
	TextTokens      int `json:"text_tokens,omitempty"`
	AudioTokens     int `json:"audio_tokens,omitempty"`
	ImageTokens     int `json:"image_tokens,omitempty"`
}

type responsesUsageWire struct {
	InputTokens         int                              `json:"input_tokens"`
	OutputTokens        int                              `json:"output_tokens"`
	TotalTokens         int                              `json:"total_tokens"`
	InputTokensDetails  *responsesInputTokenDetailsWire  `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *responsesOutputTokenDetailsWire `json:"output_tokens_details,omitempty"`
}

type responsesResponseWire struct {
	ID                string                          `json:"id"`
	Object            string                          `json:"object,omitempty"`
	CreatedAt         int64                           `json:"created_at,omitempty"`
	Status            string                          `json:"status,omitempty"`
	IncompleteDetails *responsesIncompleteDetailsWire `json:"incomplete_details,omitempty"`
	Model             string                          `json:"model,omitempty"`
	ServiceTier       string                          `json:"service_tier,omitempty"`
	Output            []json.RawMessage               `json:"output,omitempty"`
	Usage             *responsesUsageWire             `json:"usage,omitempty"`
	Error             json.RawMessage                 `json:"error,omitempty"`
}

type responsesOutputItemWire struct {
	Type             string                     `json:"type"`
	ID               string                     `json:"id,omitempty"`
	Status           string                     `json:"status,omitempty"`
	Role             string                     `json:"role,omitempty"`
	Content          json.RawMessage            `json:"content,omitempty"`
	Summary          []responsesReasoningPart   `json:"summary,omitempty"`
	EncryptedContent string                     `json:"encrypted_content,omitempty"`
	CallID           string                     `json:"call_id,omitempty"`
	Name             string                     `json:"name,omitempty"`
	Arguments        string                     `json:"arguments,omitempty"`
	Input            string                     `json:"input,omitempty"`
	Quality          string                     `json:"quality,omitempty"`
	Size             string                     `json:"size,omitempty"`
}

// DecodeResponse parses a non-streaming OpenAI Responses API JSON body into the provider-neutral IR.
func (c *Codec) DecodeResponse(raw []byte, ctx *ir.ConversionContext) (*ir.Response, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("openairesponses: empty response body")
	}
	var wire responsesResponseWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("openairesponses: decode response: %w", err)
	}

	var decodedErr *ir.ResponseError
	if !isNullOrEmptyJSON(wire.Error) {
		decodedErr = decodeResponseError(wire.Error)
		// Pure gateway error envelope (no response id/status/output): fail fast rather than returning FinishStop.
		if wire.ID == "" && wire.Status == "" && len(wire.Output) == 0 {
			errType := "error"
			errMsg := "upstream response error"
			if decodedErr != nil {
				if decodedErr.Type != "" {
					errType = decodedErr.Type
				} else if decodedErr.Code != "" {
					errType = decodedErr.Code
				}
				if decodedErr.Message != "" {
					errMsg = decodedErr.Message
				}
			}
			return nil, fmt.Errorf("openairesponses: upstream error (%s): %s", errType, errMsg)
		}
	}

	resp := &ir.Response{
		ID:          wire.ID,
		Model:       wire.Model,
		Created:     wire.CreatedAt,
		ServiceTier: wire.ServiceTier,
		Error:       decodedErr,
	}

	var parts []ir.ContentPart
	hasToolCalls := false
	webSearchRequests := 0
	fileSearchCalls := 0
	hasImageGenCall := false
	imageGenQuality := ""
	imageGenSize := ""

	for idx, rawItem := range wire.Output {
		trimmed := bytes.TrimSpace(rawItem)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		var item responsesOutputItemWire
		if err := json.Unmarshal(trimmed, &item); err != nil {
			return nil, fmt.Errorf("openairesponses: decode response.output[%d]: %w", idx, err)
		}

		switch item.Type {
		case "message":
			msgParts, err := decodeMessageContent(item.Content)
			if err != nil {
				return nil, fmt.Errorf("openairesponses: decode response.output[%d].content: %w", idx, err)
			}
			parts = append(parts, msgParts...)

		case "reasoning":
			reasoning := decodeReasoningItem(item.ID, item.Summary, item.Content, item.EncryptedContent)
			parts = append(parts, ir.ContentPart{
				Type:      ir.PartReasoning,
				Reasoning: reasoning,
			})

		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			tc := &ir.ToolCall{
				ID:        callID,
				ItemID:    item.ID,
				Name:      item.Name,
				Arguments: item.Arguments,
				Kind:      "function",
			}
			parts = append(parts, ir.ContentPart{
				Type:     ir.PartToolCall,
				ToolCall: tc,
			})
			hasToolCalls = true
			recordToolBridge(ctx, tc)

		case "custom_tool_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			args := item.Input
			if args == "" {
				args = item.Arguments
			}
			tc := &ir.ToolCall{
				ID:        callID,
				ItemID:    item.ID,
				Name:      item.Name,
				Arguments: args,
				Kind:      "custom",
			}
			parts = append(parts, ir.ContentPart{
				Type:     ir.PartToolCall,
				ToolCall: tc,
			})
			hasToolCalls = true
			recordToolBridge(ctx, tc)

		case "web_search_call":
			webSearchRequests++
			resp.PassthroughItems = append(resp.PassthroughItems, ir.PassthroughItem{
				OriginFormat: ir.FormatOpenAIResponses,
				Position:     idx,
				Kind:         item.Type,
				Payload:      cloneRaw(trimmed),
			})

		case "file_search_call":
			fileSearchCalls++
			resp.PassthroughItems = append(resp.PassthroughItems, ir.PassthroughItem{
				OriginFormat: ir.FormatOpenAIResponses,
				Position:     idx,
				Kind:         item.Type,
				Payload:      cloneRaw(trimmed),
			})

		case "image_generation_call":
			hasImageGenCall = true
			if q, s := extractImageGenParams(trimmed); q != "" || s != "" {
				if q != "" {
					imageGenQuality = q
				}
				if s != "" {
					imageGenSize = s
				}
			}
			resp.PassthroughItems = append(resp.PassthroughItems, ir.PassthroughItem{
				OriginFormat: ir.FormatOpenAIResponses,
				Position:     idx,
				Kind:         item.Type,
				Payload:      cloneRaw(trimmed),
			})

		default:
			resp.PassthroughItems = append(resp.PassthroughItems, ir.PassthroughItem{
				OriginFormat: ir.FormatOpenAIResponses,
				Position:     idx,
				Kind:         item.Type,
				Payload:      cloneRaw(trimmed),
			})
		}
	}

	finishReason, rawFinish := deriveFinishReason(wire.Status, wire.IncompleteDetails, hasToolCalls)
	resp.Choices = []ir.Choice{
		{
			Index: 0,
			Message: ir.Message{
				Role:  ir.RoleAssistant,
				Parts: parts,
			},
			FinishReason:    finishReason,
			RawFinishReason: rawFinish,
		},
	}

	usage := decodeResponsesUsage(wire.Usage)
	if usage == nil && (webSearchRequests > 0 || fileSearchCalls > 0 || hasImageGenCall) {
		usage = &ir.Usage{InputExclusive: false}
	}
	if usage != nil {
		usage.WebSearchRequests = webSearchRequests
		usage.FileSearchCalls = fileSearchCalls
		usage.HasImageGenCall = hasImageGenCall
		if imageGenQuality != "" {
			usage.ImageGenQuality = imageGenQuality
		}
		if imageGenSize != "" {
			usage.ImageGenSize = imageGenSize
		}
		resp.Usage = usage
	}

	return resp, nil
}

func decodeResponseError(raw json.RawMessage) *ir.ResponseError {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var obj struct {
		Type    string `json:"type"`
		Code    any    `json:"code"`
		Message string `json:"message"`
		Param   string `json:"param"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return &ir.ResponseError{Message: string(trimmed)}
	}
	re := &ir.ResponseError{
		Type:    obj.Type,
		Message: obj.Message,
		Param:   obj.Param,
	}
	if obj.Code != nil {
		re.Code = fmt.Sprintf("%v", obj.Code)
	}
	return re
}

func extractImageGenParams(raw json.RawMessage) (quality, size string) {
	var obj struct {
		Quality string `json:"quality"`
		Size    string `json:"size"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Quality, obj.Size
	}
	return "", ""
}

func recordToolBridge(ctx *ir.ConversionContext, tc *ir.ToolCall) {
	if ctx == nil || ctx.BridgeState == nil || tc == nil || tc.ID == "" {
		return
	}
	ctx.BridgeState.RecordToolCall(ir.ToolCallBridgeEntry{
		ToolCallID:      tc.ID,
		ResponsesItemID: tc.ItemID,
		ToolName:        tc.Name,
		OriginFormat:    ir.FormatOpenAIResponses,
	})
}

func deriveFinishReason(status string, incomplete *responsesIncompleteDetailsWire, hasToolCalls bool) (ir.FinishReason, string) {
	switch status {
	case "failed":
		return ir.FinishError, "failed"
	case "cancelled":
		return ir.FinishError, "cancelled"
	case "incomplete":
		reason := incomplete.EffectiveReason()
		switch reason {
		case "content_filter":
			return ir.FinishContentFilter, "content_filter"
		case "max_output_tokens":
			return ir.FinishLength, "max_output_tokens"
		default:
			if reason == "" {
				reason = "incomplete"
			}
			return ir.FinishLength, reason
		}
	default:
		if hasToolCalls {
			return ir.FinishToolCalls, "completed"
		}
		if status == "" {
			status = "completed"
		}
		return ir.FinishStop, status
	}
}

func decodeResponsesUsage(w *responsesUsageWire) *ir.Usage {
	if w == nil {
		return nil
	}
	u := &ir.Usage{
		InputTokens:    w.InputTokens,
		OutputTokens:   w.OutputTokens,
		TotalTokens:    w.TotalTokens,
		InputExclusive: false,
	}
	if w.InputTokensDetails != nil {
		u.CachedReadTokens = w.InputTokensDetails.CachedTokens
		u.CacheCreationTokens = w.InputTokensDetails.EffectiveCacheWriteTokens()
		u.TextInputTokens = w.InputTokensDetails.TextTokens
		u.AudioInputTokens = w.InputTokensDetails.AudioTokens
		u.ImageInputTokens = w.InputTokensDetails.ImageTokens
	}
	if w.OutputTokensDetails != nil {
		u.ReasoningTokens = w.OutputTokensDetails.ReasoningTokens
		u.TextOutputTokens = w.OutputTokensDetails.TextTokens
		u.AudioOutputTokens = w.OutputTokensDetails.AudioTokens
		u.ImageOutputTokens = w.OutputTokensDetails.ImageTokens
	}
	if u.TotalTokens == 0 && (u.InputTokens > 0 || u.OutputTokens > 0) {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	return u
}

func encodeResponsesUsage(u *ir.Usage) *responsesUsageWire {
	if u == nil {
		return nil
	}
	inputTokens := u.InclusiveInputTokens()
	outputTokens := u.OutputTokens
	totalTokens := u.TotalTokens
	if totalTokens < inputTokens+outputTokens || u.InputExclusive {
		totalTokens = inputTokens + outputTokens
	}

	inDetails := &responsesInputTokenDetailsWire{
		CachedTokens: u.CachedReadTokens,
		TextTokens:   u.TextInputTokens,
		AudioTokens:  u.AudioInputTokens,
		ImageTokens:  u.ImageInputTokens,
	}
	if u.CacheCreationTokens > 0 {
		v := u.CacheCreationTokens
		inDetails.CacheWriteTokens = &v
	}

	outDetails := &responsesOutputTokenDetailsWire{
		ReasoningTokens: u.ReasoningTokens,
		TextTokens:      u.TextOutputTokens,
		AudioTokens:     u.AudioOutputTokens,
		ImageTokens:     u.ImageOutputTokens,
	}

	return &responsesUsageWire{
		InputTokens:         inputTokens,
		OutputTokens:        outputTokens,
		TotalTokens:         totalTokens,
		InputTokensDetails:  inDetails,
		OutputTokensDetails: outDetails,
	}
}

// EncodeResponse serializes a provider-neutral IR Response into a canonical OpenAI Responses JSON object.
func (c *Codec) EncodeResponse(resp *ir.Response, ctx *ir.ConversionContext) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("openairesponses: nil IR response")
	}

	respID := ensureResponseID(resp.ID)
	suffix := shortIDSuffix(respID)
	created := resp.Created
	if created == 0 {
		created = time.Now().Unix()
	}

	status := "completed"
	var incompleteDetails *responsesIncompleteDetailsWire
	itemStatus := "completed"

	var choice *ir.Choice
	if len(resp.Choices) > 0 {
		choice = &resp.Choices[0]
		switch choice.FinishReason {
		case ir.FinishLength:
			status = "incomplete"
			itemStatus = "incomplete"
			incompleteDetails = &responsesIncompleteDetailsWire{Reason: "max_output_tokens"}
		case ir.FinishContentFilter:
			status = "incomplete"
			itemStatus = "incomplete"
			incompleteDetails = &responsesIncompleteDetailsWire{Reason: "content_filter"}
		case ir.FinishError:
			status = "failed"
		}
	}

	output := make([]any, 0)
	if choice != nil {
		msgIdx := 0
		rsIdx := 0
		fcIdx := 0

		var pendingContent []map[string]any
		flushMessage := func() {
			if len(pendingContent) == 0 {
				return
			}
			output = append(output, map[string]any{
				"id":      fmt.Sprintf("msg_%s_%d", suffix, msgIdx),
				"type":    "message",
				"status":  itemStatus,
				"role":    "assistant",
				"content": pendingContent,
			})
			msgIdx++
			pendingContent = nil
		}

		for _, part := range choice.Message.Parts {
			switch part.Type {
			case ir.PartText:
				pendingContent = append(pendingContent, map[string]any{
					"type":        "output_text",
					"text":        part.Text,
					"annotations": []any{},
				})

			case ir.PartImage:
				if part.Image != nil {
					if md := formatResponsesImageMarkdown(part.Image); md != "" {
						pendingContent = append(pendingContent, map[string]any{
							"type":        "output_text",
							"text":        md,
							"annotations": []any{},
						})
					}
				}

			case ir.PartAudio, ir.PartFile:
				ctx.AddWarning("unsupported_response_part", "output", fmt.Sprintf("OpenAI Responses assistant output cannot carry %q part natively", part.Type))

			case ir.PartCitation:
				if part.Citation == nil {
					continue
				}
				ann := map[string]any{
					"type":        "url_citation",
					"url":         part.Citation.URL,
					"title":       part.Citation.Title,
					"start_index": part.Citation.StartIndex,
					"end_index":   part.Citation.EndIndex,
				}
				if part.Citation.CitedText != "" {
					ann["text"] = part.Citation.CitedText
				}
				// Attach citation to the most recent output_text part if present.
				for i := len(pendingContent) - 1; i >= 0; i-- {
					if pendingContent[i]["type"] == "output_text" {
						anns, _ := pendingContent[i]["annotations"].([]any)
						pendingContent[i]["annotations"] = append(anns, ann)
						break
					}
				}

			case ir.PartRefusal:
				pendingContent = append(pendingContent, map[string]any{
					"type":    "refusal",
					"refusal": part.Refusal,
				})

			case ir.PartReasoning:
				if part.Reasoning == nil {
					continue
				}
				flushMessage()
				output = append(output, encodeResponseReasoningItem(part.Reasoning, suffix, rsIdx, itemStatus, ctx))
				rsIdx++

			case ir.PartToolCall:
				if part.ToolCall == nil {
					continue
				}
				flushMessage()
				output = append(output, encodeResponseToolCallItem(part.ToolCall, fcIdx, itemStatus))
				fcIdx++
			}
		}
		flushMessage()
	}

	var nativePassthrough []ir.PassthroughItem
	for _, pt := range resp.PassthroughItems {
		if len(pt.Payload) == 0 {
			continue
		}
		if shouldPreserveResponsesNative(pt.OriginFormat, ctx) {
			nativePassthrough = append(nativePassthrough, pt)
		}
	}
	if len(nativePassthrough) > 0 {
		sort.SliceStable(nativePassthrough, func(i, j int) bool {
			return nativePassthrough[i].Position < nativePassthrough[j].Position
		})
		for _, pt := range nativePassthrough {
			pos := pt.Position
			if pos < 0 || pos >= len(output) {
				output = append(output, pt.Payload)
				continue
			}
			output = append(output, nil)
			copy(output[pos+1:], output[pos:])
			output[pos] = pt.Payload
		}
	}

	out := map[string]any{
		"id":         respID,
		"object":     "response",
		"created_at": created,
		"status":     status,
		"model":      resp.Model,
		"output":     output,
	}
	if incompleteDetails != nil {
		out["incomplete_details"] = incompleteDetails
	}
	if resp.Error != nil {
		errObj := map[string]any{
			"code":    resp.Error.Code,
			"message": resp.Error.Message,
		}
		if resp.Error.Type != "" {
			errObj["type"] = resp.Error.Type
		}
		out["error"] = errObj
	} else if status == "failed" {
		out["error"] = map[string]any{
			"code":    "server_error",
			"message": "upstream generation failed",
		}
	}
	if resp.ServiceTier != "" {
		out["service_tier"] = resp.ServiceTier
	}
	if u := encodeResponsesUsage(resp.Usage); u != nil {
		out["usage"] = u
	}

	return json.Marshal(out)
}

func formatResponsesImageMarkdown(img *ir.ImageSource) string {
	if img == nil {
		return ""
	}
	if img.URL != "" {
		return fmt.Sprintf("![image](%s)", img.URL)
	}
	if img.Data != "" {
		return fmt.Sprintf("![image](%s)", buildDataURL(img.MediaType, img.Data))
	}
	return ""
}

func encodeResponseReasoningItem(r *ir.Reasoning, suffix string, idx int, itemStatus string, ctx *ir.ConversionContext) map[string]any {
	if itemStatus == "" {
		itemStatus = "completed"
	}
	rsID := r.ItemID
	if rsID == "" {
		rsID = fmt.Sprintf("rs_%s_%d", suffix, idx)
	}
	summaryText := r.Summary
	if summaryText == "" {
		summaryText = r.Text
	}
	summary := make([]map[string]any, 0, 1)
	if summaryText != "" {
		summary = append(summary, map[string]any{
			"type": "summary_text",
			"text": summaryText,
		})
	}
	item := map[string]any{
		"id":      rsID,
		"type":    "reasoning",
		"status":  itemStatus,
		"summary": summary,
	}
	if r.Summary != "" && r.Text != "" && r.Text != r.Summary {
		item["content"] = []map[string]any{{
			"type": "reasoning_text",
			"text": r.Text,
		}}
	}
	if r.OriginFormat == ir.FormatOpenAIResponses && r.RedactedData != "" && shouldPreserveResponsesNative(r.OriginFormat, ctx) {
		item["encrypted_content"] = r.RedactedData
	} else if r.OriginFormat != ir.FormatOpenAIResponses && (r.RedactedData != "" || r.Signature != "") {
		ctx.AddWarning("dropped_foreign_reasoning_opaque", "output.reasoning", fmt.Sprintf("dropped foreign reasoning opaque data from origin %q", r.OriginFormat))
	}
	return item
}

func encodeResponseToolCallItem(tc *ir.ToolCall, idx int, itemStatus string) map[string]any {
	if itemStatus == "" {
		itemStatus = "completed"
	}
	item := encodeRequestToolCallItem(tc, idx)
	item["status"] = itemStatus
	return item
}
