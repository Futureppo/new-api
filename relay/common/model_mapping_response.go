package common

import (
	"bytes"
	"encoding/json"
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

func (w *relayResponseWriter) WriteHeader(code int) {
	info := GetRelayInfo(w.context)
	if (info != nil && info.IsModelMappingFullActive()) || (code >= 400 && shouldHideChannelErrorDetails(w.context)) {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *relayResponseWriter) WriteHeaderNow() {
	info := GetRelayInfo(w.context)
	if (info != nil && info.IsModelMappingFullActive()) ||
		(w.ResponseWriter.Status() >= 400 && shouldHideChannelErrorDetails(w.context)) {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeaderNow()
}

func (w *relayResponseWriter) Flush() {
	info := GetRelayInfo(w.context)
	if (info != nil && info.IsModelMappingFullActive()) ||
		(w.ResponseWriter.Status() >= 400 && shouldHideChannelErrorDetails(w.context)) {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.Flush()
}

func (w *relayResponseWriter) Write(data []byte) (int, error) {
	rewritten := rewriteClientResponseBytes(w.context, data, &w.sseEventError)
	n, err := w.ResponseWriter.Write(rewritten)
	if err == nil && n == len(rewritten) {
		return len(data), nil
	}
	return n, err
}

func (w *relayResponseWriter) WriteString(data string) (int, error) {
	rewritten := string(rewriteClientResponseBytes(w.context, []byte(data), &w.sseEventError))
	n, err := w.ResponseWriter.WriteString(rewritten)
	if err == nil && n == len(rewritten) {
		return len(data), nil
	}
	return n, err
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
		changed := false

		for key, raw := range object {
			normalized := normalizeModelKey(key)
			if objectIsError && isErrorDetailContainerKey(normalized) {
				delete(object, key)
				changed = true
				continue
			}

			childInsideError := false
			if normalized == "error" || normalized == "errors" {
				if isJSONNull(raw) {
					continue
				}
				childInsideError = true
			}

			if objectIsError {
				switch {
				case normalized == "code" && rawScalarString(raw) == "":
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
				case normalized == "type" && hasExplicitErrorCode(object) && !rawStringEquals(raw, "error"):
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
	changed := false
	eventIsError := forceError
	if sseEventError != nil && *sseEventError {
		eventIsError = true
	}
	for i, line := range lines {
		lineEnding := []byte{}
		body := line
		if bytes.HasSuffix(body, []byte("\n")) {
			lineEnding = []byte("\n")
			body = body[:len(body)-1]
		}
		trimmed := bytes.TrimLeft(body, " \t\r")
		if len(trimmed) == 0 {
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
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}

		prefixLen := len(body) - len(trimmed) + len("data:")
		payloadStart := prefixLen
		for payloadStart < len(body) && (body[payloadStart] == ' ' || body[payloadStart] == '\t') {
			payloadStart++
		}
		payload := body[payloadStart:]
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}

		var rewritten []byte
		payloadChanged := false
		if looksLikeJSON(payload) {
			var err error
			rewritten, payloadChanged, err = rewriteErrorJSONValue(payload, eventIsError, false, "", 0)
			if err == nil && !payloadChanged {
				continue
			}
			if err != nil && !eventIsError {
				continue
			}
		} else if !eventIsError {
			continue
		}
		if !payloadChanged {
			rewritten = []byte("upstream_error")
			payloadChanged = !bytes.Equal(payload, rewritten)
		}
		if !payloadChanged {
			continue
		}
		lines[i] = append(append(append([]byte{}, body[:payloadStart]...), rewritten...), lineEnding...)
		changed = true
	}
	if sseEventError != nil {
		*sseEventError = eventIsError
	}
	if !changed {
		return data
	}
	return bytes.Join(lines, nil)
}

func isErrorEventObject(object map[string]json.RawMessage) bool {
	for _, key := range []string{"type", "event"} {
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
		if value := rawScalarString(object[key]); value != "" {
			return value
		}
	}
	if value := rawScalarString(object["type"]); value != "" && !strings.EqualFold(value, "error") {
		return value
	}
	if fallback != "" {
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

func isErrorDetailContainerKey(key string) bool {
	switch key {
	case "metadata", "details", "data", "properties", "result", "responsebody", "debug", "stack", "trace":
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
