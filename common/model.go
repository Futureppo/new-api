package common

import "strings"

var (
	// OpenAIResponseOnlyModels is a list of models that are only available for OpenAI responses.
	OpenAIResponseOnlyModels = []string{
		"o3-pro",
		"o3-deep-research",
		"o4-mini-deep-research",
	}
	ImageGenerationModels = []string{
		"dall-e-3",
		"dall-e-2",
		"gpt-image-1",
		"agnes-image-",
		"prefix:imagen-",
		"flux-",
		"flux.1-",
	}
	VideoGenerationModels = []string{
		"agnes-video-",
		"grok-imagine-video",
		"sora-",
		"veo-",
		"kling-",
	}
	OpenAITextModels = []string{
		"gpt-",
		"o1",
		"o3",
		"o4",
		"chatgpt",
	}
)

func IsOpenAIResponseOnlyModel(modelName string) bool {
	for _, m := range OpenAIResponseOnlyModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}

func IsImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range ImageGenerationModels {
		if strings.Contains(modelName, m) {
			return true
		}
		if strings.HasPrefix(m, "prefix:") && strings.HasPrefix(modelName, strings.TrimPrefix(m, "prefix:")) {
			return true
		}
	}
	return false
}

func IsVideoGenerationModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range VideoGenerationModels {
		if strings.Contains(modelName, m) {
			return true
		}
		if strings.HasPrefix(m, "prefix:") && strings.HasPrefix(modelName, strings.TrimPrefix(m, "prefix:")) {
			return true
		}
	}
	return false
}

func IsOpenAITextModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	for _, m := range OpenAITextModels {
		if strings.Contains(modelName, m) {
			return true
		}
	}
	return false
}

func IsCohereRerankModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "rerank")
}

func IsCohereEmbeddingModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "embed") || strings.Contains(modelName, "embedding")
}

func IsGCPSpeechModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return modelName == "gcp-text-to-speech" ||
		strings.HasPrefix(modelName, "gcp-tts") ||
		strings.HasPrefix(modelName, "tts-")
}

func IsGCPTranscriptionModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return modelName == "gcp-speech-to-text" ||
		strings.HasPrefix(modelName, "gcp-stt") ||
		strings.HasPrefix(modelName, "whisper-")
}
