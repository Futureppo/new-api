package vyceai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConsumeImageStream(t *testing.T) {
	t.Parallel()

	response := sseResponse(strings.Join([]string{
		": keepalive",
		"event: progress",
		`data: {"message":"working"}`,
		"",
		"event: complete",
		`data: {"message":"done",`,
		`data: "url":"data:image/png;base64,aGVsbG8="}`,
		"",
	}, "\n"))

	image, err := consumeImageStream(response)
	require.NoError(t, err)
	require.Equal(t, "aGVsbG8=", image)
}

func TestConsumeImageStreamErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed json", body: "event: progress\ndata: {bad}\n\n", want: "invalid upstream event data"},
		{name: "missing completion", body: "event: progress\ndata: {\"message\":\"working\"}\n\n", want: "ended before the completion event"},
		{name: "missing url", body: "event: complete\ndata: {\"message\":\"done\"}\n\n", want: "missing url"},
		{name: "http url", body: "event: complete\ndata: {\"url\":\"https://example.com/image.png\"}\n\n", want: "not a Base64 data URL"},
		{name: "non image data url", body: "event: complete\ndata: {\"url\":\"data:text/plain;base64,aGVsbG8=\"}\n\n", want: "not an image Base64 data URL"},
		{name: "invalid base64", body: "event: complete\ndata: {\"url\":\"data:image/png;base64,%%%\"}\n\n", want: "invalid Base64"},
		{name: "error event", body: "event: error\ndata: {\"message\":\"generation rejected\"}\n\n", want: "generation rejected"},
		{name: "error object", body: "event: message\ndata: {\"error\":{\"message\":\"provider failed\"}}\n\n", want: "provider failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := consumeImageStream(sseResponse(test.body))
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestBase64FromDataURLRejectsEmptyPayload(t *testing.T) {
	t.Parallel()

	_, err := base64FromDataURL("data:image/jpeg;base64,")
	require.ErrorContains(t, err, "empty payload")
}

func TestSafetyErrorReturnsForbidden(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{}
	resp := sseResponse("event: error\ndata: {\"message\":\"Prompt blocked by safety filters: Grok declined to generate this prompt due to content policies.\"}\n\n")

	_, apiErr := handleImageResponse(c, resp, info)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodePromptBlocked, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestNonSafetyErrorStillReturnsBadGateway(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	info := &relaycommon.RelayInfo{}
	resp := sseResponse("event: error\ndata: {\"message\":\"provider unavailable\"}\n\n")

	_, apiErr := handleImageResponse(c, resp, info)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeBadResponseBody, apiErr.GetErrorCode())
}

func sseResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
