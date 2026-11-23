package common

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestErrorPrivacyRetainsOnlyProtocolFields(t *testing.T) {
	for _, input := range []string{
		`{"error":{"code":"invalid_api_key","message":"private detail","channel_id":42,"provider":"private","endpoint":"https://private.example","future_field":{"account":"private"}}}`,
		`{"object":"error","code":"invalid_api_key","message":"private detail","provider":"private"}`,
		`{"success":false,"code":"invalid_api_key","message":"private detail","provider":"private"}`,
		`{"errors":[{"code":"invalid_api_key","message":"private detail","provider":"private"}]}`,
	} {
		c := errorPrivacyTestContext(t, false, http.StatusOK)
		got := string(RewriteClientResponseBytes(c, []byte(input)))
		require.Contains(t, got, "invalid_api_key")
		require.NotContains(t, got, "private")
		require.NotContains(t, got, "channel_id")
		require.NotContains(t, got, "future_field")
	}
}

func TestErrorPrivacyRejectsNonCodeDetails(t *testing.T) {
	c := errorPrivacyTestContext(t, false, http.StatusBadRequest)
	for _, input := range []string{
		`{"error":{"code":{"provider":"private"},"message":"private detail"}}`,
		`{"error":{"code":"https://private.example","type":"private account disabled","message":"private detail","request_id":{"provider":"private"}}}`,
	} {
		got := string(RewriteClientResponseBytes(c, []byte(input)))
		require.NotContains(t, got, "private")
		require.Contains(t, got, "upstream_error")
	}
}

func TestErrorPrivacyBuffersSSEFrames(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for _, showDetails := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{ShowErrorDetails: showDetails})
			InstallRelayResponseWriter(c)
			c.Header("Content-Type", "text/event-stream")
			c.Header("Content-Length", "999")
			errorFrame := strings.Join([]string{"event: error", `data: {"code":"stream_failed",`, `data: "message":"private detail","provider":"private"}`, "", ""}, ending)
			successFrame := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}" + ending + ending
			input := errorFrame + successFrame
			for start := 0; start < len(input); start += 7 {
				chunk := input[start:min(start+7, len(input))]
				n, err := c.Writer.WriteString(chunk)
				require.NoError(t, err)
				require.Equal(t, len(chunk), n)
				c.Writer.Flush()
			}
			if showDetails {
				require.Equal(t, input, recorder.Body.String())
			} else {
				require.Empty(t, recorder.Result().Header.Get("Content-Length"))
				require.NotContains(t, recorder.Body.String(), "private")
				require.Contains(t, recorder.Body.String(), `"message":"stream_failed"`)
				require.Contains(t, recorder.Body.String(), successFrame)
			}
		}
	}
}

func TestErrorPrivacyKeepsResponsesFailureEnvelope(t *testing.T) {
	c := errorPrivacyTestContext(t, false, http.StatusOK)
	got := string(RewriteClientResponseBytes(c, []byte(`{"type":"response.failed","sequence_number":2,"response":{"id":"resp_123","status":"failed","error":{"code":"server_error","message":"private detail"},"provider":"private"}}`)))
	require.Contains(t, got, `"type":"response.failed"`)
	require.Contains(t, got, `"response":`)
	require.Contains(t, got, `"id":"resp_123"`)
	require.NotContains(t, got, "private")
}

func TestErrorPrivacyDoesNotSwallowHTTP400JSONWhenSSEHeaderWasPreSet(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{ShowErrorDetails: false})
	InstallRelayResponseWriter(c)

	// Simulate DoApiRequest setting SSE headers before client.Do returns HTTP 400.
	c.Header("Content-Type", "text/event-stream")
	c.JSON(http.StatusBadRequest, gin.H{
		"error": gin.H{
			"code":    "invalid_request_error",
			"message": "private upstream detail",
			"type":    "invalid_request_error",
		},
	})

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.NotEmpty(t, recorder.Body.String(), "HTTP 400 error JSON must not be trapped in ssePending")
	require.Contains(t, recorder.Body.String(), "invalid_request_error")
	require.NotContains(t, recorder.Body.String(), "private upstream detail")
}

func TestFinalizeRelayResponseFlushesTrailingSSEPending(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{ShowErrorDetails: false})
	InstallRelayResponseWriter(c)

	c.Header("Content-Type", "text/event-stream")
	_, err := c.Writer.WriteString(`data: {"error":{"code":"stream_failed","message":"private secret"}}`)
	require.NoError(t, err)
	// Still buffered because no \n\n delimiter arrived yet.
	require.Empty(t, recorder.Body.String())

	require.NoError(t, FinalizeRelayResponse(c))
	require.NotEmpty(t, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"message":"stream_failed"`)
	require.NotContains(t, recorder.Body.String(), "private secret")
}
