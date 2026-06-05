package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSupportsResponsesCompactAllowsXAIOnlyForCodexCompatibilityHit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		apiType int
		enabled bool
		ua      string
		want    bool
	}{
		{
			name:    "openai always supported",
			apiType: appconstant.APITypeOpenAI,
			want:    true,
		},
		{
			name:    "codex always supported",
			apiType: appconstant.APITypeCodex,
			want:    true,
		},
		{
			name:    "xai supported when compatibility enabled and codex user agent",
			apiType: appconstant.APITypeXai,
			enabled: true,
			ua:      "Codex CLI",
			want:    true,
		},
		{
			name:    "xai rejected when compatibility disabled",
			apiType: appconstant.APITypeXai,
			enabled: false,
			ua:      "Codex CLI",
			want:    false,
		},
		{
			name:    "xai rejected when user agent does not match",
			apiType: appconstant.APITypeXai,
			enabled: true,
			ua:      "curl/8.0",
			want:    false,
		},
		{
			name:    "other api type rejected",
			apiType: appconstant.APITypeAnthropic,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
			c.Request.Header.Set("User-Agent", tt.ua)

			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiType: tt.apiType,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						XAICodexCompatibilityEnabled: tt.enabled,
					},
				},
			}

			require.Equal(t, tt.want, supportsResponsesCompact(c, info))
		})
	}
}
