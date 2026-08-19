package common

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func activeFullMappingInfo() *RelayInfo {
	return &RelayInfo{
		ClientModelName:        "request-alias",
		ModelMappingTargetName: "upstream-model-2026",
		ChannelMeta: &ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{ModelMappingFullEnabled: true},
			UpstreamModelName: "upstream-model-2026",
			IsModelMapped:     true,
		},
	}
}

func TestRewriteModelMappingBytesJSONMetadataOnly(t *testing.T) {
	input := []byte(`{
		"model":"upstream-model-2026",
		"response":{"model_id":"upstream-model-2026","modelName":"upstream-model-2026"},
		"provider":{"modelVersion":"upstream-model-2026","originModelName":"upstream-model-2026"},
		"deep":{"nested":{"provider":{"model":"provider-resolved-model"}}},
		"choices":[{"message":{"content":"upstream-model-2026 is discussed here","tool_calls":[{"function":{"arguments":"{\"model\":\"upstream-model-2026\"}"}}]}}],
		"precise":123456789012345678901234567890
	}`)

	got := RewriteModelMappingBytes(input, "request-alias", []string{"upstream-model-2026"}, false)

	require.Contains(t, string(got), `"model":"request-alias"`)
	require.Contains(t, string(got), `"model_id":"request-alias"`)
	require.Contains(t, string(got), `"modelName":"request-alias"`)
	require.Contains(t, string(got), `"modelVersion":"request-alias"`)
	require.Contains(t, string(got), `"originModelName":"request-alias"`)
	require.Contains(t, string(got), `"provider":{"model":"request-alias"}`)
	require.Contains(t, string(got), `"content":"upstream-model-2026 is discussed here"`)
	require.Contains(t, string(got), `"arguments":"{\"model\":\"upstream-model-2026\"}"`)
	require.Contains(t, string(got), `123456789012345678901234567890`)
}

func TestRewriteModelMappingBytesSSEAndErrors(t *testing.T) {
	input := []byte("event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"response\":{\"model\":\"upstream-model-2026\"},\"delta\":\"upstream-model-2026\"}\n\n" +
		"data: {\"type\":\"error\",\"error\":{\"message\":\"upstream-model-2026 is unavailable\",\"modelVersion\":\"upstream-model-2026\"}}\n\n" +
		"data: [DONE]\n\n")

	got := RewriteModelMappingBytes(input, "request-alias", []string{"upstream-model-2026"}, false)

	require.Contains(t, string(got), `"model":"request-alias"`)
	require.Contains(t, string(got), `"delta":"upstream-model-2026"`)
	require.Contains(t, string(got), `"message":"request-alias is unavailable"`)
	require.Contains(t, string(got), `"modelVersion":"request-alias"`)
	require.Contains(t, string(got), "data: [DONE]")
}

func TestRewriteModelMappingBytesNonJSONUnchanged(t *testing.T) {
	input := []byte{0x00, 0x01, 0xff, 'u', 'p', 's', 't', 'r', 'e', 'a', 'm'}
	got := RewriteModelMappingBytes(input, "request-alias", []string{"upstream"}, false)
	require.True(t, bytes.Equal(input, got))
}

func TestRewriteModelMappingBytesIgnoresEmptyHiddenModel(t *testing.T) {
	input := []byte(`{"error":{"message":"plain failure"},"model":"upstream-model"}`)
	got := RewriteModelMappingBytes(input, "request-alias", []string{"", "upstream-model"}, false)

	require.Contains(t, string(got), `"message":"plain failure"`)
	require.Contains(t, string(got), `"model":"request-alias"`)
}

func TestRewriteClientResponseBytesRequiresActiveMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := activeFullMappingInfo()
	info.ChannelSetting.ModelMappingFullEnabled = false
	SetRelayInfo(c, info)
	input := []byte(`{"model":"upstream-model-2026"}`)

	require.Equal(t, input, RewriteClientResponseBytes(c, input))

	info.ChannelSetting.ModelMappingFullEnabled = true
	info.ModelMappingBypassed = true
	require.Equal(t, input, RewriteClientResponseBytes(c, input))
}

func TestModelMappingResponseWriterRewritesAndRemovesContentLength(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	SetRelayInfo(c, activeFullMappingInfo())
	InstallModelMappingResponseWriter(c)
	c.Header("Content-Length", "35")
	c.Data(http.StatusOK, "application/json", []byte(`{"model":"upstream-model-2026"}`))

	require.Equal(t, `{"model":"request-alias"}`, recorder.Body.String())
	require.Empty(t, recorder.Header().Get("Content-Length"))
}

func errorPrivacyTestContext(t *testing.T, showDetails bool, statusCode int) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{ShowErrorDetails: showDetails})
	if statusCode != 0 {
		c.Status(statusCode)
	}
	return c
}

func TestRewriteClientResponseBytesHidesProtocolErrorDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		input    string
		contains []string
		excludes []string
	}{
		{
			name:     "openai",
			input:    `{"error":{"message":"invalid key for https://provider.example","type":"invalid_request_error","param":"model","code":"invalid_api_key","metadata":{"provider":"secret"},"details":["secret"]}}`,
			contains: []string{`"message":"invalid_api_key"`, `"type":"upstream_error"`, `"param":""`, `"code":"invalid_api_key"`},
			excludes: []string{"provider.example", "metadata", "details", "secret"},
		},
		{
			name:     "claude",
			input:    `{"type":"error","error":{"type":"authentication_error","message":"anthropic account rejected"}}`,
			contains: []string{`"type":"authentication_error"`, `"message":"authentication_error"`},
			excludes: []string{"anthropic account"},
		},
		{
			name:     "gemini",
			input:    `{"error":{"code":400,"message":"vertex project secret","status":"INVALID_ARGUMENT","details":[{"reason":"secret"}]}}`,
			contains: []string{`"code":400`, `"message":"400"`, `"status":"INVALID_ARGUMENT"`},
			excludes: []string{"vertex project", "details", "reason"},
		},
		{
			name:     "task",
			input:    `{"code":"build_request_failed","message":"provider payload rejected","data":{"body":"secret"}}`,
			contains: []string{`"code":"build_request_failed"`, `"message":"build_request_failed"`},
			excludes: []string{"provider payload", `"data"`, "secret"},
		},
		{
			name:     "midjourney",
			input:    `{"description":"discord account unavailable","type":"upstream_error","code":4,"result":"secret","properties":{"provider":"secret"}}`,
			contains: []string{`"description":"4"`, `"type":"upstream_error"`, `"code":4`},
			excludes: []string{"discord account", "result", "properties", "secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := errorPrivacyTestContext(t, false, http.StatusBadRequest)
			got := string(RewriteClientResponseBytes(c, []byte(tt.input)))
			for _, expected := range tt.contains {
				require.Contains(t, got, expected)
			}
			for _, hidden := range tt.excludes {
				require.NotContains(t, got, hidden)
			}
		})
	}
}

func TestRewriteClientResponseBytesPreservesDetailsWhenEnabled(t *testing.T) {
	input := []byte(`{"error":{"message":"provider detail","code":"invalid_api_key","metadata":{"provider":"secret"}}}`)
	c := errorPrivacyTestContext(t, true, http.StatusBadRequest)
	require.Equal(t, input, RewriteClientResponseBytes(c, input))
}

func TestRewriteClientResponseBytesHidesSSEAndRealtimeErrors(t *testing.T) {
	c := errorPrivacyTestContext(t, false, http.StatusOK)
	sse := []byte("event: error\n" +
		"data: {\"code\":\"stream_failed\",\"message\":\"provider stream secret\",\"metadata\":{\"host\":\"secret\"}}\n\n")
	gotSSE := string(RewriteClientResponseBytes(c, sse))
	require.Contains(t, gotSSE, `"message":"stream_failed"`)
	require.NotContains(t, gotSSE, "provider stream")
	require.NotContains(t, gotSSE, "metadata")
	safeSSE := []byte("event: error\n" + "data: {\"code\":\"already_safe\"}\n\n")
	require.Equal(t, safeSSE, RewriteClientResponseBytes(c, safeSSE))

	realtime := []byte(`{"type":"error","event_id":"evt_public","error":{"message":"websocket provider secret","code":"realtime_failed","metadata":{"host":"secret"}}}`)
	gotRealtime := string(RewriteClientResponseBytes(c, realtime))
	require.Contains(t, gotRealtime, `"event_id":"evt_public"`)
	require.Contains(t, gotRealtime, `"message":"realtime_failed"`)
	require.NotContains(t, gotRealtime, "provider secret")
	require.NotContains(t, gotRealtime, "metadata")
}

func TestRewriteClientResponseBytesUsesSafeFallbackAndLeavesSuccessUnchanged(t *testing.T) {
	c := errorPrivacyTestContext(t, false, http.StatusOK)
	success := []byte(`{"id":"chatcmpl-1","choices":[{"message":{"content":"normal response"}}]}`)
	require.Equal(t, success, RewriteClientResponseBytes(c, success))

	c.Status(http.StatusInternalServerError)
	fallback := string(RewriteClientResponseBytes(c, []byte(`{"error":{"type":"error","message":"provider secret"}}`)))
	require.Contains(t, fallback, `"message":"upstream_error"`)
	require.NotContains(t, fallback, "provider secret")

	plainContext := errorPrivacyTestContext(t, false, http.StatusBadGateway)
	require.Equal(t, "upstream_error", string(RewriteClientResponseBytes(plainContext, []byte("provider gateway secret"))))
}

func TestRelayResponseWriterRemovesErrorContentLength(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	InstallRelayResponseWriter(c)
	c.Header("Content-Length", "999")
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "bad_request", "message": "provider secret"}})

	require.Empty(t, recorder.Header().Get("Content-Length"))
	require.Contains(t, recorder.Body.String(), `"message":"bad_request"`)
	require.NotContains(t, recorder.Body.String(), "provider secret")
}

func TestRelayResponseWriterTracksSplitSSEErrorEvents(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{})
	InstallRelayResponseWriter(c)
	c.Header("Content-Type", "text/event-stream")

	_, err := c.Writer.Write([]byte("event: error\n"))
	require.NoError(t, err)
	_, err = c.Writer.Write([]byte("data: {\"message\":\"provider-only detail\"}\n\n"))
	require.NoError(t, err)

	require.Contains(t, recorder.Body.String(), `"message":"upstream_error"`)
	require.NotContains(t, recorder.Body.String(), "provider-only")
}
