package openaichat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

// Compile-time check that Codec implements ir.Codec.
var _ ir.Codec = (*Codec)(nil)

// Codec implements bidirectional translation between OpenAI Chat Completions and the provider-neutral IR.
type Codec struct{}

// New returns a new OpenAI Chat Completions IR codec.
func New() *Codec {
	return &Codec{}
}

// Format returns ir.FormatOpenAIChat.
func (c *Codec) Format() ir.Format {
	return ir.FormatOpenAIChat
}

// flexInt64 unmarshals an integer, float, or null JSON number into an int64.
type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = 0
		return nil
	}
	var i int64
	if err := json.Unmarshal(trimmed, &i); err == nil {
		*f = flexInt64(i)
		return nil
	}
	var fl float64
	if err := json.Unmarshal(trimmed, &fl); err == nil {
		*f = flexInt64(math.Trunc(fl))
		return nil
	}
	return fmt.Errorf("openaichat: invalid integer value %s", string(trimmed))
}

// flexBool unmarshals a JSON boolean or integer (0/1) into a bool.
type flexBool bool

func (f *flexBool) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*f = false
		return nil
	}
	var b bool
	if err := json.Unmarshal(trimmed, &b); err == nil {
		*f = flexBool(b)
		return nil
	}
	var i int
	if err := json.Unmarshal(trimmed, &i); err == nil {
		*f = flexBool(i != 0)
		return nil
	}
	return fmt.Errorf("openaichat: invalid boolean value %s", string(trimmed))
}

type chatErrorPayload struct {
	Message string          `json:"message,omitempty"`
	Type    string          `json:"type,omitempty"`
	Param   string          `json:"param,omitempty"`
	Code    json.RawMessage `json:"code,omitempty"`
}

func (e *chatErrorPayload) codeString() string {
	return rawJSONToString(e.Code)
}

func rawJSONToString(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err == nil {
		return s
	}
	var i int64
	if err := json.Unmarshal(trimmed, &i); err == nil {
		return strconv.FormatInt(i, 10)
	}
	return string(trimmed)
}

func isNullOrEmptyJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func normalizeFinishReason(raw string) ir.FinishReason {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "stop":
		return ir.FinishStop
	case "length", "max_tokens":
		return ir.FinishLength
	case "tool_calls", "function_call":
		return ir.FinishToolCalls
	case "content_filter":
		return ir.FinishContentFilter
	case "error":
		return ir.FinishError
	default:
		return ir.FinishStop
	}
}

func encodeFinishReason(reason ir.FinishReason) string {
	switch reason {
	case ir.FinishStop:
		return "stop"
	case ir.FinishLength:
		return "length"
	case ir.FinishToolCalls:
		return "tool_calls"
	case ir.FinishContentFilter:
		return "content_filter"
	case ir.FinishError:
		return "error"
	default:
		return "stop"
	}
}

func audioFormatToMediaType(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "audio/wav"
	case "mp3":
		return "audio/mpeg"
	case "ogg":
		return "audio/ogg"
	case "flac":
		return "audio/flac"
	case "aac":
		return "audio/aac"
	case "webm":
		return "audio/webm"
	case "pcm16", "pcm":
		return "audio/pcm"
	default:
		if format == "" {
			return ""
		}
		return "audio/" + strings.ToLower(strings.TrimSpace(format))
	}
}

func mediaTypeToAudioFormat(mediaType string) string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	switch mt {
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/ogg":
		return "ogg"
	case "audio/flac":
		return "flac"
	case "audio/aac":
		return "aac"
	case "audio/webm":
		return "webm"
	case "audio/pcm", "audio/l16":
		return "pcm16"
	default:
		if strings.HasPrefix(mt, "audio/") {
			return strings.TrimPrefix(mt, "audio/")
		}
		return ""
	}
}

func cloneBytes(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
