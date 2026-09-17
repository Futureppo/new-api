package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type ChannelIDRange struct {
	StartID int `json:"start_id"`
	EndID   int `json:"end_id"`
}

func DeleteChannelRange(c *gin.Context) {
	var request ChannelIDRange
	if err := c.ShouldBindJSON(&request); err != nil || !model.IsValidChannelIDRange(request.StartID, request.EndID) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "渠道 ID 范围无效"})
		return
	}
	deleted, ids, err := model.DeleteChannelsByIDRange(request.StartID, request.EndID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetChannelRPMStates(ids)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": deleted})
}
