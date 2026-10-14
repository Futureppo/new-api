package vertex

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type embeddingBatch struct {
	Requests []map[string]json.RawMessage `json:"requests"`
}
type embeddingResult struct {
	Embedding     dto.ContentEmbedding    `json:"embedding"`
	UsageMetadata dto.GeminiUsageMetadata `json:"usageMetadata"`
	Truncated     *bool                   `json:"truncated,omitempty"`
}
type embeddingResults struct {
	Results []embeddingResult `json:"results"`
}

func raw(v any) json.RawMessage { b, _ := common.Marshal(v); return b }

func (a *Adaptor) convertEmbedding(c *gin.Context, info *relaycommon.RelayInfo, r dto.EmbeddingRequest) (any, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	if a.capability.Family != vertexmodel.Embedding && a.capability.Family != vertexmodel.MultimodalEmbedding {
		return nil, errors.New("use a text embedding model, or the native API for multimodal embeddings")
	}
	if a.capability.Family == vertexmodel.MultimodalEmbedding && !info.PriceData.UsePrice {
		return nil, errors.New("multimodalembedding requires per-request pricing")
	}
	var inputs []string
	switch v := r.Input.(type) {
	case string:
		inputs = []string{v}
	case []string:
		inputs = v
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("Vertex embedding input must contain only strings")
			}
			inputs = append(inputs, s)
		}
	default:
		return nil, errors.New("Vertex embedding input must be a string or array of strings")
	}
	if len(inputs) == 0 {
		return nil, errors.New("input is empty")
	}
	if r.EncodingFormat != "" && r.EncodingFormat != "float" && r.EncodingFormat != "base64" {
		return nil, errors.New("encoding_format must be float or base64")
	}
	if r.Dimensions != nil && *r.Dimensions <= 0 {
		return nil, errors.New("dimensions must be positive")
	}
	a.embeddingFormat = r.EncodingFormat
	a.responseKind = "embedding"
	a.action = a.capability.Action
	config := map[string]any{}
	if r.Dimensions != nil {
		config["outputDimensionality"] = *r.Dimensions
	}
	if r.TaskType != "" {
		config["taskType"] = r.TaskType
	} else if r.InputType != "" {
		task := r.InputType
		switch task {
		case "search_query":
			task = "RETRIEVAL_QUERY"
		case "search_document":
			task = "RETRIEVAL_DOCUMENT"
		}
		config["taskType"] = task
	}
	if r.Title != "" {
		config["title"] = r.Title
	}
	if r.AutoTruncate != nil {
		config["autoTruncate"] = *r.AutoTruncate
	} else if r.Truncate != "" {
		switch strings.ToUpper(r.Truncate) {
		case "NONE":
			config["autoTruncate"] = false
		case "END":
			config["autoTruncate"] = true
		default:
			return nil, errors.New("Vertex truncate must be NONE or END")
		}
	}
	batch := embeddingBatch{}
	for _, text := range inputs {
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("embedding input must not be empty")
		}
		batch.Requests = append(batch.Requests, map[string]json.RawMessage{"content": raw(dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: text}}}), "embedContentConfig": raw(config)})
	}
	return batch, nil
}

func (a *Adaptor) embeddingPayload(req map[string]json.RawMessage) (any, error) {
	delete(req, "model")
	config := map[string]json.RawMessage{}
	if data, ok := req["embedContentConfig"]; ok {
		if err := common.Unmarshal(data, &config); err != nil {
			return nil, err
		}
	}
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	for _, key := range []string{"taskType", "title", "outputDimensionality", "autoTruncate"} {
		if value, ok := req[key]; ok {
			if _, exists := config[key]; exists {
				return nil, fmt.Errorf("duplicate embedding configuration %s", key)
			}
			config[key] = value
			delete(req, key)
		}
	}
	if a.capability.Action == "embedContent" {
		req["embedContentConfig"] = raw(config)
		return req, nil
	}
	var content dto.GeminiChatContent
	if err := common.Unmarshal(req["content"], &content); err != nil {
		return nil, err
	}
	texts := []string{}
	for _, part := range content.Parts {
		if part.InlineData != nil || part.FileData != nil {
			return nil, errors.New("this Vertex embedding model only accepts text")
		}
		texts = append(texts, part.Text)
	}
	if strings.TrimSpace(strings.Join(texts, "")) == "" {
		return nil, errors.New("embedding content is empty")
	}
	if a.capability.Family == vertexmodel.MultimodalEmbedding {
		parameters := map[string]json.RawMessage{}
		for k, v := range config {
			if k != "outputDimensionality" {
				return nil, fmt.Errorf("multimodalembedding does not support %s", k)
			}
			parameters["dimension"] = v
		}
		return map[string]any{"instances": []any{map[string]any{"text": strings.Join(texts, "\n")}}, "parameters": parameters}, nil
	}
	instance := map[string]any{"content": strings.Join(texts, "\n")}
	parameters := map[string]json.RawMessage{}
	for k, v := range config {
		switch k {
		case "title":
			instance[k] = v
		case "taskType":
			instance["task_type"] = v
		case "outputDimensionality", "autoTruncate":
			parameters[k] = v
		default:
			return nil, fmt.Errorf("embedding configuration %s is not supported by this model", k)
		}
	}
	return map[string]any{"instances": []any{instance}, "parameters": parameters}, nil
}

// Each request contains one input, which also works for gemini-embedding-001.
// Responses are buffered until the whole batch succeeds; callers never receive
// an incomplete vector list or retry already billed successful subrequests.
func (a *Adaptor) doEmbeddings(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	var batch embeddingBatch
	if err := common.DecodeJson(body, &batch); err != nil {
		return nil, err
	}
	if len(batch.Requests) == 0 {
		return nil, errors.New("embedding requests must not be empty")
	}
	payloads := make([][]byte, len(batch.Requests))
	for i, item := range batch.Requests {
		payload, err := a.embeddingPayload(item)
		if err != nil {
			return nil, err
		}
		payloads[i], err = common.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	results := embeddingResults{}
	if len(payloads) > 1 {
		c.Set("vertex_batch_started", true)
	}
	for i, payload := range payloads {
		if err := c.Request.Context().Err(); err != nil {
			return nil, err
		}
		if i > 0 {
			if !service.TryAcquireChannelRPM(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection).Allowed {
				return nil, errors.New("Vertex embedding batch interrupted by channel RPM protection")
			}
		}
		resp, err := channel.DoApiRequest(a, c, info, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return resp, nil
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		var result embeddingResult
		if a.capability.Family == vertexmodel.MultimodalEmbedding {
			var prediction struct {
				Predictions []struct {
					TextEmbedding []float64 `json:"textEmbedding"`
				} `json:"predictions"`
			}
			if err := common.Unmarshal(data, &prediction); err != nil {
				return nil, err
			}
			if len(prediction.Predictions) != 1 {
				return nil, errors.New("invalid multimodal embedding count")
			}
			result.Embedding.Values = prediction.Predictions[0].TextEmbedding
		} else if a.capability.Action == "predict" {
			var prediction struct {
				Predictions []struct {
					Embeddings struct {
						Values     []float64 `json:"values"`
						Statistics struct {
							TokenCount int   `json:"token_count"`
							Truncated  *bool `json:"truncated"`
						} `json:"statistics"`
					} `json:"embeddings"`
				} `json:"predictions"`
			}
			if err := common.Unmarshal(data, &prediction); err != nil {
				return nil, err
			}
			if len(prediction.Predictions) != 1 {
				return nil, errors.New("invalid Vertex embedding prediction count")
			}
			p := prediction.Predictions[0].Embeddings
			result.Embedding.Values = p.Values
			result.Truncated = p.Statistics.Truncated
			result.UsageMetadata.PromptTokenCount = p.Statistics.TokenCount
			result.UsageMetadata.TotalTokenCount = p.Statistics.TokenCount
		} else {
			if err := common.Unmarshal(data, &result); err != nil {
				return nil, err
			}
		}
		if len(result.Embedding.Values) == 0 {
			return nil, errors.New("Vertex returned an empty embedding")
		}
		results.Results = append(results.Results, result)
	}
	data, err := common.Marshal(results)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func (a *Adaptor) embeddingResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	var results embeddingResults
	if err := common.DecodeJson(resp.Body, &results); err != nil {
		return nil, badResponse(err)
	}
	usage := &dto.Usage{}
	for _, r := range results.Results {
		u := gemini.UsageFromMetadata(r.UsageMetadata)
		usage.PromptTokens += u.PromptTokens
		usage.TotalTokens += u.TotalTokens
		usage.PromptTokensDetails.TextTokens += u.PromptTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens += u.PromptTokensDetails.AudioTokens
		usage.PromptTokensDetails.ImageTokens += u.PromptTokensDetails.ImageTokens
	}
	if a.capability.Family == vertexmodel.MultimodalEmbedding {
		info.NonTokenUsage = true
		info.PriceData.AddOtherRatio("n", float64(len(results.Results)))
	}
	if a.embeddingNativeAction != "" {
		if a.embeddingNativeAction == "embedContent" {
			if len(results.Results) != 1 {
				return nil, badResponse(errors.New("invalid embedding count"))
			}
			c.JSON(http.StatusOK, results.Results[0])
		} else {
			vectors := []dto.ContentEmbedding{}
			for _, r := range results.Results {
				vectors = append(vectors, r.Embedding)
			}
			c.JSON(http.StatusOK, gin.H{"embeddings": vectors, "usageMetadata": gin.H{"promptTokenCount": usage.PromptTokens, "totalTokenCount": usage.TotalTokens}})
		}
	} else {
		items := make([]gin.H, 0, len(results.Results))
		for i, r := range results.Results {
			var embedding any = r.Embedding.Values
			if a.embeddingFormat == "base64" {
				buf := make([]byte, 4*len(r.Embedding.Values))
				for j, v := range r.Embedding.Values {
					binary.LittleEndian.PutUint32(buf[j*4:], math.Float32bits(float32(v)))
				}
				embedding = base64.StdEncoding.EncodeToString(buf)
			}
			items = append(items, gin.H{"object": "embedding", "index": i, "embedding": embedding})
		}
		c.JSON(http.StatusOK, gin.H{"object": "list", "model": info.OriginModelName, "data": items, "usage": usage})
	}
	return usage, nil
}
