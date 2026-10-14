package controller

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func VertexOperationFetch(c *gin.Context) {
	var request struct {
		OperationName string `json:"operationName"`
	}
	if err := common.UnmarshalBodyReusable(c, &request); err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	id, ok := strings.CutPrefix(request.OperationName, "operations/")
	if !ok || id == "" || strings.Contains(id, "/") {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "operationName must be operations/{public task ID}")
		return
	}
	task, exists, err := model.GetByTaskId(c.GetInt("id"), id)
	if err != nil {
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Unable to query operation")
		return
	}
	if !exists || task == nil || task.Platform != constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeVertexAi)) {
		videoProxyError(c, http.StatusNotFound, "not_found", "Operation not found")
		return
	}
	ch, err := model.CacheGetChannel(task.ChannelId)
	if err != nil || ch.Type != constant.ChannelTypeVertexAi {
		videoProxyError(c, http.StatusBadGateway, "server_error", "Operation channel unavailable")
		return
	}
	adaptor := relay.GetTaskAdaptor(task.Platform)
	if err := service.RefreshVideoTask(c.Request.Context(), adaptor, ch, task); err != nil {
		videoProxyError(c, http.StatusBadGateway, "upstream_error", "Unable to refresh Vertex operation")
		return
	}
	payload := map[string]json.RawMessage{}
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		videoProxyError(c, http.StatusBadGateway, "upstream_error", "Invalid stored Vertex operation")
		return
	}
	name, _ := common.Marshal("operations/" + task.TaskID)
	payload["name"] = name
	done := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	payload["done"], _ = common.Marshal(done)
	if task.Status == model.TaskStatusFailure {
		delete(payload, "response")
		payload["error"], _ = common.Marshal(gin.H{"code": 13, "message": task.FailReason})
	}
	c.JSON(http.StatusOK, payload)
}
