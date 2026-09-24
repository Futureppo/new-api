package cline

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// Cline wraps non-streaming chat completions in {success, data}, while its
// streaming chunks already use the standard OpenAI format.
func UnwrapChatResponse(body []byte) ([]byte, error) {
	var envelope struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid Cline response")
	}
	if envelope.Success == nil {
		return body, nil
	}
	if !*envelope.Success || common.GetJsonType(envelope.Data) != "object" {
		return nil, fmt.Errorf("Cline response was unsuccessful or missing completion data")
	}
	return envelope.Data, nil
}
