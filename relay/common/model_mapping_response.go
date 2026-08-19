package common

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

type modelMappingResponseWriter struct {
	gin.ResponseWriter
	context *gin.Context
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

func InstallModelMappingResponseWriter(c *gin.Context) {
	if c == nil || c.Writer == nil {
		return
	}
	if _, ok := c.Writer.(*modelMappingResponseWriter); ok {
		return
	}
	c.Writer = &modelMappingResponseWriter{ResponseWriter: c.Writer, context: c}
}

func (w *modelMappingResponseWriter) WriteHeader(code int) {
	if info := GetRelayInfo(w.context); info != nil && info.IsModelMappingFullActive() {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *modelMappingResponseWriter) WriteHeaderNow() {
	if info := GetRelayInfo(w.context); info != nil && info.IsModelMappingFullActive() {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeaderNow()
}

func (w *modelMappingResponseWriter) Flush() {
	if info := GetRelayInfo(w.context); info != nil && info.IsModelMappingFullActive() {
		w.Header().Del("Content-Length")
	}
	w.ResponseWriter.Flush()
}

func (w *modelMappingResponseWriter) Write(data []byte) (int, error) {
	rewritten := RewriteClientResponseBytes(w.context, data)
	n, err := w.ResponseWriter.Write(rewritten)
	if err == nil && n == len(rewritten) {
		return len(data), nil
	}
	return n, err
}

func (w *modelMappingResponseWriter) WriteString(data string) (int, error) {
	rewritten := string(RewriteClientResponseBytes(w.context, []byte(data)))
	n, err := w.ResponseWriter.WriteString(rewritten)
	if err == nil && n == len(rewritten) {
		return len(data), nil
	}
	return n, err
}

func RewriteClientResponseBytes(c *gin.Context, data []byte) []byte {
	info := GetRelayInfo(c)
	if info == nil || !info.IsModelMappingFullActive() || len(data) == 0 {
		return data
	}
	return RewriteModelMappingBytes(data, info.ClientModelName, hiddenModelNames(info), responseIsError(c))
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
