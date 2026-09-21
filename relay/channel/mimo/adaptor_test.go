package mimo

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestMiMoURLs(t *testing.T) {
	a := &Adaptor{}
	for _, base := range []string{"https://api.xiaomimimo.com", "https://proxy.example/mimo"} {
		for _, suffix := range []string{"", "/", "/v1", "/v1/", "/anthropic", "/anthropic/", "/anthropic/v1/"} {
			for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
				info := &relaycommon.RelayInfo{RelayFormat: format, RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base + suffix}}
				got, err := a.GetRequestURL(info)
				require.NoError(t, err)
				path := "/v1/chat/completions"
				if format == types.RelayFormatClaude {
					path = "/anthropic/v1/messages"
				}
				require.Equal(t, base+path, got)
			}
		}
	}
	require.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeMiMo], NormalizeBaseURL(""))
	_, err := a.GetRequestURL(&relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, RelayMode: relayconstant.RelayModeResponses})
	require.ErrorContains(t, err, "only supports")
	for _, base := range []string{"file:///tmp/mimo", "relative/path", "https://example.com?url=x"} {
		_, err := a.GetRequestURL(&relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base}})
		require.ErrorContains(t, err, "invalid MiMo")
	}
}

func TestMiMoRequestRoundTrip(t *testing.T) {
	for _, body := range []string{
		`{"model":"mimo-v2.6-pro-ultraspeed","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"mimo-v2.6-pro-ultraspeed","stream":false,"max_tokens":0,"max_completion_tokens":0,"temperature":0,"top_p":0,"frequency_penalty":0,"presence_penalty":0,"parallel_tool_calls":false,"thinking":{"type":"disabled"},"stream_options":{"include_usage":false},"messages":[{"role":"assistant","content":"","reasoning_content":"kept","tool_calls":[{"id":"call1","type":"function","function":{"name":"echo","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call1","content":"OK"}],"tools":[{"type":"function","function":{"name":"echo","parameters":{"type":"object"}}}],"tool_choice":"auto"}`,
	} {
		var request dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(body, &request))
		converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, nil, &request)
		require.NoError(t, err)
		data, err := common.Marshal(converted)
		require.NoError(t, err)
		require.JSONEq(t, body, string(data))
	}
	body := `{"model":"mimo-v2.6-pro-ultraspeed","max_tokens":0,"temperature":0,"stream":false,"thinking":{"type":"disabled"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"kept","signature":"sig"},{"type":"tool_use","id":"call1","name":"echo","input":{"value":0}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call1","content":"OK","is_error":false}]}]}`
	var request dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &request))
	converted, err := (&Adaptor{}).ConvertClaudeRequest(nil, nil, &request)
	require.NoError(t, err)
	data, err := common.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, body, string(data))
}
