package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVertexPerCallWithoutInventedTokens(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{StartTime: time.Now(), NonTokenUsage: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVertexAi}, PriceData: types.PriceData{UsePrice: true, ModelPrice: 0.01, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}, OtherRatios: map[string]float64{"n": 2}}}
	usage := &dto.Usage{}
	summary := calculateTextQuotaSummary(c, info, usage)
	require.Equal(t, int(common.QuotaPerUnit*0.02), summary.Quota)
	require.Zero(t, summary.TotalTokens)
	require.Zero(t, usage.PromptTokens)
	info.NonTokenUsage = false
	summary = calculateTextQuotaSummary(c, info, usage)
	require.Zero(t, summary.Quota)
}
