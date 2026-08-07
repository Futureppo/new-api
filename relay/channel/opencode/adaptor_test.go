package opencode

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    constant.ChannelBaseURLs[constant.ChannelTypeOpenCode],
			ChannelType:       constant.ChannelTypeOpenCode,
			UpstreamModelName: model,
		},
	}
}

func TestGetRequestURLRoutesModelsToRequiredEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		isStream bool
		want     string
	}{
		{name: "gpt responses", model: "gpt-5.6-sol", want: "https://opencode.ai/zen/v1/responses"},
		{name: "grok responses", model: "grok-4.5", want: "https://opencode.ai/zen/v1/responses"},
		{name: "claude messages", model: "claude-sonnet-5", want: "https://opencode.ai/zen/v1/messages"},
		{name: "qwen messages", model: "qwen3.6-plus", want: "https://opencode.ai/zen/v1/messages"},
		{name: "gemini", model: "gemini-3-flash", want: "https://opencode.ai/zen/v1/models/gemini-3-flash:generateContent"},
		{name: "gemini stream", model: "gemini-3-flash", isStream: true, want: "https://opencode.ai/zen/v1/models/gemini-3-flash:streamGenerateContent?alt=sse"},
		{name: "openai compatible", model: "deepseek-v4-flash", want: "https://opencode.ai/zen/v1/chat/completions"},
	}

	adaptor := &Adaptor{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := newRelayInfo(tt.model)
			info.IsStream = tt.isStream
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestGetRequestURLRejectsResponsesCompaction(t *testing.T) {
	info := newRelayInfo("gpt-5.6-sol")
	info.RelayMode = relayconstant.RelayModeResponsesCompact
	_, err := (&Adaptor{}).GetRequestURL(info)
	require.ErrorContains(t, err, "does not support responses compaction")
}

func TestSetupRequestHeaderUsesEndpointSpecificAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = &http.Request{Header: make(http.Header)}

	tests := []struct {
		name       string
		model      string
		headerName string
		headerWant string
	}{
		{name: "responses bearer", model: "gpt-5.6-sol", headerName: "Authorization", headerWant: "Bearer test-key"},
		{name: "chat bearer", model: "big-pickle", headerName: "Authorization", headerWant: "Bearer test-key"},
		{name: "messages api key", model: "claude-sonnet-5", headerName: "x-api-key", headerWant: "test-key"},
		{name: "gemini api key", model: "gemini-3-flash", headerName: "x-goog-api-key", headerWant: "test-key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := newRelayInfo(tt.model)
			info.ApiKey = "test-key"
			header := make(http.Header)
			err := (&Adaptor{}).SetupRequestHeader(c, &header, info)
			require.NoError(t, err)
			require.Equal(t, tt.headerWant, header.Get(tt.headerName))
		})
	}
}
