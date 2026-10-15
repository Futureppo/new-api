package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	taskopenrouter "github.com/QuantumNous/new-api/relay/channel/task/openrouter"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func respondOpenRouterVideoTask(c *gin.Context, task *model.Task) {
	adaptor := &taskopenrouter.TaskAdaptor{}
	ch, err := model.GetChannelById(task.ChannelId, true)
	if err == nil {
		// Apply terminal submit responses through the same CAS/refund flow as polling.
		err = service.ApplyTaskPollingResult(c.Request.Context(), adaptor, ch, task, task.Data, http.StatusOK)
	}
	if err != nil {
		common.SysError("OpenRouter video initial status: " + err.Error())
	}
	body, err := adaptor.ConvertToOpenAIVideo(task)
	if err != nil {
		videoProxyError(c, 502, "invalid_response", err.Error())
		return
	}
	c.Data(http.StatusAccepted, "application/json", body)
}
