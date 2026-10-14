package controller

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/qianwen"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

var qianwenResponseID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,191}$`)

// Retrieval is routed by the creator's binding, never by a user-supplied channel
// or a fresh random channel selection. GET/DELETE do not consume generation quota.
func QianwenResponseResource(c *gin.Context) {
	fail := func(status int, message string) {
		c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
	}
	id := c.Param("response_id")
	if !qianwenResponseID.MatchString(id) {
		fail(http.StatusBadRequest, "invalid response_id")
		return
	}
	task, err := service.GetQianwenResponseRoute(id, common.GetContextKeyInt(c, constant.ContextKeyUserId), common.GetContextKeyInt(c, constant.ContextKeyTokenId))
	if err != nil {
		fail(http.StatusNotFound, "stored QianwenAI response not found for this token (store=true required; retention is 7 days)")
		return
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		allowed, _ := c.Get(string(constant.ContextKeyTokenModelLimit))
		limits, _ := allowed.(map[string]bool)
		if !limits[ratio_setting.FormatMatchingModelName(task.Properties.OriginModelName)] {
			fail(http.StatusForbidden, "token has no access to this response model")
			return
		}
	}
	ch, err := model.GetChannelById(task.ChannelId, true)
	if err != nil || ch.Type != constant.ChannelTypeQianwenAI || ch.Status != common.ChannelStatusEnabled {
		fail(http.StatusServiceUnavailable, "original QianwenAI channel is unavailable")
		return
	}
	base := task.PrivateData.QianwenBaseURL
	if err := qianwen.ValidateBaseURL(base); err != nil {
		fail(http.StatusBadGateway, "invalid stored upstream URL")
		return
	}
	path := "/compatible-mode/v1/responses/" + url.PathEscape(id)
	query := c.Request.URL.Query()
	inputItems := c.FullPath() == "/v1/responses/:response_id/input_items"
	if inputItems {
		path += "/input_items"
		if value := query.Get("limit"); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 100 {
				fail(http.StatusBadRequest, "limit must be between 1 and 100")
				return
			}
		}
		if value := query.Get("order"); value != "" && value != "asc" && value != "desc" {
			fail(http.StatusBadRequest, "order must be asc or desc")
			return
		}
	}
	for key := range query {
		if !inputItems || (key != "after" && key != "limit" && key != "order") {
			fail(http.StatusBadRequest, "unsupported QianwenAI response query parameter: "+key)
			return
		}
	}
	address := qianwen.NormalizeBaseURL(base) + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, address, nil)
	if err != nil {
		fail(http.StatusBadGateway, "invalid upstream request")
		return
	}
	req.Header.Set("Authorization", "Bearer "+task.PrivateData.Key)
	client, err := service.GetHttpClientWithProxy(ch.GetSetting().Proxy)
	if err != nil {
		fail(http.StatusBadGateway, "invalid channel proxy")
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		fail(http.StatusBadGateway, "QianwenAI response resource request failed")
		return
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fail(http.StatusBadGateway, "failed to read QianwenAI response")
		return
	}
	if c.Request.Method == http.MethodDelete && resp.StatusCode == http.StatusOK {
		var deleted struct {
			Deleted bool `json:"deleted"`
		}
		if common.Unmarshal(data, &deleted) == nil && deleted.Deleted {
			_ = model.DB.Where("platform = ? AND task_id = ? AND user_id = ?", service.QianwenResponsePlatform, id, task.UserId).Delete(&model.Task{}).Error
		}
	}
	service.IOCopyBytesGracefully(c, resp, data)
}
