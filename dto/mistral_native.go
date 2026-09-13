package dto

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// MistralNativeRequest keeps the original body, including fields unknown to the
// gateway. Only the routing identifier is rewritten when model mapping is used.
type MistralNativeRequest struct {
	Model         string
	OriginalModel string
	ModelField    string
	ContentType   string
	Body          []byte
	Stream        bool
	MaxTokens     int
	Text          string
}

func (r *MistralNativeRequest) SetModelName(model string)  { r.Model = model }
func (r *MistralNativeRequest) IsStream(*gin.Context) bool { return r.Stream }
func (r *MistralNativeRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{TokenType: types.TokenTypeTokenizer, CombineText: r.Text, MaxTokens: r.MaxTokens}
}

func ParseMistralNativeRequest(c *gin.Context) (*MistralNativeRequest, error) {
	r := &MistralNativeRequest{ContentType: c.GetHeader("Content-Type"), ModelField: "model"}
	if c.Request.Method == http.MethodGet {
		r.Model = c.Query("model")
		r.OriginalModel = r.Model
		r.Stream = true
		if r.Model == "" {
			return nil, fmt.Errorf("model query parameter is required")
		}
		return r, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	r.Body, err = storage.Bytes()
	if err != nil {
		return nil, err
	}
	mediaType, _, err := mime.ParseMediaType(r.ContentType)
	if err != nil {
		return nil, fmt.Errorf("invalid Content-Type: %w", err)
	}
	if mediaType == "multipart/form-data" {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		defer form.RemoveAll()
		if values := form.Value["model"]; len(values) == 1 {
			r.Model = values[0]
		}
		if values := form.Value["stream"]; len(values) > 0 {
			r.Stream, err = strconv.ParseBool(values[0])
			if err != nil {
				return nil, fmt.Errorf("stream must be a boolean")
			}
		}
	} else if mediaType == "application/json" {
		var payload struct {
			Model     string          `json:"model"`
			AgentID   string          `json:"agent_id"`
			Stream    *bool           `json:"stream"`
			MaxTokens *int            `json:"max_tokens"`
			Input     string          `json:"input"`
			Prompt    string          `json:"prompt"`
			Suffix    string          `json:"suffix"`
			Messages  []Message       `json:"messages"`
			Tools     json.RawMessage `json:"tools"`
		}
		if err := common.Unmarshal(r.Body, &payload); err != nil {
			return nil, err
		}
		r.Model = payload.Model
		if c.Request.URL.Path == "/v1/agents/completions" {
			r.ModelField, r.Model = "agent_id", payload.AgentID
		}
		if payload.Stream != nil {
			r.Stream = *payload.Stream
		}
		if payload.MaxTokens != nil {
			r.MaxTokens = *payload.MaxTokens
		}
		var text strings.Builder
		text.WriteString(payload.Input)
		text.WriteString(payload.Prompt)
		text.WriteString(payload.Suffix)
		for _, message := range payload.Messages {
			text.WriteString(message.StringContent())
		}
		text.Write(payload.Tools)
		r.Text = text.String()
	} else {
		return nil, fmt.Errorf("Mistral native requests require application/json or multipart/form-data")
	}
	if strings.TrimSpace(r.Model) == "" {
		return nil, fmt.Errorf("%s is required for channel routing", r.ModelField)
	}
	r.OriginalModel = r.Model
	return r, nil
}
