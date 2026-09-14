package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSignedTextQuota(t *testing.T) {
	for _, tc := range []struct {
		name  string
		price types.PriceData
		usage dto.Usage
		want  int
	}{
		{"fixed", types.PriceData{UsePrice: true, ModelPrice: -0.002}, dto.Usage{PromptTokens: 10}, -int(0.002 * common.QuotaPerUnit)},
		{"input", types.PriceData{ModelRatio: -1, CompletionRatio: 2}, dto.Usage{PromptTokens: 10, CompletionTokens: 5}, -20},
		{"output", types.PriceData{ModelRatio: 1, CompletionRatio: -3}, dto.Usage{PromptTokens: 10, CompletionTokens: 5}, -5},
		{"mixed positive", types.PriceData{ModelRatio: 1, CompletionRatio: -1}, dto.Usage{PromptTokens: 10, CompletionTokens: 5}, 5},
		{"net zero", types.PriceData{ModelRatio: 1, CompletionRatio: -2}, dto.Usage{PromptTokens: 10, CompletionTokens: 5}, 0},
		{"cache", types.PriceData{ModelRatio: 1, CacheRatio: -2}, dto.Usage{PromptTokens: 10, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 5}}, -5},
		{"cache write", types.PriceData{ModelRatio: 1, CacheCreationRatio: -2}, dto.Usage{PromptTokens: 10, PromptTokensDetails: dto.InputTokenDetails{CachedCreationTokens: 5}}, -5},
		{"image", types.PriceData{ModelRatio: 1, ImageRatio: -2}, dto.Usage{PromptTokens: 10, PromptTokensDetails: dto.InputTokenDetails{ImageTokens: 5}}, -5},
		{"tiny negative", types.PriceData{ModelRatio: -0.1}, dto.Usage{PromptTokens: 1}, 0},
		{"tiny positive", types.PriceData{ModelRatio: 0.1}, dto.Usage{PromptTokens: 1}, 1},
		{"free", types.PriceData{ModelRatio: 0}, dto.Usage{PromptTokens: 10}, 0},
		{"no usage", types.PriceData{UsePrice: true, ModelPrice: -1}, dto.Usage{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			tc.price.GroupRatioInfo.GroupRatio = 1
			info := &relaycommon.RelayInfo{OriginModelName: "signed-price-test", PriceData: tc.price, StartTime: time.Now()}
			require.Equal(t, tc.want, calculateTextQuotaSummary(ctx, info, &tc.usage).Quota)
		})
	}
}

func TestSignedAudioQuota(t *testing.T) {
	previousAudio := ratio_setting.AudioRatio2JSONString()
	previousOutput := ratio_setting.AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(previousAudio))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(previousOutput))
	})
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"signed-audio":-2}`))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(`{"signed-audio":3}`))
	info := QuotaInfo{ModelName: "signed-audio", ModelRatio: 1, GroupRatio: 0.5,
		InputDetails: TokenDetails{TextTokens: 10, AudioTokens: 5}, OutputDetails: TokenDetails{AudioTokens: 5}}
	require.Equal(t, -15, calculateAudioQuota(info))
	info.OutputDetails.AudioTokens = 0
	require.Zero(t, calculateAudioQuota(info))
	info.UsePrice, info.ModelPrice = true, -0.002
	require.Equal(t, -int(0.001*common.QuotaPerUnit), calculateAudioQuota(info))
}

func TestNegativeWalletBillingSession(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "settle once", false: "failed request"}[success], func(t *testing.T) {
			truncate(t)
			seedUser(t, 1, 1000)
			seedToken(t, 1, 1, "negative-wallet", 1000)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "negative-wallet", ForcePreConsume: true,
				UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}}
			session, apiErr := NewBillingSession(ctx, info, -100)
			require.Nil(t, apiErr)
			require.Zero(t, session.GetPreConsumedQuota())
			require.Equal(t, 1000, getUserQuota(t, 1))
			if !success {
				session.RefundSync(ctx)
			}
			require.NoError(t, session.Settle(-100))
			require.NoError(t, session.Settle(-100))
			session.RefundSync(ctx)
			want := 1000
			if success {
				want = 1100
			}
			require.Equal(t, want, getUserQuota(t, 1))
			require.Equal(t, want, getTokenRemainQuota(t, 1))
		})
	}
}

func TestNegativeSubscriptionRestoresAllowanceOnly(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "negative-subscription", 999)
	seedSubscription(t, 1, 1, 100, 11)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "negative-subscription"}
	session := &BillingSession{relayInfo: info, funding: &SubscriptionFunding{subscriptionId: 1}, preConsumedQuota: 1, tokenConsumed: 1}
	require.NoError(t, session.Settle(-50))
	require.NoError(t, session.Settle(-50))
	require.Zero(t, getSubscriptionUsed(t, 1))
	require.Equal(t, 1000, getUserQuota(t, 1))
	require.Equal(t, 1050, getTokenRemainQuota(t, 1))
}

func TestRealtimeReservesPositiveUsageAndSettlesNetCreditOnce(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 1000)
	seedToken(t, 1, 1, "negative-realtime", 1000)
	seedChannel(t, 1)
	previousRatio, previousCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatio))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(previousCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"negative-realtime":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"negative-realtime":-2}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, TokenKey: "negative-realtime", OriginModelName: "negative-realtime", UsingGroup: "default", UserGroup: "default", ForcePreConsume: true,
		StartTime: time.Now(), UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: -2, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	info.ChannelMeta = &relaycommon.ChannelMeta{ChannelId: 1}
	require.Nil(t, PreConsumeBilling(ctx, 0, info))
	input := &dto.RealtimeUsage{InputTokens: 10, TotalTokens: 10}
	input.InputTokenDetails.TextTokens = 10
	require.NoError(t, PreWssConsumeQuota(ctx, info, input))
	require.Equal(t, 990, getUserQuota(t, 1))
	output := &dto.RealtimeUsage{OutputTokens: 10, TotalTokens: 10}
	output.OutputTokenDetails.TextTokens = 10
	require.NoError(t, PreWssConsumeQuota(ctx, info, output))
	require.Equal(t, 990, getUserQuota(t, 1), "a negative interim amount is not credited")
	input.OutputTokens = 10
	input.OutputTokenDetails.TextTokens = 10
	input.TotalTokens = 20
	PostWssConsumeQuota(ctx, info, "negative-realtime", input, "")
	require.Equal(t, 1010, getUserQuota(t, 1))
	require.Equal(t, 1010, getTokenRemainQuota(t, 1))
	require.NoError(t, SettleBilling(ctx, info, -10))
	require.Equal(t, 1010, getUserQuota(t, 1))
}

func TestTextAudioUsesConfiguredSignedPrices(t *testing.T) {
	previousAudio, previousCompletion := ratio_setting.AudioRatio2JSONString(), ratio_setting.AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(previousAudio))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(previousCompletion))
	})
	require.NoError(t, ratio_setting.UpdateAudioRatioByJSONString(`{"negative-text-audio":-2}`))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(`{"negative-text-audio":3}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "negative-text-audio", StartTime: time.Now(), PriceData: types.PriceData{ModelRatio: 1, CompletionRatio: 1, AudioRatio: -2, AudioCompletionRatio: 3, GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 5, PromptTokensDetails: dto.InputTokenDetails{AudioTokens: 5}}
	usage.CompletionTokenDetails.AudioTokens = 5
	require.Equal(t, -35, calculateTextQuotaSummary(ctx, info, usage).Quota)
}

func TestFixedNegativeOneLogKeepsExplicitPriceMode(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, usePrice := range []bool{false, true} {
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, PriceData: types.PriceData{UsePrice: usePrice, ModelPrice: -1}}
		other := GenerateTextOtherInfo(ctx, info, 0, 1, 1, 0, 1, -1, -1)
		require.Equal(t, usePrice, other["use_price"])
		require.Equal(t, float64(-1), other["model_price"])
		require.Equal(t, usePrice, GenerateMjOtherInfo(info, info.PriceData)["use_price"])
	}
}
