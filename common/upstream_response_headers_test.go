package common

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopyUpstreamResponseHeadersBlocksProviderMetadata(t *testing.T) {
	src := http.Header{
		"Content-Type": {"audio/mpeg"}, "Content-Length": {"123"},
		"Content-Encoding": {"gzip"}, "Content-Disposition": {`attachment; filename="audio.mp3"`},
		"Content-Range": {"bytes 0-122/456"}, "Accept-Ranges": {"bytes"}, "Retry-After": {"30"},
	}
	blocked := []string{
		"X-Litellm-Model-Api-Base", "X-Litellm-Version", "Llm_provider-Via", "Llm_provider-Server",
		"Server", "Via", "X-Powered-By", "X-Request-Id", "X-RateLimit-Limit", "Set-Cookie",
		"Authorization", "Location", "Content-Location", "Link", "Refresh", "WWW-Authenticate",
		"X-Unknown-Future-Provider", "Access-Control-Allow-Origin", "X-Oneapi-Request-Id",
		"X-New-Api-Version", "Connection", "Transfer-Encoding", "Trailer", "Cache-Control",
	}
	for _, name := range blocked {
		src.Set(name, "upstream-private-value")
	}
	dst := http.Header{
		"X-Oneapi-Request-Id": {"local-request"}, "X-New-Api-Version": {"local-version"},
		"Access-Control-Allow-Origin": {"*"}, "Cache-Control": {"no-cache"},
	}
	CopyUpstreamResponseHeaders(dst, src)
	require.Equal(t, "local-request", dst.Get("X-Oneapi-Request-Id"))
	require.Equal(t, "local-version", dst.Get("X-New-Api-Version"))
	require.Equal(t, "*", dst.Get("Access-Control-Allow-Origin"))
	require.Equal(t, "no-cache", dst.Get("Cache-Control"))
	for _, name := range blocked {
		require.NotContains(t, dst.Values(name), "upstream-private-value", name)
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Encoding", "Content-Disposition", "Content-Range", "Accept-Ranges", "Retry-After"} {
		require.Equal(t, src.Values(name), dst.Values(name), name)
	}
	// Copying must not alias or strip the upstream response needed internally.
	dst["Content-Type"][0] = "changed"
	require.Equal(t, "audio/mpeg", src.Get("Content-Type"))
	require.Equal(t, "upstream-private-value", src.Get("X-Litellm-Model-Api-Base"))
}

func TestCopyUpstreamResponseHeadersHandlesCasingAndHopByHop(t *testing.T) {
	src := http.Header{
		"content-type": {"application/json"}, "connection": {"Content-Encoding, RETRY-AFTER"},
		"Content-Encoding": {"gzip"}, "Retry-After": {"30"}, "Content-Length": nil,
		"x-litellm-model-api-base": {"https://private.example/v1"},
	}
	dst := make(http.Header)
	CopyUpstreamResponseHeaders(dst, src)
	require.Equal(t, http.Header{"Content-Type": {"application/json"}}, dst)
	CopyUpstreamResponseHeaders(nil, src)
	CopyUpstreamResponseHeaders(dst, nil)
}

func TestCopyUpstreamResponseHeadersValidatesRetryAfter(t *testing.T) {
	for _, value := range []string{"0", "30", "Wed, 21 Oct 2015 07:28:00 GMT"} {
		dst := make(http.Header)
		CopyUpstreamResponseHeaders(dst, http.Header{"Retry-After": {value}})
		require.Equal(t, value, dst.Get("Retry-After"))
	}
	for _, value := range []string{"", "-1", "https://private.example/retry", "30; provider=private"} {
		dst := make(http.Header)
		CopyUpstreamResponseHeaders(dst, http.Header{"Retry-After": {value}})
		require.Empty(t, dst)
	}
}
