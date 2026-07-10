package vertex

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

func TestBuildRequestURLUsesSharedVertexURLBuilder(t *testing.T) {
	credential, err := common.Marshal(vertexcore.Credentials{
		ProjectID:   "video-project",
		PrivateKey:  "test-private-key",
		ClientEmail: "video@example.iam.gserviceaccount.com",
		TokenURI:    "https://oauth2.googleapis.com/token",
	})
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelBaseUrl:    "https://gateway.example/prefix",
		ApiKey:            string(credential),
		ApiVersion:        `{"default":"global"}`,
		UpstreamModelName: "veo-3.1-generate-preview",
	}}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	requestURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(
		t,
		"https://gateway.example/prefix/v1/projects/video-project/locations/global/publishers/google/models/veo-3.1-generate-preview:predictLongRunning",
		requestURL,
	)
}
