package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	openailocalmodel "github.com/QuantumNous/new-api/relay/channel/openailocal"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAILocalSpecialEndpointsUsePublicModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "search",
			path:     "/v1/search",
			expected: openailocalmodel.ModelSearch,
		},
		{
			name:     "ppt",
			path:     "/v1/ppt/generations",
			expected: openailocalmodel.ModelPPT,
		},
		{
			name:     "psd",
			path:     "/v1/psd/generations",
			expected: openailocalmodel.ModelPSD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, nil)

			modelRequest, shouldSelectChannel, err := getModelRequest(c)
			require.NoError(t, err)
			require.True(t, shouldSelectChannel)
			require.Equal(t, tt.expected, modelRequest.Model)
		})
	}
}
