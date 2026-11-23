package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestResponseOpenAI2ClaudeAndGeminiNilSafety(t *testing.T) {
	if got := ResponseOpenAI2Claude(nil, nil); got != nil {
		t.Fatalf("expected ResponseOpenAI2Claude(nil, nil) == nil, got %+v", got)
	}
	if got := ResponseOpenAI2Gemini(nil, nil); got != nil {
		t.Fatalf("expected ResponseOpenAI2Gemini(nil, nil) == nil, got %+v", got)
	}
}

func TestCalculateTextQuotaSummaryBillsAnthropicPureCacheHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-sonnet-4-5",
		PriceData: types.PriceData{
			ModelRatio:           1.5,
			CompletionRatio:      5.0,
			CacheRatio:           0.1,
			CacheCreationRatio:   1.25,
			CacheCreation5mRatio: 1.25,
			GroupRatioInfo: types.GroupRatioInfo{
				GroupRatio: 1.0,
			},
		},
	}
	// Anthropic exclusive semantic: 0 uncached input, 0 output, 10000 cached read tokens.
	usage := &dto.Usage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 10000,
		},
	}
	summary := calculateTextQuotaSummary(ctx, info, usage)
	if summary.Quota <= 0 {
		t.Fatalf("expected positive quota for Anthropic 100%% cache hit (10000 cached tokens), got quota=%d", summary.Quota)
	}
}
