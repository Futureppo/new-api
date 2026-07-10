package vertex

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveModelRegion(t *testing.T) {
	region, err := ResolveModelRegion(`{"default":"global","gemini-test":"europe-west1"}`, "gemini-test")
	require.NoError(t, err)
	require.Equal(t, "europe-west1", region)

	region, err = ResolveModelRegion(`{"default":"us-central1"}`, "gemini-other")
	require.NoError(t, err)
	require.Equal(t, "us-central1", region)

	_, err = ResolveModelRegion(`{"default":1}`, "gemini-test")
	require.Error(t, err)
	require.Error(t, ValidateRegionConfig(`{"default":1}`))
	require.Error(t, ValidateRegionConfig(`{"gemini-test":"global"}`))
}

func TestBuildVertexURLs(t *testing.T) {
	require.Equal(
		t,
		"https://aiplatform.googleapis.com/v1/projects/project/locations/global/publishers/google/models/gemini-test:generateContent",
		BuildGoogleModelURL("", DefaultAPIVersion, "project", "global", "gemini-test", "generateContent"),
	)
	require.Equal(
		t,
		"https://us-central1-aiplatform.googleapis.com/v1/projects/project/locations/us-central1/publishers/anthropic/models/claude-test:rawPredict",
		BuildAnthropicModelURL("", DefaultAPIVersion, "project", "us-central1", "claude-test", "rawPredict"),
	)
	require.Equal(
		t,
		"https://gateway.example/prefix/v1/projects/project/locations/global/publishers/google/models/gemini-test:generateContent",
		BuildGoogleModelURL("https://gateway.example/prefix", DefaultAPIVersion, "project", "global", "gemini-test", "generateContent"),
	)
	require.Equal(
		t,
		"https://aiplatform.googleapis.com/v1beta1/projects/project/locations/us-central1/endpoints/openapi/chat/completions",
		BuildOpenSourceChatCompletionsURL("", "project", "us-central1"),
	)

	withKey, err := AppendAPIKey(
		BuildGoogleModelURL("", DefaultAPIVersion, "", "global", "gemini-test", "streamGenerateContent?alt=sse"),
		"secret-key",
	)
	require.NoError(t, err)
	parsed, err := url.Parse(withKey)
	require.NoError(t, err)
	require.Equal(t, "sse", parsed.Query().Get("alt"))
	require.Equal(t, "secret-key", parsed.Query().Get("key"))
}

func TestBuildGoogleModelsListURL(t *testing.T) {
	rawURL, err := BuildGoogleModelsListURL("https://gateway.example/prefix", "global", "next-token")
	require.NoError(t, err)
	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	require.Equal(t, "/prefix/v1beta1/publishers/google/models", parsed.Path)
	require.Equal(t, "300", parsed.Query().Get("pageSize"))
	require.Equal(t, "false", parsed.Query().Get("listAllVersions"))
	require.Equal(t, "PUBLISHER_MODEL_VIEW_BASIC", parsed.Query().Get("view"))
	require.Equal(t, "next-token", parsed.Query().Get("pageToken"))
}
