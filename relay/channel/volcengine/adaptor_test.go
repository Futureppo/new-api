package volcengine

import (
	"testing"

	channelconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/require"
)

func TestGetRequestURLNormalizesArkDataPlaneBase(t *testing.T) {
	adaptor := &Adaptor{}

	tests := []struct {
		name    string
		baseURL string
		mode    int
		model   string
		want    string
	}{
		{
			name:    "host only chat",
			baseURL: "https://ark.cn-beijing.volces.com",
			mode:    relayconstant.RelayModeChatCompletions,
			model:   "doubao-seed-2-0-pro-260215",
			want:    "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name:    "api v3 chat",
			baseURL: "https://ark.cn-beijing.volces.com/api/v3",
			mode:    relayconstant.RelayModeChatCompletions,
			model:   "doubao-seed-2-0-pro-260215",
			want:    "https://ark.cn-beijing.volces.com/api/v3/chat/completions",
		},
		{
			name:    "text embedding",
			baseURL: "https://ark.cn-beijing.volces.com/api/v3/",
			mode:    relayconstant.RelayModeEmbeddings,
			model:   "doubao-embedding-text-240715",
			want:    "https://ark.cn-beijing.volces.com/api/v3/embeddings",
		},
		{
			name:    "multimodal embedding",
			baseURL: "https://ark.cn-beijing.volces.com",
			mode:    relayconstant.RelayModeEmbeddings,
			model:   "doubao-embedding-vision-251215",
			want:    "https://ark.cn-beijing.volces.com/api/v3/embeddings/multimodal",
		},
		{
			name:    "image generation",
			baseURL: "https://ark.cn-beijing.volces.com/api/v3",
			mode:    relayconstant.RelayModeImagesGenerations,
			model:   "doubao-seedream-5-0-260128",
			want:    "https://ark.cn-beijing.volces.com/api/v3/images/generations",
		},
		{
			name:    "responses",
			baseURL: "https://ark.cn-beijing.volces.com",
			mode:    relayconstant.RelayModeResponses,
			model:   "doubao-seed-2-0-pro-260215",
			want:    "https://ark.cn-beijing.volces.com/api/v3/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
				RelayMode: tt.mode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       channelconstant.ChannelTypeVolcEngine,
					ChannelBaseUrl:    tt.baseURL,
					UpstreamModelName: tt.model,
				},
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetRequestURLUsesDoubaoCodingPlanBase(t *testing.T) {
	adaptor := &Adaptor{}

	got, err := adaptor.GetRequestURL(&relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelconstant.ChannelTypeVolcEngine,
			ChannelBaseUrl:    "doubao-coding-plan",
			UpstreamModelName: "doubao-coding",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions", got)
}
