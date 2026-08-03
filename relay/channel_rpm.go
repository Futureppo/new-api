package relay

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type httpStatusCodeError interface {
	HTTPStatusCode() int
}

func isRealUpstreamRPM429(resp any, err error) bool {
	if httpResp, ok := resp.(*http.Response); ok && httpResp != nil {
		return httpResp.StatusCode == http.StatusTooManyRequests
	}
	var statusErr httpStatusCodeError
	return err != nil && errors.As(err, &statusErr) && statusErr.HTTPStatusCode() == http.StatusTooManyRequests
}

func doChannelRPMGuardedRequest(c *gin.Context, info *relaycommon.RelayInfo, request func() (any, error)) (any, error) {
	result := service.TryAcquireChannelRPM(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
	if !result.Allowed {
		return nil, service.NewChannelRPMLimitError("")
	}
	resp, err := request()
	if isRealUpstreamRPM429(resp, err) {
		service.RecordChannelRPM429(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
	}
	return resp, err
}

func tryAcquireCurrentChannelRPM(c *gin.Context) *types.NewAPIError {
	channelID := c.GetInt("channel_id")
	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		channel, err = model.GetChannelById(channelID, true)
	}
	if err != nil || channel == nil {
		return nil
	}
	result := service.TryAcquireChannelRPM(c.Request.Context(), channelID, channel.GetSetting().RPMProtection)
	if !result.Allowed {
		return service.NewChannelRPMLimitError("")
	}
	return nil
}

func recordCurrentChannelRPM429(c *gin.Context, statusCode int) {
	if statusCode != http.StatusTooManyRequests {
		return
	}
	channelID := c.GetInt("channel_id")
	channel, err := model.CacheGetChannel(channelID)
	if err != nil || channel == nil {
		channel, err = model.GetChannelById(channelID, true)
	}
	if err != nil || channel == nil {
		return
	}
	service.RecordChannelRPM429(c.Request.Context(), channelID, channel.GetSetting().RPMProtection)
}

func doChannelRPMGuardedTaskRequest(c *gin.Context, info *relaycommon.RelayInfo, request func() (*http.Response, error)) (*http.Response, error) {
	result := service.TryAcquireChannelRPM(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
	if !result.Allowed {
		return nil, service.NewChannelRPMLimitError("")
	}
	resp, err := request()
	if isRealUpstreamRPM429(resp, err) {
		service.RecordChannelRPM429(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
	}
	return resp, err
}
