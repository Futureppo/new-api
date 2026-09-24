package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChatToResponsesTokenLimitPrecedence(t *testing.T) {
	for _, tc := range []struct {
		fields string
		want   *uint
	}{
		{``, nil},
		{`,"max_tokens":0`, common.GetPointer(uint(0))},
		{`,"max_tokens":64`, common.GetPointer(uint(64))},
		{`,"max_tokens":64,"max_completion_tokens":0`, common.GetPointer(uint(0))},
		{`,"max_tokens":64,"max_completion_tokens":16`, common.GetPointer(uint(16))},
	} {
		var request dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"hi"}]`+tc.fields+`}`, &request))
		response, err := ChatCompletionsRequestToResponsesRequest(&request)
		require.NoError(t, err)
		require.Equal(t, tc.want, response.MaxOutputTokens)
		if tc.want != nil {
			require.Equal(t, *tc.want, request.GetMaxTokens())
			require.Equal(t, int(*tc.want), request.GetTokenCountMeta().MaxTokens)
		}
	}
}
