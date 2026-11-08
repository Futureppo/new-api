package common

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

type relayResponseWriter struct {
	gin.ResponseWriter
	context       *gin.Context
	sseEventError bool
	ssePending    []byte
}

func SetRelayInfo(c *gin.Context, info *RelayInfo) {
	if c == nil {
		return
	}
	basecommon.SetContextKey(c, constant.ContextKeyRelayInfo, info)
}

func GetRelayInfo(c *gin.Context) *RelayInfo {
	if c == nil {
		return nil
	}
	info, _ := basecommon.GetContextKeyType[*RelayInfo](c, constant.ContextKeyRelayInfo)
	return info
}

func InstallRelayResponseWriter(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	if _, ok := c.Writer.(*relayResponseWriter); ok {
		return
	}
	c.Writer = &relayResponseWriter{ResponseWriter: c.Writer, context: c}
}

// InstallModelMappingResponseWriter is kept as a compatibility alias for callers
// that only need model mapping. The installed writer now also enforces channel
// error-detail privacy.
func InstallModelMappingResponseWriter(c *gin.Context) {
	InstallRelayResponseWriter(c)
}

func (w *relayResponseWriter) removeContentLengthForRewrite(code int) {
	info := GetRelayInfo(w.context)
	privacyRewrite := shouldHideChannelErrorDetails(w.context) &&
		(code >= 400 || strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream"))
	if (info != nil && info.IsModelMappingFullActive()) || privacyRewrite {
		w.Header().Del("Content-Length")
	}
}

func (w *relayResponseWriter) WriteHeader(code int) {
	w.removeContentLengthForRewrite(code)
	w.ResponseWriter.WriteHeader(code)
}

func (w *relayResponseWriter) WriteHeaderNow() {
	w.removeContentLengthForRewrite(w.ResponseWriter.Status())
	w.ResponseWriter.WriteHeaderNow()
}

func (w *relayResponseWriter) Flush() {
	w.removeContentLengthForRewrite(w.ResponseWriter.Status())
	w.ResponseWriter.Flush()
}

func (w *relayResponseWriter) Write(data []byte) (int, error) {
	inputLen := len(data)
	if shouldHideChannelErrorDetails(w.context) && strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream") {
		// Writes are transport chunks, not complete events. Wait for the SSE
		// delimiter so split JSON and multiline data cannot escape filtering.
		if len(w.ssePending)+len(data) > 64<<20 {
			w.ssePending = nil
			return 0, errors.New("relay SSE event exceeds privacy buffer limit")
		}
		w.ssePending = append(w.ssePending, data...)
		end := bytes.LastIndex(w.ssePending, []byte("\n\n"))
		if end >= 0 {
			end += 2
		}
		if crlfEnd := bytes.LastIndex(w.ssePending, []byte("\r\n\r\n")); crlfEnd >= 0 && crlfEnd+4 > end {
			end = crlfEnd + 4
		}
		if end < 0 {
			return inputLen, nil
		}
		data = w.ssePending[:end]
		w.ssePending = bytes.Clone(w.ssePending[end:])
	}
	rewritten := rewriteClientResponseBytes(w.context, data, &w.sseEventError)
	if !bytes.Equal(data, rewritten) {
		w.Header().Del("Content-Length")
	}
	n, err := w.ResponseWriter.Write(rewritten)
	if err == nil && n == len(rewritten) {
		return inputLen, nil
	}
	if err == nil {
		err = io.ErrShortWrite
	}
	return 0, err
}

func (w *relayResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func RewriteClientResponseBytes(c *gin.Context, data []byte) []byte {
	return rewriteClientResponseBytes(c, data, nil)
}

func rewriteClientResponseBytes(c *gin.Context, data []byte, sseEventError *bool) []byte {
	rewritten := rewriteClientErrorDetailsBytes(c, data, sseEventError)
	info := GetRelayInfo(c)
	if info == nil || !info.IsModelMappingFullActive() || len(rewritten) == 0 {
		return rewritten
	}
	return RewriteModelMappingBytes(rewritten, info.ClientModelName, hiddenModelNames(info), responseIsError(c))
}

func rewriteClientErrorDetailsBytes(c *gin.Context, data []byte, sseEventError *bool) []byte {
	if c == nil || len(data) == 0 {
		return data
	}
	if !shouldHideChannelErrorDetails(c) {
		return data
	}

	forceError := responseIsError(c)
	if looksLikeJSON(data) {
		if rewritten, changed, err := rewriteErrorJSONValue(data, forceError, false, "", 0); err == nil {
			if changed {
				return rewritten
			}
			return data
		}
	}
	rewritten := rewriteErrorSSEPayloads(data, forceError, sseEventError)
	if forceError && bytes.Equal(rewritten, data) && !bytes.Contains(data, []byte("data:")) {
		return []byte("upstream_error")
	}
	return rewritten
}

func looksLikeJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return false
	}
	first := trimmed[0]
	return first == '{' || first == '[' || first == '"' || first == '-' ||
		(first >= '0' && first <= '9') || first == 't' || first == 'f' || first == 'n'
}

func shouldHideChannelErrorDetails(c *gin.Context) bool {
	channelSetting, ok := basecommon.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	return ok && !channelSetting.ShowErrorDetails
}

func rewriteErrorJSONValue(data []byte, forceError bool, insideError bool, fallbackCode string, depth int) ([]byte, bool, error) {
	if depth > 64 {
		if forceError || insideError {
			return []byte(`"upstream_error"`), true, nil
		}
		return data, false, nil
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return data, false, nil
	}

	switch trimmed[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := basecommon.Unmarshal(trimmed, &object); err != nil {
			return data, false, err
		}
		objectIsError := forceError || insideError || isErrorEventObject(object)
		code := errorCodeForObject(object, fallbackCode)
		hasCode := hasExplicitErrorCode(object)
		changed := false

		for key, raw := range object {
			normalized := normalizeModelKey(key)
			// Only retain protocol fields in an error. A denylist cannot cover
			// every provider's channel, account, endpoint and diagnostic fields.
			if objectIsError && !isPublicErrorField(normalized) {
				delete(object, key)
				changed = true
				continue
			}

			childInsideError := objectIsError && normalized == "response"
			if normalized == "error" || normalized == "errors" {
				if isJSONNull(raw) {
					continue
				}
				childInsideError = true
			}

			if objectIsError {
				switch {
				case normalized == "success":
					if !bytes.Equal(bytes.TrimSpace(raw), []byte("false")) {
						delete(object, key)
						changed = true
					}
					continue
				case normalized == "id" || normalized == "eventid" || normalized == "requestid" || normalized == "sequencenumber":
					if !basecommon.IsPublicErrorCode(rawScalarString(raw)) {
						delete(object, key)
						changed = true
					}
					continue
				case (normalized == "code" || normalized == "status" || normalized == "type" || normalized == "event" || normalized == "object") && !basecommon.IsPublicErrorCode(rawScalarString(raw)):
					replacement, err := basecommon.Marshal(code)
					if err != nil {
						return data, false, err
					}
					object[key] = replacement
					changed = true
					continue
				case isErrorMessageKey(normalized):
					replacement, err := basecommon.Marshal(code)
					if err != nil {
						return data, false, err
					}
					if !bytes.Equal(bytes.TrimSpace(raw), replacement) {
						object[key] = replacement
						changed = true
					}
					continue
				case normalized == "param":
					replacement := json.RawMessage(`""`)
					if !bytes.Equal(bytes.TrimSpace(raw), replacement) {
						object[key] = replacement
						changed = true
					}
					continue
				case normalized == "type" && hasCode && !rawStringEquals(raw, "error"):
					replacement := json.RawMessage(`"upstream_error"`)
					if !bytes.Equal(bytes.TrimSpace(raw), replacement) {
						object[key] = replacement
						changed = true
					}
					continue
				}
			}

			rewritten, childChanged, err := rewriteErrorJSONValue(raw, false, childInsideError, code, depth+1)
			if err != nil {
				return data, false, err
			}
			if childChanged {
				object[key] = rewritten
				changed = true
			}
		}

		if !changed {
			return data, false, nil
		}
		out, err := basecommon.Marshal(object)
		return out, err == nil, err
	case '[':
		var array []json.RawMessage
		if err := basecommon.Unmarshal(trimmed, &array); err != nil {
			return data, false, err
		}
		changed := false
		for i, raw := range array {
			rewritten, childChanged, err := rewriteErrorJSONValue(raw, forceError, insideError, fallbackCode, depth+1)
			if err != nil {
				return data, false, err
			}
			if childChanged {
				array[i] = rewritten
				changed = true
			}
		}
		if !changed {
			return data, false, nil
		}
		out, err := basecommon.Marshal(array)
		return out, err == nil, err
	case '"':
		if !insideError && !forceError {
			return data, false, nil
		}
		code := fallbackCode
		if code == "" {
			code = "upstream_error"
		}
		out, err := basecommon.Marshal(code)
		return out, err == nil && !bytes.Equal(trimmed, out), err
	default:
		return data, false, nil
	}
}

func rewriteErrorSSEPayloads(data []byte, forceError bool, sseEventError *bool) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))
	eventIsError := forceError || (sseEventError != nil && *sseEventError)
	var payloadLines []int
	var payloads [][]byte
	changed := false
	flush := func() {
		if len(payloads) == 0 {
			return
		}
		payload := bytes.Join(payloads, []byte("\n"))
		if bytes.Equal(bytes.TrimSpace(payload), []byte("[DONE]")) {
			return
		}
		rewritten, payloadChanged, err := rewriteErrorJSONValue(payload, eventIsError, false, "", 0)
		if err != nil || !looksLikeJSON(payload) {
			if !eventIsError {
				return
			}
			rewritten = []byte("upstream_error")
			payloadChanged = !bytes.Equal(payload, rewritten)
		}
		if !payloadChanged {
			return
		}
		first := payloadLines[0]
		ending := []byte("\n")
		if bytes.HasSuffix(lines[first], []byte("\r\n")) {
			ending = []byte("\r\n")
		} else if !bytes.HasSuffix(lines[first], []byte("\n")) {
			ending = nil
		}
		lines[first] = append(append([]byte("data: "), rewritten...), ending...)
		for _, index := range payloadLines[1:] {
			lines[index] = nil
		}
		changed = true
	}
	for i, line := range lines {
		body := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		trimmed := bytes.TrimLeft(body, " \t")
		if len(trimmed) == 0 {
			flush()
			payloadLines = nil
			payloads = nil
			if len(line) > 0 {
				eventIsError = forceError
			}
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("event:")) {
			eventType := strings.TrimSpace(string(trimmed[len("event:"):]))
			eventIsError = forceError || isErrorEventType(eventType)
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("data:")) {
			payloadLines = append(payloadLines, i)
			payloads = append(payloads, bytes.TrimPrefix(trimmed[len("data:"):], []byte(" ")))
		}
	}
	flush()
	if sseEventError != nil {
		*sseEventError = eventIsError
	}
	if !changed {
		return data
	}
	return bytes.Join(lines, nil)
}

func isErrorEventObject(object map[string]json.RawMessage) bool {
	for _, key := range []string{"type", "event", "object"} {
		if value := rawScalarString(object[key]); isErrorEventType(value) {
			return true
		}
	}
	status := strings.ToLower(rawScalarString(object["status"]))
	if status == "failed" || status == "failure" || status == "error" {
		return true
	}
	if raw, ok := object["error"]; ok && !isJSONNull(raw) {
		return true
	}
	if raw, ok := object["errors"]; ok && !isJSONNull(raw) && string(bytes.TrimSpace(raw)) != "[]" {
		return true
	}
	if bytes.Equal(bytes.TrimSpace(object["success"]), []byte("false")) {
		return true
	}
	code := strings.ToLower(rawScalarString(object["code"]))
	_, hasMessage := object["message"]
	_, hasDescription := object["description"]
	return code != "" && code != "0" && code != "200" && code != "success" && (hasMessage || hasDescription)
}

func isErrorEventType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "error" || value == "failed" || value == "failure" ||
		strings.HasSuffix(value, ".error") || strings.HasSuffix(value, "_error") ||
		strings.HasSuffix(value, ".failed") || strings.HasSuffix(value, "_failed")
}

func errorCodeForObject(object map[string]json.RawMessage, fallback string) string {
	for _, key := range []string{"code", "status"} {
		if value := rawScalarString(object[key]); basecommon.IsPublicErrorCode(value) {
			return value
		}
	}
	if value := rawScalarString(object["type"]); basecommon.IsPublicErrorCode(value) && !strings.EqualFold(value, "error") {
		return value
	}
	if basecommon.IsPublicErrorCode(fallback) {
		return fallback
	}
	return "upstream_error"
}

func hasExplicitErrorCode(object map[string]json.RawMessage) bool {
	return rawScalarString(object["code"]) != "" || rawScalarString(object["status"]) != ""
}

func rawScalarString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var value string
		if err := basecommon.Unmarshal(trimmed, &value); err != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
	return string(trimmed)
}

func isJSONNull(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func isErrorMessageKey(key string) bool {
	switch key {
	case "message", "description", "detail", "errormessage", "failreason", "failurereason":
		return true
	default:
		return false
	}
}

func isPublicErrorField(key string) bool {
	switch key {
	case "error", "errors", "response", "code", "status", "type", "event", "object", "param", "success",
		"message", "description", "detail", "errormessage", "failreason", "failurereason",
		"eventid", "requestid", "id", "sequencenumber":
		return true
	default:
		return false
	}
}

func RewriteModelMappingBytes(data []byte, displayModel string, hiddenModels []string, rewriteErrors bool) []byte {
	if strings.TrimSpace(displayModel) == "" || len(data) == 0 {
		return data
	}
	hiddenModels = normalizeHiddenModels(displayModel, hiddenModels)
	if rewritten, changed, err := rewriteJSONValue(data, displayModel, hiddenModels, rewriteErrors, false, 0); err == nil && changed {
		return rewritten
	}
	return rewriteSSEPayloads(data, displayModel, hiddenModels, rewriteErrors)
}

func normalizeHiddenModels(displayModel string, hiddenModels []string) []string {
	seen := make(map[string]struct{}, len(hiddenModels))
	result := make([]string, 0, len(hiddenModels))
	for _, hidden := range hiddenModels {
		hidden = strings.TrimSpace(hidden)
		if hidden == "" || hidden == displayModel {
			continue
		}
		if _, ok := seen[hidden]; ok {
			continue
		}
		seen[hidden] = struct{}{}
		result = append(result, hidden)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return len(result[i]) > len(result[j])
	})
	return result
}

func RewriteModelMetadataBytes(data []byte, displayModel string, hiddenModels ...string) []byte {
	return RewriteModelMappingBytes(data, displayModel, hiddenModels, false)
}

func SanitizeModelText(info *RelayInfo, text string) string {
	if info == nil || !info.IsModelMappingFullActive() || text == "" {
		return text
	}
	for _, hidden := range hiddenModelNames(info) {
		text = strings.ReplaceAll(text, hidden, info.ClientModelName)
	}
	return text
}

func hiddenModelNames(info *RelayInfo) []string {
	if info == nil {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, 2)
	for _, name := range []string{info.ModelMappingTargetName, info.UpstreamModelName} {
		name = strings.TrimSpace(name)
		if name == "" || name == info.ClientModelName {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return len(result[i]) > len(result[j])
	})
	return result
}

func responseIsError(c *gin.Context) bool {
	return c != nil && c.Writer != nil && c.Writer.Status() >= 400
}

func rewriteSSEPayloads(data []byte, displayModel string, hiddenModels []string, rewriteErrors bool) []byte {
	lines := bytes.SplitAfter(data, []byte("\n"))
	changed := false
	for i, line := range lines {
		lineEnding := []byte{}
		body := line
		if bytes.HasSuffix(body, []byte("\n")) {
			lineEnding = []byte("\n")
			body = body[:len(body)-1]
		}
		trimmed := bytes.TrimLeft(body, " \t")
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		prefixLen := len(body) - len(trimmed) + len("data:")
		payloadStart := prefixLen
		for payloadStart < len(body) && (body[payloadStart] == ' ' || body[payloadStart] == '\t') {
			payloadStart++
		}
		payload := body[payloadStart:]
		if bytes.Equal(payload, []byte("[DONE]")) || len(payload) == 0 {
			continue
		}
		rewritten, payloadChanged, err := rewriteJSONValue(payload, displayModel, hiddenModels, rewriteErrors, false, 0)
		if err != nil || !payloadChanged {
			continue
		}
		lines[i] = append(append(append([]byte{}, body[:payloadStart]...), rewritten...), lineEnding...)
		changed = true
	}
	if !changed {
		return data
	}
	return bytes.Join(lines, nil)
}

func rewriteJSONValue(data []byte, displayModel string, hiddenModels []string, rewriteErrors bool, insideError bool, depth int) ([]byte, bool, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return data, false, nil
	}
	switch trimmed[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := basecommon.Unmarshal(trimmed, &object); err != nil {
			return data, false, err
		}
		rootError := insideError || rawStringEquals(object["type"], "error")
		changed := false
		for key, raw := range object {
			normalized := normalizeModelKey(key)
			if isModelMetadataKey(normalized) && isJSONString(raw) {
				replacement, _ := basecommon.Marshal(displayModel)
				object[key] = replacement
				changed = true
				continue
			}
			childError := rootError || isErrorMetadataKey(normalized)
			rewritten, childChanged, err := rewriteJSONValue(raw, displayModel, hiddenModels, rewriteErrors, childError, depth+1)
			if err != nil {
				return data, false, err
			}
			if childChanged {
				object[key] = rewritten
				changed = true
			}
		}
		if !changed {
			return data, false, nil
		}
		out, err := basecommon.Marshal(object)
		return out, err == nil, err
	case '[':
		var array []json.RawMessage
		if err := basecommon.Unmarshal(trimmed, &array); err != nil {
			return data, false, err
		}
		changed := false
		for i, raw := range array {
			rewritten, childChanged, err := rewriteJSONValue(raw, displayModel, hiddenModels, rewriteErrors, insideError, depth+1)
			if err != nil {
				return data, false, err
			}
			if childChanged {
				array[i] = rewritten
				changed = true
			}
		}
		if !changed {
			return data, false, nil
		}
		out, err := basecommon.Marshal(array)
		return out, err == nil, err
	case '"':
		if !insideError && !rewriteErrors {
			return data, false, nil
		}
		var value string
		if err := basecommon.Unmarshal(trimmed, &value); err != nil {
			return data, false, err
		}
		updated := value
		for _, hidden := range hiddenModels {
			updated = strings.ReplaceAll(updated, hidden, displayModel)
		}
		if updated == value {
			return data, false, nil
		}
		out, err := basecommon.Marshal(updated)
		return out, err == nil, err
	default:
		return data, false, nil
	}
}

func normalizeModelKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	replacer := strings.NewReplacer("_", "", "-", "", ".", "")
	return replacer.Replace(key)
}

func isModelMetadataKey(key string) bool {
	switch key {
	case "model", "modelid", "modelname", "modelversion", "majormodelversion",
		"upstreammodel", "upstreammodelid", "upstreammodelname", "upstreammodelversion",
		"originmodel", "originmodelid", "originmodelname", "originmodelversion",
		"originalmodel", "originalmodelid", "originalmodelname", "originalmodelversion":
		return true
	default:
		return false
	}
}

func isErrorMetadataKey(key string) bool {
	switch key {
	case "error", "errors", "failreason", "failurereason":
		return true
	default:
		return false
	}
}

func isJSONString(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '"'
}

func rawStringEquals(raw json.RawMessage, expected string) bool {
	if !isJSONString(raw) {
		return false
	}
	value, err := strconv.Unquote(string(bytes.TrimSpace(raw)))
	return err == nil && strings.EqualFold(value, expected)
}
