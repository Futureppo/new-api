package doubao

import (
	"testing"

	channelconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

func TestBuildRequestURLNormalizesArkDataPlaneBase(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "host only",
			baseURL: "https://ark.cn-beijing.volces.com",
			want:    "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
		},
		{
			name:    "api v3",
			baseURL: "https://ark.cn-beijing.volces.com/api/v3",
			want:    "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
		},
		{
			name:    "api v3 trailing slash",
			baseURL: "https://ark.cn-beijing.volces.com/api/v3/",
			want:    "https://ark.cn-beijing.volces.com/api/v3/contents/generations/tasks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			adaptor.Init(&relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:    channelconstant.ChannelTypeVolcEngine,
					ChannelBaseUrl: tt.baseURL,
					ApiKey:         "test-key",
				},
			})

			got, err := adaptor.BuildRequestURL(nil)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
