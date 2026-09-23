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
		{"zen chat", func() *relaycommon.RelayInfo { return newRelayInfo("mimo-v2.5") }, &Adaptor{}, "/v1/chat/completions", "Authorization", "Bearer upstream-key"},
		{"zen responses", func() *relaycommon.RelayInfo { return newRelayInfo("gpt-5.6-sol") }, &Adaptor{}, "/v1/responses", "Authorization", "Bearer upstream-key"},
		{"zen messages", func() *relaycommon.RelayInfo { return newRelayInfo("claude-sonnet-5") }, &Adaptor{}, "/v1/messages", "x-api-key", "upstream-key"},
		{"zen gemini", func() *relaycommon.RelayInfo { return newRelayInfo("gemini-3-flash") }, &Adaptor{}, "/v1/models/gemini-3-flash:generateContent", "x-goog-api-key", "upstream-key"},
		{"go chat", func() *relaycommon.RelayInfo { return newGoRelayInfo("kimi-k3") }, &GoAdaptor{}, "/v1/chat/completions", "Authorization", "Bearer upstream-key"},
		{"go responses", func() *relaycommon.RelayInfo { return newGoRelayInfo("gpt-5.6-luna") }, &GoAdaptor{}, "/v1/responses", "Authorization", "Bearer upstream-key"},
		{"go messages", func() *relaycommon.RelayInfo { return newGoRelayInfo("minimax-m3") }, &GoAdaptor{}, "/v1/messages", "x-api-key", "upstream-key"},
	}
	cases := []struct {
		name         string
		enabled      *bool
		incomingUA   string
		client       string
		omitMetadata bool
		override     bool
		runtime      bool
		passthrough  string
		status       int
	}{
		{name: "default", status: http.StatusOK},
		{name: "missing all identifiers", omitMetadata: true, status: http.StatusOK},
		{name: "explicit enabled", enabled: common.GetPointer(true), status: http.StatusOK},
		{name: "original client", incomingUA: "opencode/2.0.0 custom", client: "desktop", status: http.StatusOK},
		{name: "other user agent", incomingUA: "curl/8.0.0", status: http.StatusOK},
		{name: "agent user agent", incomingUA: "claude-code/2.0", status: http.StatusOK},
		{name: "wildcard passthrough", incomingUA: "openai-python/1.0", passthrough: "*", status: http.StatusOK},
		{name: "regex passthrough", incomingUA: "codex/1.0", passthrough: "re:(?i)^user-agent$", status: http.StatusOK},
		{name: "original user agent passthrough", incomingUA: "opencode/2.0.0 custom", passthrough: "*", status: http.StatusOK},
		{name: "disabled passthrough", enabled: common.GetPointer(false), incomingUA: "curl/8.0.0", passthrough: "*", status: http.StatusOK},
		{name: "explicit override after passthrough", incomingUA: "curl/8.0.0", passthrough: "*", override: true, status: http.StatusOK},
		{name: "runtime passthrough", incomingUA: "curl/8.0.0", passthrough: "*", runtime: true, status: http.StatusOK},
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
					info.HeadersOverride = make(map[string]any)
					if tc.override {
						overrides := map[string]any{
							"user-agent": "custom-agent", "X-OpenCode-Client": "custom-client",
							"x-opencode-session": "custom-session", "x-opencode-request": "custom-request",
							"x-opencode-project": "custom-project",
						}
						info.HeadersOverride = overrides
					}
					if tc.passthrough != "" {
						info.HeadersOverride[tc.passthrough] = ""
					}
					if tc.runtime {
						info.UseRuntimeHeadersOverride = true
						info.RuntimeHeadersOverride = info.HeadersOverride
						info.HeadersOverride = map[string]any{"User-Agent": "stale-agent"}
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
					if !channelTest && !tc.omitMetadata {
						for name, value := range metadata {
							c.Request.Header.Set(name, value)
						}
						c.Request.Header.Set("x-parent-session-id", "ses_parent")
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
					if tc.passthrough == "*" && !channelTest {
						require.Equal(t, "private-value", got.header.Get("x-opencode-unlisted"))
					} else {
						require.Empty(t, got.header.Get("x-opencode-unlisted"))
					}
					if !channelTest && !tc.omitMetadata {
						require.Equal(t, "ses_parent", got.header.Get("x-parent-session-id"))
					} else {
						require.Empty(t, got.header.Get("x-parent-session-id"))
					}
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
						if !strings.HasPrefix(wantUA, "opencode/") {
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
						if channelTest || tc.omitMetadata {
							if tc.enabled != nil && !*tc.enabled {
								require.Empty(t, got.header.Get(name))
							} else {
								switch name {
								case "x-opencode-session":
									require.Regexp(t, `^ses_[0-9a-f]{12}[0-9A-Za-z]{14}$`, got.header.Get(name))
								case "x-opencode-request":
									require.Regexp(t, `^msg_[0-9a-f]{12}[0-9A-Za-z]{14}$`, got.header.Get(name))
								case "x-opencode-project":
									require.Equal(t, "global", got.header.Get(name))
								}
							}
						} else {
							require.Equal(t, value, got.header.Get(name))
						}
					}
				})
			}
		}
	}
}

func TestMissingClientIdentifiersRemainStableAcrossRetries(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	adaptor := &Adaptor{}
	first := make(http.Header)
	require.NoError(t, adaptor.SetupRequestHeader(c, &first, newRelayInfo("mimo-v2.5-free")))
	retry := make(http.Header)
	require.NoError(t, adaptor.SetupRequestHeader(c, &retry, newGoRelayInfo("kimi-k3")))
	for _, name := range []string{"x-opencode-session", "x-opencode-request", "x-opencode-project"} {
		require.NotEmpty(t, first.Get(name))
		require.Equal(t, first.Get(name), retry.Get(name))
	}
	otherContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	otherContext.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	other := make(http.Header)
	require.NoError(t, adaptor.SetupRequestHeader(otherContext, &other, newRelayInfo("mimo-v2.5-free")))
	require.NotEqual(t, first.Get("x-opencode-session"), other.Get("x-opencode-session"))
	require.NotEqual(t, first.Get("x-opencode-request"), other.Get("x-opencode-request"))
}

func TestPartiallyMissingClientIdentifiers(t *testing.T) {
	for _, missing := range []string{"x-opencode-session", "x-opencode-request", "x-opencode-project"} {
		t.Run(missing, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			for _, name := range []string{"x-opencode-session", "x-opencode-request", "x-opencode-project"} {
				if name != missing {
					c.Request.Header.Set(name, "original-"+name)
				}
			}
			header := make(http.Header)
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &header, newRelayInfo("mimo-v2.5-free")))
			require.NotEmpty(t, header.Get(missing))
			for name, values := range c.Request.Header {
				require.Equal(t, values[0], header.Get(name))
			}
		})
	}
}
