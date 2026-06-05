package common

import "strings"

const VolcEngineArkDefaultBaseURL = "https://ark.cn-beijing.volces.com"

func GetVolcEngineArkDataPlaneBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = VolcEngineArkDefaultBaseURL
	}

	lowerBaseURL := strings.ToLower(baseURL)
	if !strings.HasPrefix(lowerBaseURL, "http://") && !strings.HasPrefix(lowerBaseURL, "https://") {
		return baseURL
	}
	if strings.HasSuffix(lowerBaseURL, "/api/v3") || strings.HasSuffix(lowerBaseURL, "/api/coding/v3") {
		return baseURL
	}
	return baseURL + "/api/v3"
}

func IsVolcEngineTextEmbeddingModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "embedding") && !strings.Contains(modelName, "embedding-vision")
}

func IsVolcEngineMultimodalEmbeddingModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "embedding-vision")
}

func IsVolcEngineEmbeddingModel(modelName string) bool {
	return IsVolcEngineTextEmbeddingModel(modelName) || IsVolcEngineMultimodalEmbeddingModel(modelName)
}

func IsVolcEngineImageGenerationModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "seedream") || strings.Contains(modelName, "seededit")
}

func IsVolcEngineVideoGenerationModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "seedance") || strings.HasPrefix(modelName, "wan2-")
}

func IsVolcEngine3DGenerationModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	return strings.Contains(modelName, "seed3d") ||
		strings.Contains(modelName, "hyper3d") ||
		strings.Contains(modelName, "hitem3d")
}

func IsVolcEngineContentGenerationTaskModel(modelName string) bool {
	return IsVolcEngineVideoGenerationModel(modelName) || IsVolcEngine3DGenerationModel(modelName)
}
