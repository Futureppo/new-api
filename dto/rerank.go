package dto

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type RerankRequest struct {
	// Multimodal providers accept a query object. Query remains the text field
	// used by existing providers and local token estimation.
	QueryObject json.RawMessage `json:"-"`
	Instruct    string          `json:"instruct,omitempty"`
	FPS         *float64        `json:"fps,omitempty"`
	// OpenRouter routing and observability extensions.
	Provider  json.RawMessage `json:"provider,omitempty"`
	SessionID json.RawMessage `json:"session_id,omitempty"`
	Trace     json.RawMessage `json:"trace,omitempty"`
	User      json.RawMessage `json:"user,omitempty"`

	Documents       []any  `json:"documents"`
	Query           string `json:"query"`
	Model           string `json:"model"`
	TopN            *int   `json:"top_n,omitempty"`
	ReturnDocuments *bool  `json:"return_documents,omitempty"`
	MaxChunkPerDoc  *int   `json:"max_chunk_per_doc,omitempty"`
	OverLapTokens   *int   `json:"overlap_tokens,omitempty"`
}

func (r *RerankRequest) UnmarshalJSON(data []byte) error {
	type alias RerankRequest
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	query := fields["query"]
	if len(query) == 0 || common.GetJsonType(query) != "object" {
		return common.Unmarshal(data, (*alias)(r))
	}
	delete(fields, "query")
	rest, err := common.Marshal(fields)
	if err != nil {
		return err
	}
	var parsed alias
	if err := common.Unmarshal(rest, &parsed); err != nil {
		return err
	}
	*r = RerankRequest(parsed)
	r.QueryObject = query
	return nil
}

func (r RerankRequest) MarshalJSON() ([]byte, error) {
	type alias RerankRequest
	if len(r.QueryObject) == 0 {
		return common.Marshal(alias(r))
	}
	return common.Marshal(struct {
		alias
		Query json.RawMessage `json:"query"`
	}{alias(r), r.QueryObject})
}

func (r *RerankRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *RerankRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var texts []string
	var files []*types.FileMeta
	addMedia := func(item map[string]any) bool {
		text, hasText := item["text"].(string)
		if hasText {
			texts = append(texts, text)
		}
		for _, media := range []struct {
			key  string
			kind types.FileType
		}{{"image", types.FileTypeImage}, {"video", types.FileTypeVideo}} {
			if url, ok := item[media.key].(string); ok && url != "" {
				files = append(files, types.NewFileMeta(media.kind, types.NewFileSourceFromData(url, "")))
				hasText = true
			}
		}
		return hasText
	}

	for _, document := range r.Documents {
		if item, ok := document.(map[string]any); ok && addMedia(item) {
			continue
		}
		texts = append(texts, fmt.Sprintf("%v", document))
	}

	if r.Query != "" {
		texts = append(texts, r.Query)
	} else if r.QueryObject != nil {
		var query map[string]any
		if common.Unmarshal(r.QueryObject, &query) == nil {
			addMedia(query)
		}
	}

	return &types.TokenCountMeta{
		CombineText: strings.Join(texts, "\n"),
		Files:       files,
	}
}

func (r *RerankRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *RerankRequest) GetReturnDocuments() bool {
	if r.ReturnDocuments == nil {
		return false
	}
	return *r.ReturnDocuments
}

type RerankResponseResult struct {
	Document       any     `json:"document,omitempty"`
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type RerankDocument struct {
	Text any `json:"text"`
}

type RerankResponse struct {
	Results []RerankResponseResult `json:"results"`
	Usage   Usage                  `json:"usage"`
}
