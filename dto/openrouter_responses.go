package dto

import (
	"bytes"
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
)

// Preserve an explicitly empty previous_response_id through copies so providers
// that reject every non-null reference can validate it after parameter overrides.
func (r *OpenAIResponsesRequest) UnmarshalJSON(data []byte) error {
	type alias OpenAIResponsesRequest
	var parsed alias
	if err := common.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*r = OpenAIResponsesRequest(parsed)
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	r.previousResponseIDPresent = len(fields["previous_response_id"]) > 0 && string(bytes.TrimSpace(fields["previous_response_id"])) != "null"
	return nil
}

func (r OpenAIResponsesRequest) MarshalJSON() ([]byte, error) {
	type alias OpenAIResponsesRequest
	if r.previousResponseIDPresent && r.PreviousResponseID == "" {
		return common.Marshal(struct {
			alias
			PreviousResponseID string `json:"previous_response_id"`
		}{alias(r), ""})
	}
	return common.Marshal(alias(r))
}
