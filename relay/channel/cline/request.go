package cline

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// ForceStreamRequest runs after parameter overrides and body passthrough, so
// Cline always receives SSE requests while unknown fields and zero values survive.
func ForceStreamRequest(body io.Reader) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := common.DecodeJson(body, &request); err != nil {
		return nil, err
	}
	if request == nil {
		return nil, fmt.Errorf("Cline request must be a JSON object")
	}
	options := make(map[string]json.RawMessage)
	if raw := request["stream_options"]; len(raw) > 0 && string(raw) != "null" {
		if err := common.Unmarshal(raw, &options); err != nil {
			return nil, err
		}
	}
	if options == nil {
		options = make(map[string]json.RawMessage)
	}
	options["include_usage"] = json.RawMessage("true")
	var err error
	request["stream_options"], err = common.Marshal(options)
	if err != nil {
		return nil, err
	}
	request["stream"] = json.RawMessage("true")
	return common.Marshal(request)
}

// Cline SDK withMaxCompletionTokensForReasoningModels, pinned to the same
// revision as client_headers.go. Other provider-qualified IDs remain intact.
var reasoningModelID = regexp.MustCompile(`(^|[^a-z0-9])(o[134]|gpt-?5)($|[^a-z0-9])`)

func NormalizeRequest(request *dto.GeneralOpenAIRequest) {
	if request.MaxTokens == nil || !reasoningModelID.MatchString(strings.ToLower(strings.TrimSpace(request.Model))) {
		return
	}
	if request.MaxCompletionTokens == nil {
		request.MaxCompletionTokens = request.MaxTokens
	}
	request.MaxTokens = nil
}
