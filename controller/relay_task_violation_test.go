package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryTaskRelaySkipsViolationFee(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	shouldRetry := shouldRetryTaskRelay(ctx, 1, &dto.TaskError{
		Code:       string(types.ErrorCodeViolationFeeGrokCSAM),
		Message:    "Content violates usage guidelines",
		StatusCode: http.StatusBadRequest,
	}, 3)

	require.False(t, shouldRetry)
}
