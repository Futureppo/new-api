package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

//type OpenAIError struct {
//	Message string `json:"message"`
//	Type    string `json:"type"`
//	Param   string `json:"param"`
//	Code    any    `json:"code"`
//}

type OpenAIErrorWithStatusCode struct {
	Error      types.OpenAIError `json:"error"`
	StatusCode int               `json:"status_code"`
	LocalError bool
}

type GeneralErrorResponse struct {
	Error    json.RawMessage `json:"error"`
	Message  string          `json:"message"`
	Msg      string          `json:"msg"`
	Err      string          `json:"err"`
	ErrorMsg string          `json:"error_msg"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	Detail   json.RawMessage `json:"detail,omitempty"`
	Header   struct {
		Message string `json:"message"`
	} `json:"header"`
	Response struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"response"`
}

func (e GeneralErrorResponse) TryToOpenAIError() *types.OpenAIError {
	if openAIError := rawToOpenAIError(e.Error); openAIError != nil {
		return openAIError
	}
	if openAIError := rawToOpenAIError(e.Detail); openAIError != nil {
		return openAIError
	}
	return nil
}

func (e GeneralErrorResponse) ToMessage() string {
	if msg := rawErrorMessage(e.Error); msg != "" {
		return msg
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Msg != "" {
		return e.Msg
	}
	if e.Err != "" {
		return e.Err
	}
	if e.ErrorMsg != "" {
		return e.ErrorMsg
	}
	if msg := rawErrorMessage(e.Detail); msg != "" {
		return msg
	}
	if e.Header.Message != "" {
		return e.Header.Message
	}
	if e.Response.Error.Message != "" {
		return e.Response.Error.Message
	}
	return ""
}

func rawToOpenAIError(raw json.RawMessage) *types.OpenAIError {
	if len(raw) == 0 || common.GetJsonType(raw) != "object" {
		return nil
	}
	var openAIError types.OpenAIError
	if err := common.Unmarshal(raw, &openAIError); err == nil && openAIError.Message != "" {
		return &openAIError
	}
	return nil
}

func rawErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch common.GetJsonType(raw) {
	case "object":
		if openAIError := rawToOpenAIError(raw); openAIError != nil {
			return openAIError.Message
		}
		var nested GeneralErrorResponse
		if err := common.Unmarshal(raw, &nested); err == nil {
			if msg := nested.ToMessage(); msg != "" {
				return msg
			}
		}
		return string(raw)
	case "string":
		var msg string
		if err := common.Unmarshal(raw, &msg); err == nil && msg != "" {
			return msg
		}
	default:
		return string(raw)
	}
	return ""
}
