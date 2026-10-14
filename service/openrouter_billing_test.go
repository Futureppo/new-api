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

func TestOpenRouterNativeMessagesBilling(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "anthropic/test", StartTime: time.Now(), FinalRequestRelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}, PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 2, CacheRatio: .25, CacheCreationRatio: 1.25, CacheCreation5mRatio: 1.25, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	u := &dto.Usage{PromptTokens: 40, CompletionTokens: 10, UsageSource: "openrouter_native_messages", UsageSemantic: "anthropic", PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 40, CachedCreationTokens: 20}, ClaudeCacheCreation5mTokens: 20, Cost: 999}
	summary := calculateTextQuotaSummary(c, info, u)
	require.Equal(t, 40, summary.PromptTokens)
	require.Equal(t, 95, summary.Quota) // 40 + 40*.25 + 20*1.25 + 10*2; cost is informational.
	params := BuildTieredTokenParams(u, summary.IsClaudeUsageSemantic, map[string]bool{"cr": true, "cc": true})
	require.Equal(t, float64(40), params.P)
	require.Equal(t, float64(100), params.Len)
	info.TieredBillingSnapshot = makeSnapshot(`tier("base", p*2 + c*4 + cr*0.5 + cc*2.5)`, 1, 0, 0)
	ok, quota, _ := TryTieredSettle(info, params)
	require.True(t, ok)
	require.Equal(t, 95, quota)
	u.PromptTokens, u.CompletionTokens = 0, 0
	require.Equal(t, 35, calculateTextQuotaSummary(c, info, u).Quota, "cache-only Messages usage remains billable")
}

func TestOpenRouterFixedImageCountWithZeroTokenUsage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}, PriceData: types.PriceData{UsePrice: true, ModelPrice: .01, OtherRatios: map[string]float64{"n": 2}, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	c.Set("openrouter_completed_images", 2)
	require.Equal(t, int(.02*common.QuotaPerUnit), calculateTextQuotaSummary(c, info, &dto.Usage{}).Quota)
	c.Set("openrouter_completed_images", 0)
	require.Zero(t, calculateTextQuotaSummary(c, info, &dto.Usage{}).Quota)
}
