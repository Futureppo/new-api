package common

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

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
