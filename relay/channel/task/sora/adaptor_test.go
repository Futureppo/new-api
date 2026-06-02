package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestURLUsesXaiGenerationsEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://api.x.ai"}

	url, err := adaptor.BuildRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-imagine-video",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.x.ai/v1/videos/generations", url)

	url, err = adaptor.BuildRequestURL(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "sora-2",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "https://api.x.ai/v1/videos", url)
}

func TestParseTaskResultAcceptsXaiDoneVideo(t *testing.T) {
	body := []byte(`{"status":"done","video":{"url":"https://vidgen.x.ai/xai-vidgen-bucket/xai-video.mp4","duration":6,"respect_moderation":true},"model":"grok-imagine-video-1.5-preview","usage":{"cost_in_usd_ticks":4200000000},"progress":100}`)

	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult(body)

	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), taskInfo.Status)
	assert.Equal(t, "https://vidgen.x.ai/xai-vidgen-bucket/xai-video.mp4", taskInfo.Url)
}

func TestNormalizeXaiVideoRequestBodyConvertsImageString(t *testing.T) {
	req := map[string]interface{}{
		"model":  "grok-imagine-video-1.5-preview",
		"prompt": "animate this",
		"image":  "https://example.com/image.png",
	}

	normalizeXaiVideoRequestBody(req)

	image, ok := req["image"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://example.com/image.png", image["url"])
}

func TestNormalizeXaiVideoRequestBodyConvertsImagesArray(t *testing.T) {
	req := map[string]interface{}{
		"model":  "grok-imagine-video-1.5-preview",
		"prompt": "animate this",
		"images": []interface{}{"https://example.com/image.png"},
	}

	normalizeXaiVideoRequestBody(req)

	image, ok := req["image"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://example.com/image.png", image["url"])
	assert.NotContains(t, req, "images")
}
