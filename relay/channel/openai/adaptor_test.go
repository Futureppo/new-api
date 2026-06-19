package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequestPreservesStreamOptionsForPoe(t *testing.T) {
	isStream := true
	request := &dto.GeneralOpenAIRequest{
		Model:         "gemma-4-31b",
		Stream:        &isStream,
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypePoe,
			SupportStreamOptions: true,
			UpstreamModelName:    "gemma-4-31b",
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, convertedRequest.StreamOptions)
	require.True(t, convertedRequest.StreamOptions.IncludeUsage)
}
