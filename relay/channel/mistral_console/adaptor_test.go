package mistralconsole

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequestBuildsBoraPayload(t *testing.T) {
	name := "alice"
	temperature := 0.0
	topP := 1.0
	maxTokens := uint(2048)
	strict := true
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ApiKey:            `ory_session_test="session"`,
		UpstreamModelName: "glm-5-2",
	}}
	request := &dto.GeneralOpenAIRequest{
		Model:           "client-model",
		Temperature:     &temperature,
		TopP:            &topP,
		MaxTokens:       &maxTokens,
		ReasoningEffort: "max",
		Messages: []dto.Message{
			{Role: "system", Content: "Follow instructions."},
			{Role: "user", Name: &name, Content: []any{
				map[string]any{"type": "text", "text": "Hello"},
				map[string]any{"type": "text", "text": " world"},
			}},
			{Role: "assistant", Content: "Hi!"},
		},
		Tools: []dto.ToolCallRequest{
			{Type: "code_interpreter"},
			{Type: "image_generation"},
			{Type: "web_search"},
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        "get_time",
					Description: "Get current time",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"timezone": map[string]any{"type": "string"},
						},
					},
					Strict: &strict,
				},
			},
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	payload, ok := converted.(*boraConversationRequest)
	require.True(t, ok)
	require.Equal(t, "glm-5-2", payload.Model)
	require.True(t, payload.Stream)
	require.Equal(t, "high", payload.CompletionArgs.ReasoningEffort)
	require.Equal(t, uint(2048), *payload.CompletionArgs.MaxTokens)

	data, err := common.Marshal(payload)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"glm-5-2",
		"instructions":"[system]\nFollow instructions.",
		"completion_args":{
			"temperature":0,
			"max_tokens":2048,
			"top_p":1,
			"reasoning_effort":"high"
		},
		"tools":[
			{"type":"code_interpreter"},
			{"type":"image_generation"},
			{"type":"web_search_premium"},
			{"type":"function","function":{
				"name":"get_time",
				"description":"Get current time",
				"parameters":{"type":"object","properties":{"timezone":{"type":"string"}}},
				"strict":true
			}}
		],
		"stream":true,
		"inputs":[
			{"object":"entry","type":"message.input","role":"user","content":"[user:alice]\nHello world","prefix":false},
			{"object":"entry","type":"message.output","role":"assistant","content":"Hi!"}
		]
	}`, string(data))
}

func TestConvertOpenAIRequestMapsFunctionCallHistory(t *testing.T) {
	prefix := true
	toolCalls, err := common.Marshal([]dto.ToolCallRequest{{
		ID:   "call-1",
		Type: "function",
		Function: dto.FunctionRequest{
			Name:      "get_time",
			Arguments: `{"timezone":"Asia/Shanghai"}`,
		},
	}})
	require.NoError(t, err)
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "user", Content: "What time is it?"},
		{
			Role:             "assistant",
			Content:          nil,
			Prefix:           &prefix,
			ReasoningContent: "private reasoning content",
			Reasoning:        "private reasoning alias",
			ToolCalls:        toolCalls,
		},
		{Role: "tool", ToolCallId: "call-1", Content: `{"time":"17:30"}`},
	}}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, testRelayInfo(false), request)
	require.NoError(t, err)
	payload := converted.(*boraConversationRequest)
	data, err := common.Marshal(payload.Inputs)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private reasoning content")
	require.NotContains(t, string(data), "private reasoning alias")
	require.NotContains(t, string(data), `"prefix":true`)
	require.JSONEq(t, `[
		{"object":"entry","type":"message.input","role":"user","content":"What time is it?","prefix":false},
		{"object":"entry","type":"function.call","name":"get_time","tool_call_id":"call-1","arguments":"{\"timezone\":\"Asia/Shanghai\"}"},
		{"object":"entry","type":"function.result","tool_call_id":"call-1","result":"{\"time\":\"17:30\"}"}
	]`, string(data))
}

func TestConvertOpenAIRequestIgnoresReasoningMetadataAndKeepsVisibleHistory(t *testing.T) {
	prefix := true
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{
		{Role: "user", Content: "Question"},
		{
			Role:             "assistant",
			Content:          "Visible answer",
			Prefix:           &prefix,
			ReasoningContent: "hidden chain of thought",
			Reasoning:        "hidden reasoning alias",
		},
		{Role: "user", Content: "Continue"},
	}}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, testRelayInfo(false), request)
	require.NoError(t, err)
	payload := converted.(*boraConversationRequest)
	data, err := common.Marshal(payload.Inputs)
	require.NoError(t, err)
	require.NotContains(t, string(data), "hidden chain of thought")
	require.NotContains(t, string(data), "hidden reasoning alias")
	require.NotContains(t, string(data), `"prefix":true`)
	require.JSONEq(t, `[
		{"object":"entry","type":"message.input","role":"user","content":"Question","prefix":false},
		{"object":"entry","type":"message.output","role":"assistant","content":"Visible answer"},
		{"object":"entry","type":"message.input","role":"user","content":"Continue","prefix":false}
	]`, string(data))
}

func TestConvertOpenAIRequestMaxTokensAndToolChoice(t *testing.T) {
	zero := uint(0)
	tooLarge := uint(defaultBoraMaxTokens + 100)
	tests := []struct {
		name     string
		request  *dto.GeneralOpenAIRequest
		expected uint
		tools    int
	}{
		{
			name:     "large default",
			request:  &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "hi"}}},
			expected: defaultBoraMaxTokens,
			tools:    3,
		},
		{
			name:     "explicit zero preserved",
			request:  &dto.GeneralOpenAIRequest{MaxCompletionTokens: &zero, Messages: []dto.Message{{Role: "user", Content: "hi"}}},
			expected: 0,
			tools:    3,
		},
		{
			name:     "oversized value clamped",
			request:  &dto.GeneralOpenAIRequest{MaxTokens: &tooLarge, Messages: []dto.Message{{Role: "user", Content: "hi"}}},
			expected: defaultBoraMaxTokens,
			tools:    3,
		},
		{
			name: "none keeps forced built-ins",
			request: &dto.GeneralOpenAIRequest{
				Messages:   []dto.Message{{Role: "user", Content: "hi"}},
				Tools:      []dto.ToolCallRequest{{Type: "code_interpreter"}},
				ToolChoice: "none",
			},
			expected: defaultBoraMaxTokens,
			tools:    3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, testRelayInfo(false), test.request)
			require.NoError(t, err)
			payload := converted.(*boraConversationRequest)
			require.Equal(t, test.expected, *payload.CompletionArgs.MaxTokens)
			require.Equal(t, test.tools, len(payload.Tools))
			require.Equal(t, "code_interpreter", payload.Tools[0].Type)
			require.Equal(t, "image_generation", payload.Tools[1].Type)
			require.Equal(t, "web_search_premium", payload.Tools[2].Type)
		})
	}
}

func TestConvertOpenAIRequestRejectsUnsupportedContent(t *testing.T) {
	info := testRelayInfo(false)
	tests := []struct {
		name    string
		request *dto.GeneralOpenAIRequest
	}{
		{
			name: "image content",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: []any{
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}},
			}}}},
		},
		{
			name: "unknown tool",
			request: &dto.GeneralOpenAIRequest{
				Messages: []dto.Message{{Role: "user", Content: "hello"}},
				Tools:    []dto.ToolCallRequest{{Type: "file_search"}},
			},
		},
		{
			name:    "function role",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "function", Content: "result"}}},
		},
		{
			name:    "tool without id",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "tool", Content: "result"}}},
		},
		{
			name:    "empty",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: ""}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, test.request)
			require.Error(t, err)
			require.NotContains(t, err.Error(), ChannelName)
			var apiErr *types.NewAPIError
			require.True(t, errors.As(err, &apiErr))
			require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			require.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
		})
	}
}

func TestSetupRequestHeaderUsesCookieOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := testRelayInfo(false)
	headers := http.Header{"Authorization": []string{"Bearer stale"}}

	err := (&Adaptor{}).SetupRequestHeader(ctx, &headers, info)
	require.NoError(t, err)
	require.Equal(t, info.ApiKey, headers.Get("Cookie"))
	require.Equal(t, "text/event-stream", headers.Get("Accept"))
	require.Equal(t, "application/json", headers.Get("Content-Type"))
	require.Empty(t, headers.Get("Authorization"))
	require.NotContains(t, info.ToString(), info.ApiKey)
}

func TestSetupRequestHeaderAddsPrefixToBareSessionValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rawSession := "MTc4NjQ2ODcyNnxTbGtaRlgxdWRDczBwTkYwVTZXLUlBQkdPQlRsOUhVc1NUNzJKX0F0SWxqU2tfYnItYklWTUJkSGt0dFgtT2Ffa3llTlhUNXZ5ZUIzWkFXa0UyZDRkeUgteGlrQlhKbFlXUW9uMGZYWWxXQi0tcHZkY0FZZ0t2OHR6dXZ4Sw=="
	info := testRelayInfo(false)
	info.ApiKey = rawSession
	headers := http.Header{}

	err := (&Adaptor{}).SetupRequestHeader(ctx, &headers, info)
	require.NoError(t, err)
	require.Equal(t, boraSessionCookieName+`="`+rawSession+`"`, headers.Get("Cookie"))
}

func TestSetupRequestHeaderRejectsInvalidCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	ctx.Request, _ = http.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	for _, cookie := range []string{"", "Cookie: session=value", "session=value", "session=value\r\nX-Test: bad"} {
		info := testRelayInfo(false)
		info.ApiKey = cookie
		err := (&Adaptor{}).SetupRequestHeader(ctx, &http.Header{}, info)
		require.Error(t, err)
		require.NotContains(t, err.Error(), ChannelName)
	}
}

func testRelayInfo(stream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream: stream,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            `ory_session_test="session"`,
			UpstreamModelName: "glm-5-2",
		},
	}
}
