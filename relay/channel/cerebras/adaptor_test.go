package cerebras

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestAdaptorMetadata(t *testing.T) {
	adaptor := &Adaptor{}

	require.Equal(t, ChannelName, adaptor.GetChannelName())
	require.Equal(t, ModelList, adaptor.GetModelList())
}

func TestAdaptorGetRequestURL(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCerebras,
			ChannelBaseUrl: constant.ChannelBaseURLs[constant.ChannelTypeCerebras],
		},
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
	}

	url, err := adaptor.GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://api.cerebras.ai/v1/chat/completions", url)
}

func TestAdaptorGetRequestURLForConvertedClaude(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCerebras,
			ChannelBaseUrl: constant.ChannelBaseURLs[constant.ChannelTypeCerebras],
		},
		RelayFormat:    types.RelayFormatClaude,
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/messages",
	}

	url, err := adaptor.GetRequestURL(info)

	require.NoError(t, err)
	require.Equal(t, "https://api.cerebras.ai/v1/chat/completions", url)
}

func TestConvertOpenAIRequestPreservesStreamOptions(t *testing.T) {
	adaptor := &Adaptor{}
	request := &dto.GeneralOpenAIRequest{
		Model:         "gpt-oss-120b",
		Stream:        lo.ToPtr(true),
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeCerebras,
			SupportStreamOptions: true,
			UpstreamModelName:    "gpt-oss-120b",
		},
	}

	converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)

	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.NotNil(t, convertedRequest.StreamOptions)
	require.True(t, convertedRequest.StreamOptions.IncludeUsage)
}
