package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Raw fields preserve extensions, large integers and explicit zero values.
// Fields are immutable; retries only replace Model on a shallow copy.
type GeminiDecisionsRequest OpenAIDecisionsRequest

func (r *GeminiDecisionsRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	var model string
	if common.Unmarshal(fields["model"], &model) != nil || strings.TrimSpace(model) == "" {
		return errors.New("model must be a non-empty string")
	}
	if _, err := geminiDecisionContent(fields["input"], true); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	var questions map[string]struct {
		Type         string          `json:"type"`
		Instructions json.RawMessage `json:"instructions"`
		Criteria     json.RawMessage `json:"criteria"`
	}
	if common.Unmarshal(fields["questions"], &questions) != nil || len(questions) < 1 || len(questions) > 1000 {
		return errors.New("questions must be an object containing 1 to 1000 questions")
	}
	for name, q := range questions {
		if !geminiDecisionID(name) {
			return errors.New("question IDs must be non-empty without surrounding whitespace or control characters")
		}
		if _, err := geminiDecisionContent(q.Instructions, false); err != nil {
			return fmt.Errorf("questions.%s.instructions: %w", name, err)
		}
		switch q.Type {
		case "noul":
			if len(q.Criteria) == 0 {
				break
			}
			var criteria map[string]json.RawMessage
			if common.Unmarshal(q.Criteria, &criteria) != nil || criteria == nil {
				return fmt.Errorf("questions.%s.criteria must be an object", name)
			}
			for _, key := range []string{"true_description", "false_description"} {
				if raw, ok := criteria[key]; ok {
					var value *string
					if common.Unmarshal(raw, &value) != nil || value == nil {
						return fmt.Errorf("questions.%s.criteria.%s must be a string", name, key)
					}
				}
			}
		case "choice":
			var criteria map[string]*string
			if common.Unmarshal(q.Criteria, &criteria) != nil || len(criteria) < 2 || len(criteria) > 255 {
				return fmt.Errorf("questions.%s.criteria must contain 2 to 255 string options", name)
			}
			for key, value := range criteria {
				if !geminiDecisionID(key) || value == nil {
					return fmt.Errorf("questions.%s.criteria requires valid option IDs and string descriptions", name)
				}
			}
		case "score":
			var criteria []*string
			if common.Unmarshal(q.Criteria, &criteria) != nil || len(criteria) < 2 || len(criteria) > 255 {
				return fmt.Errorf("questions.%s.criteria must contain 2 to 255 non-empty levels", name)
			}
			for _, level := range criteria {
				if level == nil || strings.TrimSpace(*level) == "" {
					return fmt.Errorf("questions.%s.criteria levels must be non-empty strings", name)
				}
			}
		default:
			return fmt.Errorf("questions.%s.type must be noul, choice or score", name)
		}
	}
	if raw, ok := fields["stream"]; ok {
		var stream *bool
		if common.Unmarshal(raw, &stream) != nil || (stream != nil && *stream) {
			return errors.New("Gemini Decisions does not support streaming")
		}
	}
	if raw := bytes.TrimSpace(fields["stream_options"]); len(raw) > 0 && string(raw) != "null" {
		return errors.New("Gemini Decisions does not support stream_options")
	}
	r.Model, r.Fields = model, fields
	return nil
}

func geminiDecisionID(id string) bool {
	return id != "" && strings.TrimSpace(id) == id && strings.IndexFunc(id, unicode.IsControl) < 0
}

// Extract textual input for estimation without downloading media or tokenizing
// base64 data. Actual multimodal consumption comes from upstream usage.
func geminiDecisionContent(raw json.RawMessage, media bool) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("content is required")
	}
	if raw[0] == '"' {
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return []string{value}, nil
	}
	var parts []json.RawMessage
	if raw[0] == '[' {
		if common.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
			return nil, errors.New("content blocks must be a non-empty array")
		}
	} else {
		parts = []json.RawMessage{raw}
	}
	var texts []string
	for _, part := range parts {
		var block struct {
			Type string  `json:"type"`
			Text *string `json:"text"`
			Data *string `json:"data"`
			URI  *string `json:"uri"`
		}
		if common.Unmarshal(part, &block) != nil {
			return nil, errors.New("content must be a string or typed content blocks")
		}
		switch block.Type {
		case "text":
			if block.Text == nil {
				return nil, errors.New("text content requires text")
			}
			texts = append(texts, *block.Text)
		case "image", "audio", "video", "document":
			if !media {
				return nil, errors.New("instructions only support text content")
			}
			if (block.Data == nil || *block.Data == "") && (block.URI == nil || *block.URI == "") {
				return nil, errors.New("media content requires data or uri")
			}
		default:
			return nil, errors.New("unsupported Gemini Decisions content type")
		}
	}
	return texts, nil
}

func (r GeminiDecisionsRequest) MarshalJSON() ([]byte, error) {
	return OpenAIDecisionsRequest(r).MarshalJSON()
}
func (r *GeminiDecisionsRequest) SetModelName(model string)  { r.Model = model }
func (r *GeminiDecisionsRequest) IsStream(*gin.Context) bool { return false }
func (r *GeminiDecisionsRequest) GetTokenCountMeta() *types.TokenCountMeta {
	texts, _ := geminiDecisionContent(r.Fields["input"], true)
	texts = append(texts, string(r.Fields["questions"]))
	return &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, CombineText: strings.Join(texts, "\n")}
}
