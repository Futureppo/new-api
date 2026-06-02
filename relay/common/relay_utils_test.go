package common

import (
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitReqAcceptsImageObject(t *testing.T) {
	var req TaskSubmitReq
	err := rootcommon.Unmarshal([]byte(`{
		"model":"grok-imagine-video-1.5-preview",
		"prompt":"animate this",
		"image":{"url":"https://example.com/image.png"}
	}`), &req)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/image.png", req.Image)
	require.True(t, req.HasImage())
}

func TestTaskSubmitReqAcceptsImageURL(t *testing.T) {
	var req TaskSubmitReq
	err := rootcommon.Unmarshal([]byte(`{
		"model":"grok-imagine-video-1.5-preview",
		"prompt":"animate this",
		"image_url":"https://example.com/image.png"
	}`), &req)

	require.NoError(t, err)
	require.Equal(t, "https://example.com/image.png", req.ImageURL)
	require.True(t, req.HasImage())
}
