package mistralconsole

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/stretchr/testify/require"
)

func TestBoraFunctionNameMapperAliasesAndRestores(t *testing.T) {
	mapper := newBoraFunctionNameMapper()
	names := []string{
		"web_search",
		"open_url",
		"name.with spaces",
		"name/with spaces",
		strings.Repeat("very_long_function_name_", 5),
		"mc_fn_already_prefixed",
	}
	aliases := make(map[string]string)
	for _, name := range names {
		alias := mapper.alias(name)
		require.NotEqual(t, name, alias)
		require.LessOrEqual(t, len(alias), boraFunctionNameMaxLength)
		require.Regexp(t, `^[A-Za-z0-9_-]+$`, alias)
		require.Equal(t, alias, mapper.alias(name))
		require.Equal(t, name, mapper.original(alias))
		aliases[alias] = name
	}
	require.Len(t, aliases, len(names))
	require.Equal(t, "unknown_upstream_name", mapper.original("unknown_upstream_name"))
}

func TestConvertOpenAIRequestAliasesToolsHistoryAndNamedToolChoice(t *testing.T) {
	disabled := false
	info := testRelayInfo(false)
	info.ChannelOtherSettings = dto.ChannelOtherSettings{
		MistralConsoleCodeInterpreterEnabled: &disabled,
		MistralConsoleImageGenerationEnabled: &disabled,
		MistralConsoleWebSearchEnabled:       &disabled,
	}
	toolCalls, err := common.Marshal([]dto.ToolCallRequest{{
		ID:   "call-search",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "web_search",
			Arguments: `{"query":"Bora"}`,
		},
	}})
	require.NoError(t, err)

	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "Search for Bora"},
			{Role: "assistant", ToolCalls: toolCalls},
			{Role: "tool", ToolCallId: "call-search", Content: `{"result":"ok"}`},
			{Role: "user", Content: "Continue"},
		},
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "web_search"}},
			{Type: "function", Function: dto.FunctionRequest{Name: "open_url"}},
		},
		ToolChoice: map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "web_search"},
		},
	}

	adaptor := &Adaptor{}
	converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	payload := converted.(*boraConversationRequest)
	require.Len(t, payload.Tools, 1)
	require.NotNil(t, payload.Tools[0].Function)
	alias := payload.Tools[0].Function.Name
	require.NotEqual(t, "web_search", alias)
	require.Equal(t, "web_search", adaptor.restoreFunctionName(alias))
	require.Equal(t, alias, payload.Inputs[1].Name)
	require.Contains(t, payload.Instructions, alias)
	require.NotContains(t, payload.Instructions, "function web_search")
	require.Equal(t, "call-search", payload.Inputs[1].ToolCallID)
	require.Equal(t, "call-search", payload.Inputs[2].ToolCallID)
}
