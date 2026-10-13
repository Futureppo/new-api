package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service/enhancement"
	"github.com/gin-gonic/gin"
)

func enhancementSessionRiskSettings(c *gin.Context) {
	common.ApiSuccess(c, enhancement.SessionRiskSettings())
}

func enhancementSaveSessionRiskSettings(c *gin.Context) {
	var req struct {
		RecordingEnabled *bool `json:"recording_enabled"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.RecordingEnabled == nil {
		common.ApiError(c, errors.New("recording_enabled is required"))
		return
	}
	operatorID, _ := operator(c)
	data, err := enhancement.SaveSessionRiskSettings(*req.RecordingEnabled, operatorID)
	respondPublic(c, data, err)
}

func enhancementSharedSessions(c *gin.Context) {
	data, err := enhancement.SharedSessions(c.DefaultQuery("kind", "session"), ipRiskQuery(c))
	respondPublic(c, data, err)
}

func enhancementBanSharedSessionUsers(c *gin.Context) {
	var req enhancement.SessionRiskBanRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	operatorID, role := operator(c)
	data, err := enhancement.BanSharedSessionUsers(req, operatorID, role)
	respondPublic(c, data, err)
}
