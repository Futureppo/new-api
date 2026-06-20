package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func SendEmailWithLog(c *gin.Context, userId int, source string, subject string, receiver string, content string) error {
	err := common.SendEmail(subject, receiver, content)
	params := model.RecordEmailLogParams{
		Receiver: receiver,
		Subject:  subject,
		Source:   source,
		Success:  err == nil,
	}
	if err != nil {
		params.Error = err.Error()
	}
	model.RecordEmailLog(c, userId, params)
	return err
}
