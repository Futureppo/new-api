package cline

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

const FreeModelsPath = "/v1/ai/cline/recommended-models"

func NormalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return constant.ChannelBaseURLs[constant.ChannelTypeCline]
	}
	return strings.TrimSuffix(baseURL, "/v1")
}
