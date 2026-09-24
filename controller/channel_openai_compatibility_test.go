package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIChannelTestUsesModelCapabilities(t *testing.T) {
	for _, name := range []string{"o3", "o3-mini-2025-01-31-high", "gpt-5.5", "gpt-6-astra-max"} {
		require.True(t, useOpenAICompletionTokens(name, constant.ChannelTypeOpenAI), name)
	}
	for _, name := range []string{"ollama", "orcarouter/model", "openrouter/auto", "gpt-5-unknown"} {
		require.False(t, useOpenAICompletionTokens(name, constant.ChannelTypeOpenAI), name)
	}
	require.False(t, useOpenAICompletionTokens("gpt-6-astra", constant.ChannelTypeModal))
	require.False(t, useOpenAICompletionTokens("gpt-5", constant.ChannelTypeAzure))
	require.True(t, useOpenAICompletionTokens("o3", constant.ChannelTypeAzure))
	for _, tc := range []struct{ model, fields, path, token string }{
		{"gpt-6-astra", `,"max_tokens":16`, "/v1/chat/completions", "max_completion_tokens"},
		{"gpt-5.1-codex-max", `,"max_tokens":16`, "/v1/responses", "max_output_tokens"},
		{"gpt-6-astra", `,"max_tokens":64,"max_completion_tokens":0,"tools":[{"type":"function","function":{"name":"test"}}]`, "/v1/responses", "max_output_tokens"},
	} {
		info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, RequestURLPath: "/v1/chat/completions", ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
		body, err := prepareOpenAIChannelTestRequest(info, []byte(`{"model":"`+tc.model+`","messages":[{"role":"user","content":"hi"}]`+tc.fields+`}`))
		require.NoError(t, err)
		require.Equal(t, tc.path, info.RequestURLPath)
		require.Equal(t, tc.model, info.UpstreamModelName)
		require.True(t, gjson.GetBytes(body, tc.token).Exists())
		require.False(t, gjson.GetBytes(body, "max_tokens").Exists())
	}
}

func TestOpenAIChannelTestRejectsUnrepresentableToolHistory(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
	_, err := prepareOpenAIChannelTestRequest(info, []byte(`{"model":"gpt-6-astra","messages":[{"role":"tool","content":"result"}]}`))
	require.ErrorContains(t, err, "messages[0].tool_call_id")
}
