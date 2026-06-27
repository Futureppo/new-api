package dto

import (
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type OpenAILocalSearchRequest struct {
	Prompt string `json:"prompt"`
}

func (r *OpenAILocalSearchRequest) GetTokenCountMeta() *types.TokenCountMeta {
	return &types.TokenCountMeta{
		TokenType:   types.TokenTypeTokenizer,
		CombineText: r.Prompt,
	}
}

func (r *OpenAILocalSearchRequest) IsStream(c *gin.Context) bool {
	return false
}

func (r *OpenAILocalSearchRequest) SetModelName(modelName string) {}
