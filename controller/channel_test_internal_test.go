package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestNormalizeChannelTestEndpointCohereModels(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeCohere}

	require.Equal(t, string(constant.EndpointTypeCohereChat), normalizeChannelTestEndpoint(channel, "command-a-03-2025", ""))
	require.Equal(t, string(constant.EndpointTypeCohereRerank), normalizeChannelTestEndpoint(channel, "rerank-v3.5", ""))
	require.Equal(t, string(constant.EndpointTypeCohereEmbeddings), normalizeChannelTestEndpoint(channel, "embed-v4.0", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAI), normalizeChannelTestEndpoint(channel, "embed-v4.0", string(constant.EndpointTypeOpenAI)))
}

func TestBuildTestRequestCohereEmbeddingIncludesInputType(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeCohere}

	request := buildTestRequest("embed-v4.0", string(constant.EndpointTypeCohereEmbeddings), channel, false)
	embeddingRequest, ok := request.(*dto.EmbeddingRequest)
	require.True(t, ok)
	require.Equal(t, "embed-v4.0", embeddingRequest.Model)
	require.Equal(t, "search_document", embeddingRequest.InputType)
	require.Equal(t, []string{"float"}, embeddingRequest.EmbeddingTypes)

	autoRequest := buildTestRequest("embed-v4.0", "", channel, false)
	autoEmbeddingRequest, ok := autoRequest.(*dto.EmbeddingRequest)
	require.True(t, ok)
	require.Equal(t, "search_document", autoEmbeddingRequest.InputType)
	require.Equal(t, []string{"float"}, autoEmbeddingRequest.EmbeddingTypes)
}
