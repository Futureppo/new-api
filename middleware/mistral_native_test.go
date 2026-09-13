package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMistralNativeRoutingIdentifiers(t *testing.T) {
	for _, tc := range []struct{ method, path, body, model string }{
		{"POST", "/v1/ocr", `{"model":"mistral-ocr-latest","document":{"type":"document_url","document_url":"https://example.com/a.pdf"}}`, "mistral-ocr-latest"},
		{"POST", "/v1/agents/completions", `{"agent_id":"ag:example","messages":[]}`, "ag:example"},
		{"GET", "/v1/audio/transcriptions/realtime?model=voxtral-mini-transcribe-realtime-2602", "", "voxtral-mini-transcribe-realtime-2602"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		defer common.CleanupBodyStorage(c)
		c.Request = httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		request, selectChannel, err := getModelRequest(c)
		require.NoError(t, err)
		require.True(t, selectChannel)
		require.Equal(t, tc.model, request.Model)
	}
}
