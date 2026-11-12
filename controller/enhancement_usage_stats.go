package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/enhancement"
	"github.com/gin-gonic/gin"
)

func enhancementUsageStats(c *gin.Context) {
	period := c.DefaultQuery("period", "24h")
	if period != "24h" && period != "7d" && period != "30d" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid statistics period"})
		return
	}
	userID := 0
	if raw := c.Query("user_id"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user ID"})
			return
		}
		userID = parsed
	}
	data, err := enhancement.GetUsageStats(period, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}
