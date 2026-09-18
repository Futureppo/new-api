package controller

import (
	"bytes"
	"net/http"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/tidwall/gjson"
)

// Capture only the upstream body consumed by the adaptor. Do not register a
// conversation log or inspect its converted (possibly model-mapped) output.
func captureTestUpstreamResponse(info *relaycommon.RelayInfo, resp *http.Response) *relaycommon.ConversationCapture {
	capture := relaycommon.NewConversationCapture()
	info.ConversationCapture = capture
	relaycommon.WrapConversationUpstreamResponse(info, resp)
	return capture
}

func extractTestUpstreamModel(body []byte) string {
	if gjson.ValidBytes(body) {
		return extractTestModelJSON(body)
	}

	// Assemble SSE data fields before parsing so network read boundaries, CRLF,
	// multi-line events and large events do not affect model extraction.
	var event []byte
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			if name := extractTestModelJSON(event); name != "" {
				return name
			}
			event = event[:0]
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		if len(event) > 0 {
			event = append(event, '\n')
		}
		event = append(event, bytes.TrimPrefix(line[len("data:"):], []byte{' '})...)
	}
	return extractTestModelJSON(event)
}

func extractTestModelJSON(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	value := gjson.ParseBytes(body)
	if !value.IsArray() {
		return extractTestModelMetadata(value)
	}
	var name string
	value.ForEach(func(_, item gjson.Result) bool {
		name = extractTestModelMetadata(item)
		return name == ""
	})
	return name
}

func extractTestModelMetadata(value gjson.Result) string {
	if !value.IsObject() {
		return ""
	}
	// Restrict extraction to response metadata, never generated text, tool
	// arguments, request echoes or a configured/requested model fallback.
	for _, path := range []string{
		"model", "modelVersion", "response.model", "response.modelVersion",
		"message.model", "data.model", "result.model", "result.modelVersion",
	} {
		field := value.Get(path)
		if field.Type == gjson.String {
			if name := strings.TrimSpace(field.String()); name != "" {
				return name
			}
		}
	}
	return ""
}
