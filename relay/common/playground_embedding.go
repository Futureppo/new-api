package common

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/gin-gonic/gin"
)

type EmbeddingImageFormat string

const (
	EmbeddingImageArk        EmbeddingImageFormat = "ark"
	EmbeddingImageJina       EmbeddingImageFormat = "jina"
	EmbeddingImageOpenRouter EmbeddingImageFormat = "openrouter"
	EmbeddingImageCohere     EmbeddingImageFormat = "cohere"
	EmbeddingImageGemini     EmbeddingImageFormat = "gemini"
	EmbeddingImageDashScope  EmbeddingImageFormat = "dashscope"
)

func PlaygroundEmbeddingImageFormat(channelType int, name string) EmbeddingImageFormat {
	name = strings.ToLower(strings.TrimSpace(name))
	base := strings.SplitN(name[strings.LastIndexByte(name, '/')+1:], ":", 2)[0]
	jina := strings.HasPrefix(base, "jina-clip-") || base == "jina-embeddings-v4" || strings.HasPrefix(base, "jina-embeddings-v5-omni-")
	switch channelType {
	case constant.ChannelTypeQianwenAI:
		if name == "qwen3-vl-embedding" || name == "qwen2.5-vl-embedding" || name == "multimodal-embedding-v1" ||
			strings.HasPrefix(name, "tongyi-embedding-vision-") {
			return EmbeddingImageDashScope
		}
	case constant.ChannelTypeVolcEngine:
		if common.IsVolcEngineMultimodalEmbeddingModel(name) {
			return EmbeddingImageArk
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeOpenAILocal, constant.ChannelTypeJina:
		if jina {
			return EmbeddingImageJina
		}
	case constant.ChannelTypeOpenRouter:
		if strings.Contains(name, "/gemini-embedding-2") || strings.Contains(name, "-embed-vl-") ||
			strings.Contains(name, "/qwen3-vl-embedding") || strings.Contains(name, "/embed-v4") ||
			strings.Contains(name, "/embed-v5") || strings.HasPrefix(base, "voyage-multimodal-3.5") ||
			strings.HasPrefix(name, "cohere/embed-5-") || jina {
			return EmbeddingImageOpenRouter
		}
	case constant.ChannelTypeCohere:
		if strings.HasPrefix(name, "embed-v4") || strings.HasPrefix(name, "embed-v5") {
			return EmbeddingImageCohere
		}
	case constant.ChannelTypeGemini:
		if strings.HasPrefix(name, "gemini-embedding-2") {
			return EmbeddingImageGemini
		}
	case constant.ChannelTypeVertexAi:
		capability, err := vertexmodel.Resolve(name)
		if err == nil && (capability.Action == "embedContent" || capability.Family == vertexmodel.MultimodalEmbedding) {
			return EmbeddingImageGemini
		}
	}
	return ""
}

func PlaygroundEmbeddingModes(channelType int, name string) (separate, joint bool, limit int) {
	switch PlaygroundEmbeddingImageFormat(channelType, name) {
	case EmbeddingImageJina:
		return true, false, 0
	case EmbeddingImageArk:
		return false, true, 0
	case EmbeddingImageCohere:
		return true, true, 96
	case EmbeddingImageGemini:
		if channelType == constant.ChannelTypeVertexAi {
			if capability, err := vertexmodel.Resolve(name); err == nil && capability.Family == vertexmodel.MultimodalEmbedding {
				return true, false, 0
			}
		}
		return true, true, 0
	case EmbeddingImageOpenRouter:
		return true, true, 0
	case EmbeddingImageDashScope:
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "qwen3-vl-embedding":
			return true, true, 10
		case "qwen2.5-vl-embedding":
			return false, true, 1
		case "multimodal-embedding-v1":
			return true, false, 1
		default:
			return true, false, 0
		}
	}
	return false, false, 0
}

type PlaygroundEmbeddingAttachment struct {
	Images []*dto.GeminiInlineData
	Mode   string
}

func PlaygroundEmbeddingImages(c *gin.Context) PlaygroundEmbeddingAttachment {
	if c == nil {
		return PlaygroundEmbeddingAttachment{}
	}
	images, _ := common.GetContextKeyType[PlaygroundEmbeddingAttachment](c, constant.ContextKeyPlaygroundEmbeddingImage)
	return images
}

type PlaygroundEmbeddingInput struct {
	Texts  []string
	Images []*dto.GeminiInlineData
}

func (a PlaygroundEmbeddingAttachment) Inputs(texts []string, separate bool) []PlaygroundEmbeddingInput {
	if !separate {
		return []PlaygroundEmbeddingInput{{Texts: texts, Images: a.Images}}
	}
	inputs := make([]PlaygroundEmbeddingInput, 0, len(texts)+len(a.Images))
	for _, text := range texts {
		if strings.TrimSpace(text) != "" {
			inputs = append(inputs, PlaygroundEmbeddingInput{Texts: []string{text}})
		}
	}
	for _, image := range a.Images {
		inputs = append(inputs, PlaygroundEmbeddingInput{Images: []*dto.GeminiInlineData{image}})
	}
	return inputs
}

func (input PlaygroundEmbeddingInput) MediaParts() []dto.MediaContent {
	parts := make([]dto.MediaContent, 0, len(input.Texts)+len(input.Images))
	for _, text := range input.Texts {
		if strings.TrimSpace(text) != "" {
			parts = append(parts, dto.MediaContent{Type: "text", Text: text})
		}
	}
	for _, image := range input.Images {
		url := "data:" + image.MimeType + ";base64," + image.Data
		parts = append(parts, dto.MediaContent{Type: "image_url", ImageUrl: &dto.MessageImageUrl{Url: url}})
	}
	return parts
}

func (input PlaygroundEmbeddingInput) GeminiContent() dto.GeminiChatContent {
	parts := make([]dto.GeminiPart, 0, len(input.Texts)+len(input.Images))
	for _, text := range input.Texts {
		if strings.TrimSpace(text) != "" {
			parts = append(parts, dto.GeminiPart{Text: text})
		}
	}
	for _, image := range input.Images {
		parts = append(parts, dto.GeminiPart{InlineData: image})
	}
	return dto.GeminiChatContent{Parts: parts}
}
