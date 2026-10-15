package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// OpenRouter Decisions uses named questions and state, not OpenAI's input array.
// Raw fields preserve routing extensions and explicit zero values on retries.
type OpenRouterDecisionsRequest OpenAIDecisionsRequest

func decisionGuidance(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && (raw[0] == '"' || raw[0] == '{' || raw[0] == '[')
}

func (r *OpenRouterDecisionsRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	var model string
	if common.Unmarshal(fields["model"], &model) != nil || strings.TrimSpace(model) == "" {
		return errors.New("model must be a non-empty string")
	}
	if !decisionGuidance(fields["state"]) {
		return errors.New("state must be a string, object or array")
	}
	var questions map[string]struct {
		Type         string          `json:"type"`
		Instructions json.RawMessage `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if common.Unmarshal(fields["questions"], &questions) != nil || len(questions) == 0 {
		return errors.New("questions must be a non-empty object")
	}
	for name, q := range questions {
		if !decisionGuidance(q.Instructions) {
			return fmt.Errorf("questions.%s.instructions must be a string, object or array", name)
		}
		switch q.Type {
		case "noul", "choice":
			if q.Type == "noul" && len(q.Criteria) == 0 {
				continue
			}
			var criteria map[string]json.RawMessage
			if common.Unmarshal(q.Criteria, &criteria) != nil || criteria == nil {
				return fmt.Errorf("questions.%s.criteria must be an object", name)
			}
			if q.Type == "noul" && (!decisionGuidance(criteria["true"]) || !decisionGuidance(criteria["false"])) {
				return fmt.Errorf("questions.%s.criteria requires true and false guidance", name)
			}
			if q.Type == "choice" {
				for _, guidance := range criteria {
					if !decisionGuidance(guidance) && string(bytes.TrimSpace(guidance)) != "null" {
						return fmt.Errorf("questions.%s.criteria values must be strings, objects, arrays or null", name)
					}
				}
			}
		case "score":
			var criteria []json.RawMessage
			if common.Unmarshal(q.Criteria, &criteria) != nil || len(criteria) == 0 {
				return fmt.Errorf("questions.%s.criteria must be a non-empty array", name)
			}
			for _, guidance := range criteria {
				if !decisionGuidance(guidance) {
					return fmt.Errorf("questions.%s.criteria values must be strings, objects or arrays", name)
				}
			}
		default:
			return fmt.Errorf("questions.%s.type must be noul, choice or score", name)
		}
	}
	if raw, exists := fields["stream"]; exists {
		var stream *bool
		if common.Unmarshal(raw, &stream) != nil || (stream != nil && *stream) {
			return errors.New("OpenRouter Decisions does not support streaming")
		}
	}
	if raw := bytes.TrimSpace(fields["stream_options"]); len(raw) > 0 && string(raw) != "null" {
		return errors.New("OpenRouter Decisions does not support stream_options")
	}
	r.Model, r.Fields = model, fields
	return nil
}

func (r OpenRouterDecisionsRequest) MarshalJSON() ([]byte, error) {
	return OpenAIDecisionsRequest(r).MarshalJSON()
}
func (r *OpenRouterDecisionsRequest) SetModelName(model string)  { r.Model = model }
func (r *OpenRouterDecisionsRequest) IsStream(*gin.Context) bool { return false }
func (r *OpenRouterDecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, CombineText: string(r.Fields["state"]) + "\n" + string(r.Fields["questions"])}
}
