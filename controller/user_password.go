package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func respondPasswordError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, common.ErrPasswordPolicy), errors.Is(err, service.ErrOriginalPassword),
		errors.Is(err, service.ErrPasswordUnchanged), errors.Is(err, service.ErrPasswordConflict):
		common.ApiErrorI18n(c, err.Error())
	default:
		common.ApiError(c, err)
	}
}

func UpdateSelfPassword(c *gin.Context) {
	if c.GetBool("use_access_token") || sessions.Default(c).Get("id") != c.GetInt("id") {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgUnauthorized)})
		return
	}
	var request struct {
		Password         string `json:"password"`
		OriginalPassword string `json:"original_password"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := service.SetUserLoginPassword(c.GetInt("id"), request.OriginalPassword, request.Password); err != nil {
		respondPasswordError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"has_password": true})
}
