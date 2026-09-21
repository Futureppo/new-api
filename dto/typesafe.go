package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// TypeSafeRequest preserves native fields, including unknown fields and explicit
// zero values. Fields are immutable after parsing; model mapping changes Model.
type TypeSafeRequest struct {
	Model  string
	Fields map[string]json.RawMessage
}

func (r *TypeSafeRequest) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	var model string
	if err := common.Unmarshal(fields["model"], &model); err != nil || strings.TrimSpace(model) == "" {
		return errors.New("model must be a non-empty string")
	}
	state := bytes.TrimSpace(fields["state"])
	if len(state) == 0 || (state[0] != '"' && state[0] != '{' && state[0] != '[') {
		return errors.New("state must be a string, object or array")
	}
	questions := bytes.TrimSpace(fields["questions"])
	if len(questions) == 0 || questions[0] != '{' {
		return errors.New("questions must be an object")
	}
	if raw, ok := fields["stream"]; ok {
		var stream *bool
		if err := common.Unmarshal(raw, &stream); err != nil || (stream != nil && *stream) {
			return errors.New("TypeSafe does not support streaming")
		}
	}
	if raw := bytes.TrimSpace(fields["stream_options"]); len(raw) > 0 && string(raw) != "null" {
		return errors.New("TypeSafe does not support stream_options")
	}
	r.Model, r.Fields = model, fields
	return nil
}

func (r TypeSafeRequest) MarshalJSON() ([]byte, error) {
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

func (r *TypeSafeRequest) SetModelName(model string)  { r.Model = model }
func (r *TypeSafeRequest) IsStream(*gin.Context) bool { return false }
func (r *TypeSafeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		TokenType:   types.TokenTypeTokenizer,
		CombineText: string(r.Fields["state"]) + "\n" + string(r.Fields["questions"]),
	}
}
