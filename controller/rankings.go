package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRankings(c *gin.Context) {
	period := c.DefaultQuery("period", "week")
	if period != "today" && period != "week" && period != "month" && period != "year" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid ranking period"})
		return
	}
	data, err := service.GetRankingsSnapshot(period)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "rankings temporarily unavailable"})
		return
	}
	// This public payload contains model/vendor aggregates only, never users,
	// balances, channel names, credentials or individual request records.
	common.ApiSuccess(c, data)
}
