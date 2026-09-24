package common

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIModelTokenLimits(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5", "gpt-5.2-2025-12-11", "o3-mini"} {
		for _, tc := range []struct {
			fields string
			want   string
		}{
			{``, ""}, {`,"max_tokens":0`, "0"}, {`,"max_tokens":123`, "123"},
			{`,"max_completion_tokens":0`, "0"}, {`,"max_completion_tokens":12`, "12"},
			{`,"max_tokens":456,"max_completion_tokens":12`, "12"},
			{`,"max_tokens":456,"max_completion_tokens":0`, "0"},
			{`,"max_tokens":0,"max_completion_tokens":null`, "0"},
		} {
			t.Run(model+tc.fields, func(t *testing.T) {
				body, name, _, err := NormalizeOpenAIRequest([]byte(`{"model":"`+model+`"`+tc.fields+`}`), false)
				require.NoError(t, err)
				require.Equal(t, model, name)
				require.False(t, gjson.GetBytes(body, "max_tokens").Exists())
				require.Equal(t, tc.want, gjson.GetBytes(body, "max_completion_tokens").Raw)
			})
		}
	}
}

func TestOpenAIModelSamplingAndTools(t *testing.T) {
	for _, tc := range []struct {
		model, effort     string
		remove, responses bool
	}{
		{"gpt-6-astra", "max", true, true},
		{"gpt-6-sol", "", true, true},
		{"gpt-6-luna", "none", false, false},
		{"gpt-5.1", "", false, false},
		{"gpt-5.2", "none", false, false},
		{"gpt-5.2", "xhigh", true, false},
		{"gpt-5", "minimal", true, false},
		{"o3", "", true, false},
		{"gpt-5.1-codex-max", "", false, true},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			request := map[string]any{"model": tc.model, "reasoning_effort": tc.effort, "temperature": 0, "top_p": 0, "logprobs": false, "top_logprobs": 0, "parallel_tool_calls": false, "override_extension": false, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "test"}}}}
			input, err := common.Marshal(request)
			require.NoError(t, err)
			body, _, responses, err := NormalizeOpenAIRequest(input, false)
			require.NoError(t, err)
			require.Equal(t, tc.responses, responses)
			for _, name := range []string{"temperature", "top_p", "logprobs", "top_logprobs"} {
				require.Equal(t, !tc.remove, gjson.GetBytes(body, name).Exists(), name)
			}
			require.Equal(t, "false", gjson.GetBytes(body, "parallel_tool_calls").Raw)
			require.Equal(t, "false", gjson.GetBytes(body, "override_extension").Raw)
		})
	}
	for _, messages := range []string{
		`[{"role":"assistant","tool_calls":[{"id":"call","type":"function","function":{"name":"test","arguments":"{}"}}]}]`,
		`[{"role":"tool","tool_call_id":"call","content":"ok"}]`,
		`[{"role":"function","name":"test","content":"ok"}]`,
	} {
		_, _, responses, err := NormalizeOpenAIRequest([]byte(`{"model":"gpt-6-astra","messages":`+messages+`}`), false)
		require.NoError(t, err)
		require.True(t, responses)
	}
	_, _, responses, err := NormalizeOpenAIRequest([]byte(`{"model":"gpt-6-astra","messages":[{"role":"user","content":"hi"}]}`), false)
	require.NoError(t, err)
	require.False(t, responses)
}

func TestOpenAIResponsesSamplingAndEffort(t *testing.T) {
	input := []byte(`{"model":"gpt-6-astra-max","reasoning":{"summary":"auto","mode":"pro"},"temperature":0,"top_p":0,"top_logprobs":0,"max_output_tokens":0,"include":["message.output_text.logprobs","reasoning.encrypted_content"],"store":false}`)
	body, model, responses, err := NormalizeOpenAIRequest(input, true)
	require.NoError(t, err)
	require.Equal(t, "gpt-6-astra", model)
	require.False(t, responses)
	require.Equal(t, "max", gjson.GetBytes(body, "reasoning.effort").String())
	require.Equal(t, "pro", gjson.GetBytes(body, "reasoning.mode").String())
	require.Equal(t, "auto", gjson.GetBytes(body, "reasoning.summary").String())
	require.JSONEq(t, `["reasoning.encrypted_content"]`, gjson.GetBytes(body, "include").Raw)
	require.Equal(t, "0", gjson.GetBytes(body, "max_output_tokens").Raw)
	require.Equal(t, "false", gjson.GetBytes(body, "store").Raw)
	for _, name := range []string{"temperature", "top_p", "top_logprobs"} {
		require.False(t, gjson.GetBytes(body, name).Exists())
	}
	for _, payload := range []string{
		`{"model":"gpt-6-astra","reasoning":{"effort":"none"}}`,
		`{"model":"gpt-6-sol","reasoning":{"effort":"minimal"}}`,
	} {
		_, _, _, err := NormalizeOpenAIRequest([]byte(payload), true)
		require.ErrorContains(t, err, "reasoning.effort")
		require.ErrorContains(t, err, "supported values")
	}
}

func TestOpenAIUnknownNamesRemainOpaque(t *testing.T) {
	for _, model := range []string{"ollama", "openrouter/auto", "orcarouter/model", "gpt-5-new-model", "gpt-6-astra-2099-01-01", "grok-4.6-high"} {
		input := []byte(`{"model":"` + model + `","max_tokens":0,"temperature":0,"reasoning_effort":"custom","messages":[{"role":"system","content":"hi"}]}`)
		body, name, responses, err := NormalizeOpenAIRequest(input, false)
		require.NoError(t, err)
		require.Equal(t, model, name)
		require.Equal(t, input, body)
		require.False(t, responses)
	}
}
