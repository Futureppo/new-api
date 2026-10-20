package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBilibiliWebEchoPerRequestPrice(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, success := range []bool{false, true} {
		for _, channelType := range []int{constant.ChannelTypeBilibiliWeb, constant.ChannelTypeBilibili} {
			info := &relaycommon.RelayInfo{
				StartTime: time.Now(), NonTokenUsage: success,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType},
				PriceData:   types.PriceData{UsePrice: true, ModelPrice: .01, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}},
			}
			usage := &dto.Usage{}
			result := calculateTextQuotaSummary(c, info, usage)
			expected := 0
			if success && channelType == constant.ChannelTypeBilibiliWeb {
				expected = int(common.QuotaPerUnit * .01)
			}
			require.Equal(t, expected, result.Quota)
			require.Zero(t, result.TotalTokens)
			require.Zero(t, usage.PromptTokens)
			require.Zero(t, usage.CompletionTokens)
		}
	}
}
