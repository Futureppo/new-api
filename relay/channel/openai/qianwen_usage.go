package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func normalizeResponsesUsageForChannel(upstream *dto.Usage, info *relaycommon.RelayInfo) dto.Usage {
	usage := normalizeResponsesUsage(upstream)
	if upstream == nil || info == nil || info.ChannelType != constant.ChannelTypeQianwenAI || len(upstream.XDetails) == 0 {
		return usage
	}
	var details []struct {
		BillingType string                 `json:"x_billing_type"`
		Input       *dto.InputTokenDetails `json:"input_tokens_details"`
	}
	if err := common.Unmarshal(upstream.XDetails, &details); err != nil {
		return usage
	}
	for _, detail := range details {
		if detail.BillingType != "response_api" || detail.Input == nil {
			continue
		}
		// x_details describes the same usage, not additional tokens to add.
		if upstream.InputTokensDetails == nil {
			usage.PromptTokensDetails = *detail.Input
		} else if usage.PromptTokensDetails.CacheWriteTokens == nil && usage.PromptTokensDetails.CacheCreationInputTokens == nil && usage.PromptTokensDetails.CachedCreationTokens == 0 {
			usage.PromptTokensDetails.CacheCreationInputTokens = detail.Input.CacheCreationInputTokens
		}
		break
	}
	return usage
}
