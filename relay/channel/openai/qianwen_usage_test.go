package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestQianwenResponsesCacheDetails(t *testing.T) {
	for _, tc := range []struct {
		top          string
		read, create int
	}{
		{``, 40, 20},
		{`,"input_tokens_details":{"cached_tokens":0,"cache_creation_input_tokens":0}`, 0, 0},
		{`,"input_tokens_details":{"cached_tokens":30}`, 30, 20},
		{`,"input_tokens_details":{"cached_tokens":30,"cache_write_tokens":10}`, 30, 10},
	} {
		var upstream dto.Usage
		require.NoError(t, common.UnmarshalJsonStr(`{"input_tokens":100,"output_tokens":10,"total_tokens":110,"output_tokens_details":{"reasoning_tokens":3}`+tc.top+`,"x_details":[{"x_billing_type":"tool","input_tokens_details":{"cached_tokens":999}},{"x_billing_type":"response_api","input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":40,"cache_creation_input_tokens":20}},{"x_billing_type":"response_api","input_tokens_details":{"cache_creation_input_tokens":999}}]}`, &upstream))
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeQianwenAI}}
		usage := normalizeResponsesUsageForChannel(&upstream, info)
		require.Equal(t, 100, usage.PromptTokens)
		require.Equal(t, 10, usage.CompletionTokens)
		require.Equal(t, 110, usage.TotalTokens)
		require.Equal(t, 3, usage.CompletionTokenDetails.ReasoningTokens)
		require.Equal(t, tc.read, usage.PromptTokensDetails.CachedTokens)
		require.Equal(t, tc.create, usage.PromptTokensDetails.GetCacheCreationTokens())
		info.ChannelType = constant.ChannelTypeAli
		require.Equal(t, normalizeResponsesUsage(&upstream), normalizeResponsesUsageForChannel(&upstream, info))
	}
}
