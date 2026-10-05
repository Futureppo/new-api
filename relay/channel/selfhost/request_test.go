package selfhost

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeDeploymentURL(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/", "/v1/chat/completions", "/v1/responses", "/v1/models", "/chat/completions"} {
		base := "https://host.example/prefix" + suffix
		require.NoError(t, ValidateBaseURL(base))
		require.Equal(t, "https://host.example/prefix", NormalizeBaseURL(base))
	}
	for _, base := range []string{"", "/v1", "ftp://example.com", "http://user:pass@example.com", "http://example.com?x=1", "http://example.com#fragment"} {
		require.Error(t, ValidateBaseURL(base), base)
	}
}

func TestFinalRequestCompatibility(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeLiteLLM} {
		for _, model := range []string{"Qwen3.8-Flash-Next-FP8", "qwen3.8-27b-fp8", "org/Qwen3.8-27B"} {
			for _, responses := range []bool{false, true} {
				body := `{"model":"` + model + `","temperature":0,"parallel_tool_calls":false,"unknown":{"flag":false},"allowed_openai_params":["tools","tools","reasoning_effort"],"reasoning_effort":"high","reasoning":{"effort":"high","summary":"auto"},"tools":[],"tool_choice":"auto"}`
				out, gotModel, effort, err := NormalizeRequest([]byte(body), channelType, responses)
				require.NoError(t, err)
				require.Equal(t, model, gotModel)
				require.Equal(t, "medium", effort)
				path := "reasoning_effort"
				if responses {
					path = "reasoning.effort"
					require.Equal(t, "auto", gjson.GetBytes(out, "reasoning.summary").String())
				}
				require.Equal(t, "medium", gjson.GetBytes(out, path).String())
				require.Equal(t, "0", gjson.GetBytes(out, "temperature").Raw)
				require.Equal(t, "false", gjson.GetBytes(out, "parallel_tool_calls").Raw)
				require.Equal(t, "false", gjson.GetBytes(out, "unknown.flag").Raw)
				require.False(t, gjson.GetBytes(out, "tools").Exists())
				require.False(t, gjson.GetBytes(out, "tool_choice").Exists())
				if channelType == constant.ChannelTypeVLLM {
					require.False(t, gjson.GetBytes(out, "allowed_openai_params").Exists())
				} else if !responses {
					require.JSONEq(t, `["tools","reasoning_effort"]`, gjson.GetBytes(out, "allowed_openai_params").Raw)
				}
			}
		}
	}
	for _, effort := range []string{"none", "low", "medium", "xhigh", "minimal"} {
		out, _, got, err := NormalizeRequest([]byte(`{"model":"qwen3.8-27b","reasoning_effort":"`+effort+`"}`), constant.ChannelTypeLiteLLM, false)
		require.NoError(t, err)
		require.Equal(t, effort, got)
		require.Equal(t, effort, gjson.GetBytes(out, "reasoning_effort").String())
	}
	out, _, effort, err := NormalizeRequest([]byte(`{"model":"other-qwen3.8-model","reasoning_effort":"high"}`), constant.ChannelTypeLiteLLM, false)
	require.NoError(t, err)
	require.Equal(t, "high", effort)
	require.False(t, gjson.GetBytes(out, "allowed_openai_params").Exists())
}

func TestExtraBodyAndToolValidation(t *testing.T) {
	body := `{"model":"qwen3.8-27b","temperature":0,"extra_body":{"temperature":1,"ignore_eos":false,"min_p":0,"chat_template_kwargs":{"enable_thinking":false},"reasoning_effort":"high"},"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","properties":{"flag":{"const":false}}}}}],"tool_choice":"required"}`
	out, _, effort, err := NormalizeRequest([]byte(body), constant.ChannelTypeVLLM, false)
	require.NoError(t, err)
	require.Equal(t, "medium", effort)
	for path, want := range map[string]string{"temperature": "0", "min_p": "0", "ignore_eos": "false", "chat_template_kwargs.enable_thinking": "false", "tools.0.function.parameters.properties.flag.const": "false"} {
		require.Equal(t, want, gjson.GetBytes(out, path).Raw)
	}
	require.False(t, gjson.GetBytes(out, "extra_body").Exists())
	for _, fields := range []string{`"tool_choice":"required"`, `"tool_choice":{"type":"function","function":{"name":"f"}}`, `"tools":{}`, `"extra_body":[]`, `"reasoning_effort":123`} {
		_, _, _, err := NormalizeRequest([]byte(`{"model":"qwen3.8-27b",`+fields+`}`), constant.ChannelTypeVLLM, false)
		require.Error(t, err, fields)
	}
}
