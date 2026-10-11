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

// OpenAIDecisionsRequest keeps the native payload intact. Fields are immutable
// after decoding, so model mapping on a shallow copy is safe across retries.
type OpenAIDecisionsRequest struct {
	Model  string
	Fields map[string]json.RawMessage
}

type DecisionQuestion struct {
	Type         string          `json:"type"`
	Instructions *string         `json:"instructions,omitempty"`
	Name         *string         `json:"name,omitempty"`
	Choices      json.RawMessage `json:"choices,omitempty"`
	Levels       json.RawMessage `json:"levels,omitempty"`
}

func (r *OpenAIDecisionsRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	var model string
	if err := common.Unmarshal(fields["model"], &model); err != nil || strings.TrimSpace(model) == "" {
		return errors.New("model must be a non-empty string")
	}
	if _, err := decisionsInputText(fields["input"]); err != nil {
		return err
	}
	var questions []DecisionQuestion
	if err := common.Unmarshal(fields["questions"], &questions); err != nil || len(questions) == 0 {
		return errors.New("questions must be a non-empty array")
	}
	for i, question := range questions {
		if err := validateDecisionQuestion(question); err != nil {
			return fmt.Errorf("questions[%d]: %w", i, err)
		}
	}
	if raw, ok := fields["stream"]; ok {
		var stream *bool
		if err := common.Unmarshal(raw, &stream); err != nil || (stream != nil && *stream) {
			return errors.New("Decisions does not support streaming")
		}
	}
	if raw := bytes.TrimSpace(fields["stream_options"]); len(raw) > 0 && string(raw) != "null" {
		return errors.New("Decisions does not support stream_options")
	}
	if raw, ok := fields["safety_identifier"]; ok {
		var identifier *string
		if err := common.Unmarshal(raw, &identifier); err != nil {
			return errors.New("safety_identifier must be a string or null")
		}
	}
	r.Model, r.Fields = model, fields
	return nil
}

func validateDecisionQuestion(question DecisionQuestion) error {
	if question.Instructions == nil {
		return errors.New("instructions must be a string")
	}
	switch question.Type {
	case "predicate":
		return nil
	case "choice":
		var choices []struct {
			Value       json.RawMessage `json:"value"`
			Description *string         `json:"description,omitempty"`
		}
		if err := common.Unmarshal(question.Choices, &choices); err != nil || len(choices) < 2 || len(choices) > 255 {
			return errors.New("choices must contain 2 to 255 options")
		}
		seen := make(map[any]bool, len(choices))
		for _, choice := range choices {
			var value any
			if err := common.Unmarshal(choice.Value, &value); err != nil {
				return errors.New("choice value must be a string or boolean")
			}
			switch value.(type) {
			case string, bool:
			default:
				return errors.New("choice value must be a string or boolean")
			}
			if seen[value] {
				return errors.New("choice values must be unique")
			}
			seen[value] = true
		}
	case "score":
		var levels []struct {
			Label       *string `json:"label"`
			Description *string `json:"description,omitempty"`
		}
		if err := common.Unmarshal(question.Levels, &levels); err != nil || len(levels) == 0 {
			return errors.New("levels must be a non-empty array")
		}
		for _, level := range levels {
			if level.Label == nil {
				return errors.New("level label must be a string")
			}
		}
	default:
		return errors.New("type must be predicate, choice or score")
	}
	return nil
}

// Return only text for token estimation and sensitive-word checks. Image data
// must not be tokenized as base64 or downloaded by the gateway. Actual image
// usage is included in the upstream input_tokens used for settlement.
func decisionsInputText(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("input is required")
	}
	if raw[0] == '"' {
		var input string
		if err := common.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
		return []string{input}, nil
	}
	var messages []struct {
		Role    string          `json:"role"`
		Type    *string         `json:"type,omitempty"`
		Content json.RawMessage `json:"content"`
	}
	if err := common.Unmarshal(raw, &messages); err != nil || len(messages) == 0 {
		return nil, errors.New("input must be a string or a non-empty array of user messages")
	}
	var texts []string
	for _, message := range messages {
		if message.Role != "user" || (message.Type != nil && *message.Type != "message") {
			return nil, errors.New("Decisions input only supports user messages")
		}
		content := bytes.TrimSpace(message.Content)
		if len(content) > 0 && content[0] == '"' {
			var text string
			if err := common.Unmarshal(content, &text); err != nil {
				return nil, err
			}
			texts = append(texts, text)
			continue
		}
		var parts []struct {
			Type     string  `json:"type"`
			Text     *string `json:"text,omitempty"`
			ImageURL *string `json:"image_url,omitempty"`
		}
		if err := common.Unmarshal(content, &parts); err != nil || len(parts) == 0 {
			return nil, errors.New("message content must be text or a non-empty array of input parts")
		}
		for _, part := range parts {
			switch part.Type {
			case "input_text":
				if part.Text == nil {
					return nil, errors.New("input_text requires text")
				}
				texts = append(texts, *part.Text)
			case "input_image":
				if part.ImageURL == nil || *part.ImageURL == "" {
					return nil, errors.New("input_image requires image_url")
				}
			default:
				return nil, errors.New("Decisions only supports input_text and input_image parts")
			}
		}
	}
	return texts, nil
}

func (r OpenAIDecisionsRequest) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(r.Fields)+1)
	for key, value := range r.Fields {
		fields[key] = value
	}
	model, err := common.Marshal(r.Model)
	if err != nil {
		return nil, err
	}
	fields["model"] = model
	return common.Marshal(fields)
}

func (r *OpenAIDecisionsRequest) SetModelName(model string)  { r.Model = model }
func (r *OpenAIDecisionsRequest) IsStream(*gin.Context) bool { return false }
func (r *OpenAIDecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	texts, _ := decisionsInputText(r.Fields["input"])
	texts = append(texts, string(r.Fields["questions"]))
	return &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, CombineText: strings.Join(texts, "\n")}
}
