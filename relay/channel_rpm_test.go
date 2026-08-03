package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type channelRPMStatusError struct {
	statusCode int
}

func (e channelRPMStatusError) Error() string {
	return "upstream request failed"
}

func (e channelRPMStatusError) HTTPStatusCode() int {
	return e.statusCode
}

func channelRPMTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func channelRPMTestInfo(channelID, limit int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId: channelID,
		ChannelSetting: dto.ChannelSettings{RPMProtection: &dto.ChannelRPMProtectionSettings{
			Enabled:                    true,
			RPMLimit:                   limit,
			ProtectionThresholdPercent: 50,
			RampMinutes:                5,
		}},
	}}
}

func withMemoryChannelRPM(t *testing.T, channelID int) {
	t.Helper()
	oldEnabled := common.RedisEnabled
	common.RedisEnabled = false
	service.ResetChannelRPMState(channelID)
	t.Cleanup(func() {
		service.ResetChannelRPMState(channelID)
		common.RedisEnabled = oldEnabled
	})
}

func TestChannelRPMGuardUsesOriginalUpstream429(t *testing.T) {
	const channelID = 920001
	withMemoryChannelRPM(t, channelID)
	c := channelRPMTestContext()
	info := channelRPMTestInfo(channelID, 2)

	_, err := doChannelRPMGuardedRequest(c, info, func() (any, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests}, nil
	})
	require.NoError(t, err)

	called := false
	_, err = doChannelRPMGuardedRequest(c, info, func() (any, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	require.False(t, called)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.True(t, service.IsChannelRPMLimitError(apiErr))
}

func TestChannelRPMGuardIgnoresNon429BeforeLocalStatusMapping(t *testing.T) {
	const channelID = 920002
	withMemoryChannelRPM(t, channelID)
	c := channelRPMTestContext()
	info := channelRPMTestInfo(channelID, 2)

	_, err := doChannelRPMGuardedRequest(c, info, func() (any, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError}, nil
	})
	require.NoError(t, err)

	called := false
	_, err = doChannelRPMGuardedRequest(c, info, func() (any, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	require.NoError(t, err)
	require.True(t, called, "a non-429 upstream response must not activate dynamic protection")
}

func TestChannelRPMGuardUsesStatusFromUpstreamError(t *testing.T) {
	const channelID = 920003
	withMemoryChannelRPM(t, channelID)
	c := channelRPMTestContext()
	info := channelRPMTestInfo(channelID, 2)

	_, err := doChannelRPMGuardedRequest(c, info, func() (any, error) {
		return nil, fmt.Errorf("wrapped: %w", channelRPMStatusError{statusCode: http.StatusTooManyRequests})
	})
	require.Error(t, err)

	called := false
	_, err = doChannelRPMGuardedRequest(c, info, func() (any, error) {
		called = true
		return &http.Response{StatusCode: http.StatusOK}, nil
	})
	require.False(t, called)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.True(t, service.IsChannelRPMLimitError(apiErr))
}
