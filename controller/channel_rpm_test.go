package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipRPMLimitedChannelOnlyForRoutableRequests(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	channel := &model.Channel{Id: 930001}
	require.True(t, shouldSkipRPMLimitedChannel(c, channel))
	require.True(t, service.IsChannelRPMLimitSkipped(c, channel.Id))

	specific, _ := gin.CreateTestContext(httptest.NewRecorder())
	specific.Set(string(constant.ContextKeyTokenSpecificChannelId), channel.Id)
	require.False(t, shouldSkipRPMLimitedChannel(specific, channel))
	require.False(t, service.IsChannelRPMLimitSkipped(specific, channel.Id))
}

func TestRespondTaskErrorPreservesChannelRPMError(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	taskErr := &dto.TaskError{
		Code:       string(types.ErrorCodeChannelRPMLimitExceeded),
		Message:    service.ChannelRPMLimitExceededMessage,
		StatusCode: http.StatusTooManyRequests,
		LocalError: true,
	}

	respondTaskError(c, taskErr)
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	var response dto.TaskError
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, string(types.ErrorCodeChannelRPMLimitExceeded), response.Code)
	require.Equal(t, service.ChannelRPMLimitExceededMessage, response.Message)
}

func TestMidjourneyBoundChannelRPMErrorDoesNotReroute(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("channel_rpm_locked", true)
	attempts := 0

	result := relayMidjourneyWithRPMFallback(c, &relaycommon.RelayInfo{}, func() *dto.MidjourneyResponse {
		attempts++
		return service.MidjourneyErrorWrapper(constant.MjRequestError, service.ChannelRPMLimitExceededMessage)
	})

	require.Equal(t, 1, attempts)
	require.NotNil(t, result)
	require.Equal(t, service.ChannelRPMLimitExceededMessage, result.Description)
}
