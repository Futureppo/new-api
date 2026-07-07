package controller

import (
	"net/http"
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

func TestNormalizeChannelTestEndpointVideoModels(t *testing.T) {
	require.Equal(t, string(constant.EndpointTypeOpenAIVideo), normalizeChannelTestEndpoint(nil, "sora-2", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAIVideo), normalizeChannelTestEndpoint(nil, "grok-imagine-video-1.5-preview", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAI), normalizeChannelTestEndpoint(nil, "sora-2", string(constant.EndpointTypeOpenAI)))
	require.Equal(t, "", normalizeChannelTestEndpoint(&model.Channel{Type: constant.ChannelTypePoe}, "sora-2", ""))
}

func TestNormalizeChannelTestEndpointVolcEngineModels(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeVolcEngine}

	require.Equal(t, "", normalizeChannelTestEndpoint(channel, "doubao-seed-2-0-pro-260215", ""))
	require.Equal(t, "", normalizeChannelTestEndpoint(channel, "deepseek-v4-flash-260425", ""))
	require.Equal(t, string(constant.EndpointTypeEmbeddings), normalizeChannelTestEndpoint(channel, "doubao-embedding-text-240715", ""))
	require.Equal(t, string(constant.EndpointTypeEmbeddings), normalizeChannelTestEndpoint(channel, "doubao-embedding-vision-251215", ""))
	require.Equal(t, string(constant.EndpointTypeImageGeneration), normalizeChannelTestEndpoint(channel, "doubao-seedream-5-0-260128", ""))
	require.Equal(t, string(constant.EndpointTypeImageGeneration), normalizeChannelTestEndpoint(channel, "doubao-seededit-3-0-i2i-250628", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAIVideo), normalizeChannelTestEndpoint(channel, "doubao-seedance-2-0-fast-260128", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAIVideo), normalizeChannelTestEndpoint(channel, "wan2-1-14b-i2v-250225", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAIVideo), normalizeChannelTestEndpoint(channel, "doubao-seed3d-2-0-260328", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAIVideo), normalizeChannelTestEndpoint(channel, "hyper3d-gen2-260112", ""))
	require.Equal(t, string(constant.EndpointTypeOpenAI), normalizeChannelTestEndpoint(channel, "doubao-seedream-5-0-260128", string(constant.EndpointTypeOpenAI)))
}

func TestBuildTestVideoRequestBody(t *testing.T) {
	data, err := buildTestVideoRequestBody("sora-2")
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, common.Unmarshal(data, &body))
	require.Equal(t, "sora-2", body["model"])
	require.NotEmpty(t, body["prompt"])
	require.Equal(t, "4", body["seconds"])
	require.Equal(t, "720x1280", body["size"])

	data, err = buildTestVideoRequestBody("veo-3.1-generate-preview")
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, &body))
	require.Equal(t, float64(8), body["duration"])
	require.Equal(t, "1280x720", body["size"])

	data, err = buildTestVideoRequestBody("doubao-seed3d-2-0-260328")
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, &body))
	require.NotEmpty(t, body["image_url"])

	data, err = buildTestVideoRequestBody("wan2-1-14b-flf2v-250417")
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(data, &body))
	require.Len(t, body["images"], 2)
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

func TestExtractOpenAIRateLimitInfoOfficialHeaders(t *testing.T) {
	baseURL := "https://api.openai.com"
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &baseURL}
	headers := http.Header{}
	headers.Set("x-ratelimit-limit-requests", "500")
	headers.Set("x-ratelimit-limit-tokens", "30,000")
	headers.Set("x-ratelimit-remaining-requests", "499")
	headers.Set("x-ratelimit-remaining-tokens", "29900")
	headers.Set("x-ratelimit-reset-requests", "120ms")
	headers.Set("x-ratelimit-reset-tokens", "1s")
	headers.Set("x-ratelimit-limit-project-tokens", "100000")
	headers.Set("x-ratelimit-remaining-project-tokens", "99000")
	headers.Set("x-ratelimit-reset-project-tokens", "1m")

	originalRules := openAIRateLimitTierRules
	openAIRateLimitTierRules = []openAIRateLimitTierRule{
		{
			Model:         "gpt-test",
			LimitRequests: "500",
			LimitTokens:   "30000",
			Tier:          "T2",
		},
	}
	defer func() {
		openAIRateLimitTierRules = originalRules
	}()

	info := extractOpenAIRateLimitInfo(channel, "gpt-test", headers)
	require.NotNil(t, info)
	require.Equal(t, "openai", info.Provider)
	require.Equal(t, "gpt-test", info.Model)
	require.Equal(t, "T2", info.Tier)
	require.Equal(t, "500", info.LimitRequests)
	require.Equal(t, "30,000", info.LimitTokens)
	require.Equal(t, "499", info.RemainingRequests)
	require.Equal(t, "29900", info.RemainingTokens)
	require.Equal(t, "120ms", info.ResetRequests)
	require.Equal(t, "1s", info.ResetTokens)
	require.Equal(t, "100000", info.LimitProjectTokens)
	require.Equal(t, "99000", info.RemainingProjectTokens)
	require.Equal(t, "1m", info.ResetProjectTokens)
}

func TestExtractOpenAIRateLimitInfoMissingHeaders(t *testing.T) {
	baseURL := "https://api.openai.com"
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &baseURL}

	require.Nil(t, extractOpenAIRateLimitInfo(channel, "gpt-test", http.Header{}))
}

func TestExtractOpenAIRateLimitInfoOnlyOfficialOpenAIChannel(t *testing.T) {
	headers := http.Header{}
	headers.Set("x-ratelimit-limit-requests", "500")

	defaultBaseChannel := &model.Channel{Type: constant.ChannelTypeOpenAI}
	require.NotNil(t, extractOpenAIRateLimitInfo(defaultBaseChannel, "gpt-test", headers))

	customBaseURL := "https://example.com"
	customBaseChannel := &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &customBaseURL}
	require.Nil(t, extractOpenAIRateLimitInfo(customBaseChannel, "gpt-test", headers))

	openAIBaseURL := "https://api.openai.com"
	openAILocalChannel := &model.Channel{Type: constant.ChannelTypeOpenAILocal, BaseURL: &openAIBaseURL}
	require.Nil(t, extractOpenAIRateLimitInfo(openAILocalChannel, "gpt-test", headers))
}

func TestExtractOpenAIRateLimitInfoInvalidValuesDoNotInferTier(t *testing.T) {
	baseURL := "https://api.openai.com"
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, BaseURL: &baseURL}
	headers := http.Header{}
	headers.Set("x-ratelimit-limit-requests", "not-a-number")

	originalRules := openAIRateLimitTierRules
	openAIRateLimitTierRules = []openAIRateLimitTierRule{
		{
			Model:         "gpt-test",
			LimitRequests: "500",
			Tier:          "T1",
		},
	}
	defer func() {
		openAIRateLimitTierRules = originalRules
	}()

	info := extractOpenAIRateLimitInfo(channel, "gpt-test", headers)
	require.NotNil(t, info)
	require.Equal(t, "not-a-number", info.LimitRequests)
	require.Empty(t, info.Tier)
}
