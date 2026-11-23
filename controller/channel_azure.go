package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type azureDiscoveryRequest struct {
	ChannelID       *int    `json:"channel_id,omitempty"`
	BaseURL         *string `json:"base_url,omitempty"`
	Key             string  `json:"key"`
	Other           *string `json:"other,omitempty"`
	Proxy           *string `json:"proxy,omitempty"`
	HeaderOverride  *string `json:"header_override,omitempty"`
	ResourceID      *string `json:"resource_id,omitempty"`
	ManagementToken string  `json:"management_token"`
	Target          string  `json:"target"`
}

// DiscoverAzureChannel uses the current draft without saving any credentials or settings.
func DiscoverAzureChannel(c *gin.Context) {
	var req azureDiscoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Azure 查询参数无效"})
		return
	}
	runAzureChannelDiscovery(c, req)
}

func DiscoverSavedAzureChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道 ID 无效"})
		return
	}
	runAzureChannelDiscovery(c, azureDiscoveryRequest{ChannelID: &id, Target: c.Query("target")})
}

func runAzureChannelDiscovery(c *gin.Context, req azureDiscoveryRequest) {
	channel := &model.Channel{Type: constant.ChannelTypeAzure}
	if req.ChannelID != nil {
		var err error
		channel, err = model.GetChannelById(*req.ChannelID, true)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "渠道不存在"})
			return
		}
		if channel.Type != constant.ChannelTypeAzure {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "仅支持 Azure 渠道"})
			return
		}
	}
	if req.BaseURL != nil {
		channel.BaseURL = req.BaseURL
	}
	if req.Other != nil {
		channel.Other = *req.Other
	}
	if req.HeaderOverride != nil {
		channel.HeaderOverride = req.HeaderOverride
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		key = channel.Key
	}
	key = strings.TrimSpace(strings.Split(strings.TrimSpace(key), "\n")[0])
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var channelSettings dto.ChannelSettings
	var otherSettings dto.ChannelOtherSettings
	if channel.Setting != nil && *channel.Setting != "" {
		if err := common.UnmarshalJsonStr(*channel.Setting, &channelSettings); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道设置格式无效"})
			return
		}
	}
	if channel.OtherSettings != "" {
		if err := common.UnmarshalJsonStr(channel.OtherSettings, &otherSettings); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "渠道其他设置格式无效"})
			return
		}
	}
	proxy := channelSettings.Proxy
	if req.Proxy != nil {
		proxy = strings.TrimSpace(*req.Proxy)
	}
	resourceID := otherSettings.AzureResourceID
	if req.ResourceID != nil {
		resourceID = *req.ResourceID
	}
	result, err := service.DiscoverAzure(c.Request.Context(), service.AzureDiscoveryOptions{
		BaseURL: channel.GetBaseURL(), Key: key, APIVersion: channel.Other,
		Proxy: proxy, Headers: headers, ResourceID: resourceID,
		ManagementToken: req.ManagementToken, Target: req.Target,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	common.ApiSuccess(c, result)
}
