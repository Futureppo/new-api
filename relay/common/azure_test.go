package common

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestAzureDeploymentCapabilities(t *testing.T) {
	for _, model := range []string{"gpt-6-luna", "gpt-6-sol", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-sol", "gpt-6.2-astra", "gpt-6.2-sol-2026-11-17"} {
		t.Run(model, func(t *testing.T) {
			info := &RelayInfo{OriginModelName: "public-alias", ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeAzure,
				UpstreamModelName: "deployment.v2-high", ChannelOtherSettings: dto.ChannelOtherSettings{AzureDeploymentModels: map[string]string{"deployment.v2-high": model}}}}
			for _, fields := range []string{`"max_tokens":0`, `"max_tokens":100,"max_completion_tokens":0`} {
				body, id, needResponses, err := NormalizeAzureRequest(info, []byte(`{"model":"deployment.v2-high",`+fields+`,"temperature":0,"presence_penalty":1,"custom":false}`), false)
				require.NoError(t, err)
				require.Equal(t, "deployment.v2-high", id)
				require.Equal(t, id, gjson.GetBytes(body, "model").String())
				require.Equal(t, "0", gjson.GetBytes(body, "max_completion_tokens").Raw)
				for _, field := range []string{"max_tokens", "temperature", "presence_penalty", "reasoning_effort"} {
					require.False(t, gjson.GetBytes(body, field).Exists(), field)
				}
				require.Equal(t, "false", gjson.GetBytes(body, "custom").Raw)
				require.False(t, needResponses)
			}
		})
	}
}

func TestAzureToolsAndOverrides(t *testing.T) {
	info := &RelayInfo{OriginModelName: "gpt-6.1-terra", ModelMappingTargetName: "dep", ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeAzure, UpstreamModelName: "dep"}}
	for _, tc := range []struct {
		name, fields                        string
		responses, wantResponses, wantError bool
	}{
		{"tools default", `"tools":[{"type":"function","function":{"name":"f"}}]`, false, true, false},
		{"tools without reasoning", `"tools":[{"type":"function","function":{"name":"f"}}],"reasoning_effort":"none","temperature":0`, false, false, false},
		{"max effort", `"reasoning_effort":"max"`, false, true, false},
		{"invalid effort", `"reasoning_effort":"minimal"`, false, false, true},
		{"native responses", `"input":"hi","max_output_tokens":0,"reasoning":{"effort":"low"}`, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _, needResponses, err := NormalizeAzureRequest(info, []byte(`{"model":"dep",`+tc.fields+`}`), tc.responses)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantResponses, needResponses)
			if tc.name == "tools without reasoning" {
				require.Equal(t, "0", gjson.GetBytes(body, "temperature").Raw)
			}
			if tc.responses {
				require.Equal(t, "0", gjson.GetBytes(body, "max_output_tokens").Raw)
			}
		})
	}
	// A model override may remove the original reasoning model restrictions.
	body := []byte(`{"model":"gpt-4o","max_tokens":0,"temperature":0}`)
	out, _, _, err := NormalizeAzureRequest(info, body, false)
	require.NoError(t, err)
	require.Equal(t, body, out)
	info.UpstreamModelName = "overridden-deployment"
	require.Equal(t, "overridden-deployment", AzureModelName(info, info.UpstreamModelName))
	info.UpstreamModelName = "dep"
	info.ChannelOtherSettings.AzureDeploymentModels = map[string]string{"dep": "gpt-6.2-sol"}
	_, _, _, err = NormalizeAzureRequest(info, []byte(`{"model":"dep","reasoning_effort":"none"}`), false)
	require.ErrorContains(t, err, "does not support")
}

func TestAzureModelMetadataPrecedence(t *testing.T) {
	info := &RelayInfo{OriginModelName: "gpt-6.2-sol", ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeAzure, UpstreamModelName: "gpt-4o",
		ChannelOtherSettings: dto.ChannelOtherSettings{AzureDeploymentModels: map[string]string{"gpt-4o": "claude-opus-5"}}}}
	require.True(t, IsAzureClaude(info))
	require.Equal(t, "other-deployment", AzureModelName(info, "other-deployment"))
}
