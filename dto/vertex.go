package dto

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// VertexRequest keeps publisher-specific fields intact across relay parsing.
type VertexRequest struct {
	Model  string
	Action string
	Fields map[string]json.RawMessage
}

func ParseVertexRequest(c *gin.Context) (*VertexRequest, error) {
	r := &VertexRequest{Action: vertexmodel.NativeAction(c.Request.URL.Path)}
	if r.Action == "" {
		return nil, errors.New("unsupported Vertex native endpoint")
	}
	if err := common.UnmarshalBodyReusable(c, &r.Fields); err != nil {
		return nil, err
	}
	if r.Fields == nil {
		return nil, errors.New("Vertex request must be a JSON object")
	}
	if p := strings.Index(c.Request.URL.Path, "/models/"); p >= 0 {
		r.Model = strings.SplitN(c.Request.URL.Path[p+8:], ":", 2)[0]
	} else if err := common.Unmarshal(r.Fields["model"], &r.Model); err != nil {
		return nil, errors.New("model is required")
	}
	return r, nil
}
func (r *VertexRequest) SetModelName(name string) { r.Model = name }
func (r *VertexRequest) IsStream(c *gin.Context) bool {
	var stream bool
	_ = common.Unmarshal(r.Fields["stream"], &stream)
	return strings.HasPrefix(r.Action, "stream") || stream
}
func (r *VertexRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var texts []string
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, y := range x {
				if k == "text" || k == "prompt" || k == "content" || k == "input" {
					if s, ok := y.(string); ok {
						texts = append(texts, s)
						continue
					}
				}
				if k != "data" && k != "bytesBase64Encoded" {
					visit(y)
				}
			}
		case []any:
			for _, y := range x {
				visit(y)
			}
		}
	}
	for _, raw := range r.Fields {
		var v any
		if common.Unmarshal(raw, &v) == nil {
			visit(v)
		}
	}
	for _, k := range []string{"prompt", "input"} {
		var s string
		if common.Unmarshal(r.Fields[k], &s) == nil {
			texts = append(texts, s)
		}
	}
	meta := &types.TokenCountMeta{CombineText: strings.Join(texts, "\n")}
	var config struct {
		MaxOutputTokens int `json:"maxOutputTokens"`
	}
	_ = common.Unmarshal(r.Fields["generationConfig"], &config)
	meta.MaxTokens = config.MaxOutputTokens
	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		var n int
		if common.Unmarshal(r.Fields[key], &n) == nil && n > meta.MaxTokens {
			meta.MaxTokens = n
		}
	}
	return meta
}

func (r *VertexRequest) MarshalJSON() ([]byte, error) { return common.Marshal(r.Fields) }
