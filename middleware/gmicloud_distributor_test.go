package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGMICloudBatchModelAliasIsCanonicalizedForDistribution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/batch/generations",
		strings.NewReader(`{"model":"gemini-batch-inference","payload":{"model":"gemini-3-flash-preview","input_data":"{}"}}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	request, shouldSelectChannel, err := getModelRequest(c)
	require.NoError(t, err)
	require.True(t, shouldSelectChannel)
	require.Equal(t, gmicloud.BatchInferenceModel, request.Model)
	require.Equal(t, relayconstant.RelayModeBatchGenerationSubmit, c.GetInt("relay_mode"))
}
