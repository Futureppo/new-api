package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/enhancement"
	"github.com/gin-gonic/gin"
)

func enhancementUsageStats(c *gin.Context) {
	userID := 0
	if raw := c.Query("user_id"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user ID"})
			return
		}
		userID = parsed
	}
	respondUsageStats(c, userID)
}

func GetSelfUsageStats(c *gin.Context) {
	userID := c.GetInt("id")
	if userID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "authentication required"})
		return
	}
	// Always use the authenticated identity, never a user_id supplied by a visitor.
	respondUsageStats(c, userID)
}

func respondUsageStats(c *gin.Context, userID int) {
	period := c.DefaultQuery("period", "24h")
	if period != "24h" && period != "7d" && period != "30d" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid statistics period"})
		return
	}
	data, err := enhancement.GetUsageStats(period, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, data)
}
