package qianwen

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const multimodalEmbeddingPath = "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"

func multimodalEmbeddingRequest(c *gin.Context, request dto.EmbeddingRequest) (any, error) {
	name := strings.ToLower(strings.TrimSpace(request.Model))
	attachment := relaycommon.PlaygroundEmbeddingImages(c)
	separate, joint, imageLimit := relaycommon.PlaygroundEmbeddingModes(constant.ChannelTypeQianwenAI, name)
	if imageLimit > 0 && len(attachment.Images) > imageLimit {
		return nil, invalid("Too many images for the selected embedding model")
	}
	texts := request.ParseInput()
	if name == "qwen2.5-vl-embedding" && len(texts) > 1 {
		return nil, invalid("qwen2.5-vl-embedding accepts up to one text input")
	}
	contents := make([]map[string]string, 0, len(texts)+len(attachment.Images))
	for _, value := range texts {
		if strings.TrimSpace(value) != "" {
			contents = append(contents, map[string]string{"text": value})
		}
	}
	for _, image := range attachment.Images {
		contents = append(contents, map[string]string{"image": "data:" + image.MimeType + ";base64," + image.Data})
	}
	if len(contents) == 0 {
		return nil, invalid("Select text or image input for multimodal embedding")
	}
	if (name == "qwen3-vl-embedding" || name == "multimodal-embedding-v1" || strings.HasSuffix(name, "-2026-03-06")) && len(contents) > 20 {
		return nil, invalid("The selected embedding model accepts up to 20 input items")
	}
	parameters := map[string]any{}
	if request.Dimensions != nil && name != "multimodal-embedding-v1" {
		parameters["dimension"] = *request.Dimensions
	}
	if separate && joint {
		// DashScope defaults to independent vectors when enable_fusion is absent.
		parameters["enable_fusion"] = attachment.Mode == "joint"
	}
	type embeddingInput struct {
		Contents []map[string]string `json:"contents"`
	}
	return struct {
		Model      string         `json:"model"`
		Input      embeddingInput `json:"input"`
		Parameters map[string]any `json:"parameters,omitempty"`
	}{Model: request.Model, Input: embeddingInput{Contents: contents}, Parameters: parameters}, nil
}

func multimodalEmbeddingResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var result struct {
		Output struct {
			Embeddings []dto.EmbeddingResponseItem `json:"embeddings"`
		} `json:"output"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			ImageTokens  int `json:"image_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if _, apiErr := readJSONResponse(resp, &result); apiErr != nil {
		return nil, apiErr
	}
	if len(result.Output.Embeddings) == 0 {
		return nil, types.NewErrorWithStatusCode(errors.New("QianwenAI returned no embeddings"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	for i := range result.Output.Embeddings {
		result.Output.Embeddings[i].Object = "embedding"
	}
	usage := dto.Usage{
		PromptTokens:     result.Usage.InputTokens + result.Usage.ImageTokens,
		CompletionTokens: result.Usage.OutputTokens,
		TotalTokens:      result.Usage.TotalTokens,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	c.JSON(http.StatusOK, dto.EmbeddingResponse{Object: "list", Model: info.OriginModelName, Data: result.Output.Embeddings, Usage: usage})
	return &usage, nil
}
