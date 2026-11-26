package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAzureURLsAndHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, base, model, version, want string
		mode                             int
	}{
		{"root chat", "https://r.openai.azure.com/", "gpt-6.1-terra", "", "https://r.openai.azure.com/openai/deployments/dep.v2/chat/completions?api-version=2024-10-21", relayconstant.RelayModeChatCompletions},
		{"v1 chat", "https://r.cognitiveservices.azure.com/openai/v1/", "gpt-4o", "", "https://r.cognitiveservices.azure.com/openai/v1/chat/completions", relayconstant.RelayModeChatCompletions},
		{"responses root", "https://r.cognitiveservices.azure.com", "gpt-6-sol", "", "https://r.cognitiveservices.azure.com/openai/v1/responses", relayconstant.RelayModeResponses},
		{"responses v1", "https://r.openai.azure.com/openai/v1", "gpt-6-sol", "", "https://r.openai.azure.com/openai/v1/responses", relayconstant.RelayModeResponses},
		{"responses legacy", "https://r.openai.azure.com/openai", "gpt-6-sol", "2025-04-01-preview", "https://r.openai.azure.com/openai/responses?api-version=2025-04-01-preview", relayconstant.RelayModeResponses},
		{"compact", "https://proxy.invalid/prefix/openai/v1", "gpt-6-sol", "", "https://proxy.invalid/prefix/openai/v1/responses/compact", relayconstant.RelayModeResponsesCompact},
		{"claude cognitiveservices", "https://r.cognitiveservices.azure.com/openai/v1/", "claude-opus-5", "", "https://r.services.ai.azure.com/anthropic/v1/messages", relayconstant.RelayModeChatCompletions},
		{"claude openai", "https://r.openai.azure.com/", "claude-opus-5", "", "https://r.services.ai.azure.com/anthropic/v1/messages", relayconstant.RelayModeChatCompletions},
		{"claude sdk", "https://r.services.ai.azure.com/anthropic", "claude-opus-5", "", "https://r.services.ai.azure.com/anthropic/v1/messages", relayconstant.RelayModeChatCompletions},
		{"claude proxy", "https://proxy.invalid/prefix/openai/v1/", "claude-opus-5", "", "https://proxy.invalid/prefix/anthropic/v1/messages", relayconstant.RelayModeChatCompletions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: tc.model, ModelMappingTargetName: "dep.v2", RelayMode: tc.mode, RelayFormat: types.RelayFormatOpenAI, RequestURLPath: "/v1/chat/completions", ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeAzure, ChannelBaseUrl: tc.base, UpstreamModelName: "dep.v2", ApiKey: "test-key", ApiVersion: "2024-10-21",
				ChannelOtherSettings: dto.ChannelOtherSettings{AzureResponsesVersion: tc.version}}}
			adaptor := &Adaptor{}
			url, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, tc.want, url)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			h := http.Header{}
			require.NoError(t, adaptor.SetupRequestHeader(c, &h, info))
			if relaycommon.IsAzureClaude(info) {
				require.Equal(t, "test-key", h.Get("x-api-key"))
				require.Equal(t, "2023-06-01", h.Get("anthropic-version"))
				require.Empty(t, h.Get("api-key"))
			} else {
				require.Equal(t, "test-key", h.Get("api-key"))
			}
			require.Empty(t, h.Get("Authorization"))
		})
	}
}

func TestAzureClaudeConversionPreservesLiteralDeploymentAndZero(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "claude-opus-5", ChannelMeta: &relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeAzure, UpstreamModelName: "claude-opus-4-6-high",
		ChannelOtherSettings: dto.ChannelOtherSettings{AzureDeploymentModels: map[string]string{"claude-opus-4-6-high": "claude-opus-5"}},
	}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	out, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{
		Model: info.UpstreamModelName, MaxTokens: common.GetPointer(uint(32)), MaxCompletionTokens: common.GetPointer(uint(0)), Messages: []dto.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	request, ok := out.(*dto.ClaudeRequest)
	require.True(t, ok)
	require.Equal(t, "claude-opus-4-6-high", request.Model)
	require.EqualValues(t, 0, *request.MaxTokens)
	require.Nil(t, request.Thinking)
}
