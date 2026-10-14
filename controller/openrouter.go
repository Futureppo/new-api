package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"net/http"
)

// RelayOpenRouterImages is the native JSON alias, with the same middleware and
// billing lifecycle as the existing image generation route.
func RelayOpenRouterImages(c *gin.Context) {
	if common.GetContextKeyInt(c, constant.ContextKeyChannelType) != constant.ChannelTypeOpenRouter {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": "/v1/images requires an OpenRouter channel"}})
		return
	}
	Relay(c, types.RelayFormatOpenAIImage)
}
