package volcengine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type volcengineMultimodalEmbeddingRequest struct {
	Model          string `json:"model"`
	Input          any    `json:"input"`
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     *int   `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

func convertVolcengineMultimodalEmbeddingRequest(request dto.EmbeddingRequest) (volcengineMultimodalEmbeddingRequest, error) {
	return volcengineMultimodalEmbeddingRequest{
		Model:          request.Model,
		Input:          normalizeVolcengineMultimodalEmbeddingInput(request.Input),
		EncodingFormat: request.EncodingFormat,
		Dimensions:     request.Dimensions,
		User:           request.User,
	}, nil
}

func normalizeVolcengineMultimodalEmbeddingInput(input any) []map[string]any {
	switch value := input.(type) {
	case nil:
		return []map[string]any{}
	case string:
		return []map[string]any{volcengineTextEmbeddingItem(value)}
	case []string:
		items := make([]map[string]any, 0, len(value))
		for _, text := range value {
			items = append(items, volcengineTextEmbeddingItem(text))
		}
		return items
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, item := range value {
			items = append(items, normalizeVolcengineMultimodalEmbeddingItem(item))
		}
		return items
	case map[string]any:
		return []map[string]any{normalizeVolcengineMultimodalEmbeddingItem(value)}
	default:
		return []map[string]any{volcengineTextEmbeddingItem(fmt.Sprintf("%v", value))}
	}
}

func normalizeVolcengineMultimodalEmbeddingItem(item any) map[string]any {
	switch value := item.(type) {
	case string:
		return volcengineTextEmbeddingItem(value)
	case map[string]any:
		return normalizeVolcengineMultimodalEmbeddingMap(value)
	default:
		var m map[string]any
		if data, err := common.Marshal(value); err == nil {
			if err := common.Unmarshal(data, &m); err == nil && len(m) > 0 {
				return normalizeVolcengineMultimodalEmbeddingMap(m)
			}
		}
		return volcengineTextEmbeddingItem(fmt.Sprintf("%v", value))
	}
}

func normalizeVolcengineMultimodalEmbeddingMap(item map[string]any) map[string]any {
	normalized := make(map[string]any, len(item)+1)
	for key, value := range item {
		normalized[key] = value
	}
	if _, ok := normalized["type"]; ok {
		return normalized
	}
	if _, ok := normalized["image_url"]; ok {
		normalized["type"] = "image_url"
		return normalized
	}
	if _, ok := normalized["video_url"]; ok {
		normalized["type"] = "video_url"
		return normalized
	}
	if _, ok := normalized["text"]; ok {
		normalized["type"] = "text"
		return normalized
	}
	return normalized
}

func volcengineTextEmbeddingItem(text string) map[string]any {
	return map[string]any{
		"type": "text",
		"text": text,
	}
}

type volcengineMultimodalEmbeddingResponse struct {
	Object string          `json:"object"`
	Data   json.RawMessage `json:"data"`
	Model  string          `json:"model"`
	Usage  dto.Usage       `json:"usage"`
	Error  any             `json:"error,omitempty"`
}

type volcengineMultimodalEmbeddingData struct {
	Object    string `json:"object,omitempty"`
	Embedding any    `json:"embedding,omitempty"`
}

func handleVolcengineMultimodalEmbeddingResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var envelope volcengineMultimodalEmbeddingResponse
	if err := common.Unmarshal(responseBody, &envelope); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := dto.GetOpenAIError(envelope.Error); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	outputBody := responseBody
	if convertedBody, ok := convertVolcengineMultimodalEmbeddingResponseBody(envelope); ok {
		outputBody = convertedBody
	}

	service.IOCopyBytesGracefully(c, resp, outputBody)

	usage := envelope.Usage
	if usage.PromptTokens == 0 {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return &usage, nil
}

func convertVolcengineMultimodalEmbeddingResponseBody(envelope volcengineMultimodalEmbeddingResponse) ([]byte, bool) {
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || data[0] != '{' {
		return nil, false
	}

	var dataObject volcengineMultimodalEmbeddingData
	if err := common.Unmarshal(data, &dataObject); err != nil || dataObject.Embedding == nil {
		return nil, false
	}

	object := strings.TrimSpace(dataObject.Object)
	if object == "" {
		object = "embedding"
	}

	modelName := envelope.Model
	converted := dto.FlexibleEmbeddingResponse{
		Object: "list",
		Data: []dto.FlexibleEmbeddingResponseItem{
			{
				Object:    object,
				Index:     0,
				Embedding: dataObject.Embedding,
			},
		},
		Model: modelName,
		Usage: envelope.Usage,
	}

	convertedBody, err := common.Marshal(converted)
	if err != nil {
		return nil, false
	}
	return convertedBody, true
}
