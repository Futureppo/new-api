package opencode

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClientHeadersOnUpstreamRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	protocols := []struct {
		name       string
		info       func() *relaycommon.RelayInfo
		adaptor    channel.Adaptor
		path       string
		authHeader string
		authValue  string
	}{
		{"zen chat", func() *relaycommon.RelayInfo { return newRelayInfo("big-pickle") }, &Adaptor{}, "/v1/chat/completions", "Authorization", "Bearer upstream-key"},
		{"zen responses", func() *relaycommon.RelayInfo { return newRelayInfo("gpt-5.6-sol") }, &Adaptor{}, "/v1/responses", "Authorization", "Bearer upstream-key"},
		{"zen messages", func() *relaycommon.RelayInfo { return newRelayInfo("claude-sonnet-5") }, &Adaptor{}, "/v1/messages", "x-api-key", "upstream-key"},
		{"zen gemini", func() *relaycommon.RelayInfo { return newRelayInfo("gemini-3-flash") }, &Adaptor{}, "/v1/models/gemini-3-flash:generateContent", "x-goog-api-key", "upstream-key"},
		{"go chat", func() *relaycommon.RelayInfo { return newGoRelayInfo("kimi-k3") }, &GoAdaptor{}, "/v1/chat/completions", "Authorization", "Bearer upstream-key"},
		{"go responses", func() *relaycommon.RelayInfo { return newGoRelayInfo("gpt-5.6-luna") }, &GoAdaptor{}, "/v1/responses", "Authorization", "Bearer upstream-key"},
		{"go messages", func() *relaycommon.RelayInfo { return newGoRelayInfo("minimax-m3") }, &GoAdaptor{}, "/v1/messages", "x-api-key", "upstream-key"},
	}
	cases := []struct {
		name       string
		enabled    *bool
		incomingUA string
		client     string
		override   bool
		runtime    bool
		status     int
	}{
		{name: "default", status: http.StatusOK},
		{name: "explicit enabled", enabled: common.GetPointer(true), status: http.StatusOK},
		{name: "original client", incomingUA: "opencode/2.0.0 custom", client: "desktop", status: http.StatusOK},
		{name: "other user agent", incomingUA: "curl/8.0.0", status: http.StatusOK},
		{name: "client without user agent", client: "desktop", status: http.StatusOK},
		{name: "disabled", enabled: common.GetPointer(false), incomingUA: "opencode/2.0.0", client: "desktop", status: http.StatusOK},
		{name: "disabled without identifiers", enabled: common.GetPointer(false), status: http.StatusOK},
		{name: "disabled with other user agent", enabled: common.GetPointer(false), incomingUA: "curl/8.0.0", status: http.StatusOK},
		{name: "override enabled", incomingUA: "opencode/2.0.0", client: "desktop", override: true, status: http.StatusOK},
		{name: "override disabled", enabled: common.GetPointer(false), override: true, status: http.StatusOK},
		{name: "runtime override", override: true, runtime: true, status: http.StatusOK},
		{name: "upstream rejection preserved", status: http.StatusForbidden},
	}
	for _, protocol := range protocols {
		for _, tc := range cases {
			for _, channelTest := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/channel_test=%t", protocol.name, tc.name, channelTest), func(t *testing.T) {
					type capturedRequest struct {
						header http.Header
						path   string
					}
					captured := make(chan capturedRequest, 1)
					responseBody := `{"error":{"type":"FreeTierError","message":"OpenCode's free tier can only be used from within OpenCode"}}`
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						captured <- capturedRequest{header: r.Header.Clone(), path: r.URL.Path}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(tc.status)
						_, _ = io.WriteString(w, responseBody)
					}))
					defer upstream.Close()
					info := protocol.info()
					info.ChannelBaseUrl = upstream.URL
					info.ApiKey = "upstream-key"
					info.IsChannelTest = channelTest
					info.ChannelOtherSettings.OpenCodeClientHeadersEnabled = tc.enabled
					if tc.override {
						overrides := map[string]any{
							"user-agent": "custom-agent", "X-OpenCode-Client": "custom-client",
							"x-opencode-session": "custom-session", "x-opencode-request": "custom-request",
							"x-opencode-project": "custom-project",
						}
						info.HeadersOverride = overrides
						if tc.runtime {
							info.HeadersOverride = map[string]any{"User-Agent": "stale-agent"}
							info.UseRuntimeHeadersOverride = true
							info.RuntimeHeadersOverride = overrides
						}
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					c.Request.Header.Set("Content-Type", "application/json")
					c.Request.Header.Set("Authorization", "Bearer downstream-key")
					c.Request.Header.Set("Cookie", "private-cookie")
					c.Request.Header.Set("x-opencode-unlisted", "private-value")
					c.Request.Header.Set("User-Agent", tc.incomingUA)
					c.Request.Header.Set("x-opencode-client", tc.client)
					metadata := map[string]string{
						"x-opencode-session": "ses_original", "x-opencode-request": "msg_original", "x-opencode-project": "project-original",
					}
					// Background tests normally have no incoming client metadata.
					if !channelTest {
						for name, value := range metadata {
							c.Request.Header.Set(name, value)
						}
					}
					protocol.adaptor.Init(info)
					raw, err := protocol.adaptor.DoRequest(c, info, strings.NewReader(`{}`))
					require.NoError(t, err)
					resp := raw.(*http.Response)
					defer resp.Body.Close()
					require.Equal(t, tc.status, resp.StatusCode)
					body, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Equal(t, responseBody, string(body))
					got := <-captured
					require.Equal(t, protocol.path, got.path)
					require.Equal(t, protocol.authValue, got.header.Get(protocol.authHeader))
					require.Equal(t, "application/json", got.header.Get("Content-Type"))
					require.Empty(t, got.header.Get("Cookie"))
					require.Empty(t, got.header.Get("x-opencode-unlisted"))
					if tc.override {
						require.Equal(t, "custom-agent", got.header.Get("User-Agent"))
						require.Equal(t, "custom-client", got.header.Get("x-opencode-client"))
						for _, suffix := range []string{"session", "request", "project"} {
							require.Equal(t, "custom-"+suffix, got.header.Get("x-opencode-"+suffix))
						}
						return
					}
					if tc.enabled != nil && !*tc.enabled {
						if tc.incomingUA == "" {
							require.NotContains(t, got.header.Get("User-Agent"), "opencode/")
						} else {
							require.Equal(t, tc.incomingUA, got.header.Get("User-Agent"))
						}
						require.Equal(t, tc.client, got.header.Get("x-opencode-client"))
					} else {
						wantUA := tc.incomingUA
						if wantUA == "" {
							wantUA = "opencode/1.18.32"
						}
						wantClient := tc.client
						if wantClient == "" {
							wantClient = "cli"
						}
						require.Equal(t, wantUA, got.header.Get("User-Agent"))
						require.Equal(t, wantClient, got.header.Get("x-opencode-client"))
					}
					for name, value := range metadata {
						if channelTest {
							require.Empty(t, got.header.Get(name))
						} else {
							require.Equal(t, value, got.header.Get(name))
						}
					}
				})
			}
		}
	}
}
