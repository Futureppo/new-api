package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMistralNativeNoTokenCallPricing(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, tc := range []struct {
		format   types.RelayFormat
		price    float64
		expected int
	}{
		{types.RelayFormatMistralNative, 0.002, int(0.002 * common.QuotaPerUnit)},
		{types.RelayFormatMistralNative, 0, 0},
		{types.RelayFormatOpenAI, 0.002, 0},
	} {
		info := &relaycommon.RelayInfo{RelayFormat: tc.format, OriginModelName: "mistral-native-test", StartTime: time.Now(), PriceData: types.PriceData{UsePrice: true, ModelPrice: tc.price, ModelRatio: 1, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
		usage := &dto.Usage{}
		summary := calculateTextQuotaSummary(c, info, usage)
		require.Equal(t, tc.expected, summary.Quota)
		require.Zero(t, summary.TotalTokens)
		require.Zero(t, usage.TotalTokens)
	}
}
