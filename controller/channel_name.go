package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func DeleteChannelByName(c *gin.Context) {
	var request struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.Name) == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "渠道名称不能为空"})
		return
	}
	deleted, ids, err := model.DeleteChannelsByExactName(request.Name)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	service.ResetChannelRPMStates(ids)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": deleted})
}
