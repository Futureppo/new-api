package xai

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertOpenAIRequestRejectsVideoModelOnChatEndpoint(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-imagine-video"},
	}
	request := &dto.GeneralOpenAIRequest{Model: "grok-imagine-video"}

	converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)

	require.Nil(t, converted)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Contains(t, apiErr.Error(), "/v1/videos")
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityDisabledKeepsRequest(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	info := newXAICodexCompatRelayInfo(false, relayconstant.RelayModeResponses)
	req := dto.OpenAIResponsesRequest{
		Model:        "grok-4",
		Instructions: mustRawMessage(t, "system"),
		Conversation: mustRawMessage(t, "conv_123"),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	out := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, req.Instructions, out.Instructions)
	require.Equal(t, req.Conversation, out.Conversation)
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityRequiresUserAgent(t *testing.T) {
	c := newXAICodexCompatTestContext("curl/8.0")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	req := dto.OpenAIResponsesRequest{
		Model:        "grok-4",
		Instructions: mustRawMessage(t, "system"),
		Conversation: mustRawMessage(t, "conv_123"),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	out := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, req.Instructions, out.Instructions)
	require.Equal(t, req.Conversation, out.Conversation)
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityMovesInstructionsIntoInput(t *testing.T) {
	c := newXAICodexCompatTestContext("Codex CLI")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4",
		Input: mustRawMessage(t, []map[string]any{
			{"role": "user", "content": "hello"},
		}),
		Instructions: mustRawMessage(t, "be precise"),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body := marshalConvertedResponseRequest(t, converted)

	require.False(t, gjson.GetBytes(body, "instructions").Exists())
	require.Equal(t, "system", gjson.GetBytes(body, "input.0.role").String())
	require.Equal(t, "be precise", gjson.GetBytes(body, "input.0.content").String())
	require.Equal(t, "user", gjson.GetBytes(body, "input.1.role").String())
	require.Equal(t, "hello", gjson.GetBytes(body, "input.1.content").String())
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityNormalizesToolsAndToolChoice(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4",
		Tools: mustRawMessage(t, []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup",
					"description": "Lookup data",
					"parameters": map[string]any{
						"type": "object",
					},
				},
			},
			{"type": "web_search_preview", "search_context_size": "low"},
			{"type": "file_search"},
		}),
		ToolChoice: mustRawMessage(t, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "lookup"},
		}),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body := marshalConvertedResponseRequest(t, converted)

	require.Equal(t, int64(2), gjson.GetBytes(body, "tools.#").Int())
	require.Equal(t, "function", gjson.GetBytes(body, "tools.0.type").String())
	require.Equal(t, "lookup", gjson.GetBytes(body, "tools.0.name").String())
	require.False(t, gjson.GetBytes(body, "tools.0.function").Exists())
	require.Equal(t, "web_search", gjson.GetBytes(body, "tools.1.type").String())
	require.Equal(t, "function", gjson.GetBytes(body, "tool_choice.type").String())
	require.Equal(t, "lookup", gjson.GetBytes(body, "tool_choice.name").String())
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityNormalizesInputItems(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4",
		Input: mustRawMessage(t, []any{
			map[string]any{
				"type":    "reasoning",
				"id":      "rs_123",
				"summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}},
			},
			map[string]any{
				"type": "message",
				"role": "developer",
				"content": []any{
					map[string]any{"type": "input_text", "text": "developer note"},
				},
			},
			map[string]any{
				"type": "message",
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "output_text", "text": "previous answer"},
				},
			},
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_fn",
				"name":      "lookup",
				"arguments": map[string]any{"query": "xai"},
				"status":    "completed",
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_fn",
				"output":  map[string]any{"result": "ok"},
			},
			map[string]any{
				"type":    "computer_call_output",
				"call_id": "call_computer",
				"output": map[string]any{
					"type":      "input_image",
					"image_url": "data:image/png;base64,AAAA",
				},
			},
			map[string]any{
				"type":    "local_shell_call_output",
				"call_id": "call_shell",
				"output":  "shell ok",
			},
			map[string]any{
				"type":    "computer_call",
				"call_id": "call_computer",
				"action":  map[string]any{"type": "click", "x": 1, "y": 2},
			},
			map[string]any{
				"type":   "image_generation_call",
				"id":     "ig_123",
				"status": "completed",
			},
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "next question"},
				},
			},
		}),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body := marshalConvertedResponseRequest(t, converted)

	require.Equal(t, int64(6), gjson.GetBytes(body, "input.#").Int())
	require.Equal(t, "system", gjson.GetBytes(body, "input.0.role").String())
	require.Equal(t, "developer note", gjson.GetBytes(body, "input.0.content").String())
	require.Equal(t, "assistant", gjson.GetBytes(body, "input.1.role").String())
	require.Equal(t, "previous answer", gjson.GetBytes(body, "input.1.content").String())
	require.Equal(t, "function_call", gjson.GetBytes(body, "input.2.type").String())
	require.Equal(t, `{"query":"xai"}`, gjson.GetBytes(body, "input.2.arguments").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(body, "input.3.type").String())
	require.Equal(t, `{"result":"ok"}`, gjson.GetBytes(body, "input.3.output").String())
	require.Equal(t, "user", gjson.GetBytes(body, "input.4.role").String())
	require.Contains(t, gjson.GetBytes(body, "input.4.content").String(), "local_shell_call_output call_shell output:")
	require.Equal(t, "user", gjson.GetBytes(body, "input.5.role").String())
	require.Equal(t, "next question", gjson.GetBytes(body, "input.5.content").String())
	require.NotContains(t, string(body), "computer_call")
	require.NotContains(t, string(body), "image_generation_call")
	require.NotContains(t, string(body), "reasoning")
	require.NotContains(t, string(body), "data:image/png")
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityDeletesUnsupportedFieldsAndPreservesZeroValues(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	zeroUint := uint(0)
	zeroInt := 0
	zeroFloat := 0.0
	streamFalse := false
	req := dto.OpenAIResponsesRequest{
		Model:                "grok-4",
		Include:              mustRawMessage(t, []string{"reasoning.encrypted_content", "file_search_call.results"}),
		Conversation:         mustRawMessage(t, "conv_123"),
		ContextManagement:    mustRawMessage(t, map[string]any{"mode": "auto"}),
		MaxOutputTokens:      &zeroUint,
		TopLogProbs:          &zeroInt,
		Temperature:          &zeroFloat,
		TopP:                 &zeroFloat,
		Store:                mustRawMessage(t, false),
		Stream:               &streamFalse,
		PromptCacheKey:       mustRawMessage(t, "cache-key"),
		PromptCacheRetention: mustRawMessage(t, "24h"),
		StreamOptions:        &dto.StreamOptions{IncludeObfuscation: true},
		Prompt:               mustRawMessage(t, map[string]any{"id": "pmpt_123"}),
		EnableThinking:       mustRawMessage(t, true),
		Preset:               mustRawMessage(t, "sonar"),
		ServiceTier:          "flex",
		MaxToolCalls:         &zeroUint,
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body := marshalConvertedResponseRequest(t, converted)

	require.Equal(t, int64(1), gjson.GetBytes(body, "include.#").Int())
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(body, "include.0").String())
	require.False(t, gjson.GetBytes(body, "conversation").Exists())
	require.False(t, gjson.GetBytes(body, "context_management").Exists())
	require.False(t, gjson.GetBytes(body, "prompt_cache_retention").Exists())
	require.False(t, gjson.GetBytes(body, "stream_options").Exists())
	require.False(t, gjson.GetBytes(body, "prompt").Exists())
	require.False(t, gjson.GetBytes(body, "enable_thinking").Exists())
	require.False(t, gjson.GetBytes(body, "preset").Exists())
	require.False(t, gjson.GetBytes(body, "service_tier").Exists())
	require.False(t, gjson.GetBytes(body, "max_tool_calls").Exists())
	require.True(t, gjson.GetBytes(body, "max_output_tokens").Exists())
	require.True(t, gjson.GetBytes(body, "top_logprobs").Exists())
	require.True(t, gjson.GetBytes(body, "temperature").Exists())
	require.True(t, gjson.GetBytes(body, "top_p").Exists())
	require.True(t, gjson.GetBytes(body, "store").Exists())
	require.True(t, gjson.GetBytes(body, "stream").Exists())
	require.True(t, gjson.GetBytes(body, "prompt_cache_key").Exists())
	require.False(t, gjson.GetBytes(body, "store").Bool())
	require.False(t, gjson.GetBytes(body, "stream").Bool())
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityDeletesToolChoiceWhenToolRemoved(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	req := dto.OpenAIResponsesRequest{
		Model: "grok-4",
		Tools: mustRawMessage(t, []map[string]any{
			{"type": "file_search"},
		}),
		ToolChoice: mustRawMessage(t, map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "lookup"},
		}),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body := marshalConvertedResponseRequest(t, converted)

	require.False(t, gjson.GetBytes(body, "tools").Exists())
	require.False(t, gjson.GetBytes(body, "tool_choice").Exists())
}

func TestConvertOpenAIResponsesRequestCodexCompatibilityCompactKeepsOnlyModelAndInput(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponsesCompact)
	req := dto.OpenAIResponsesRequest{
		Model:              "grok-4",
		Input:              mustRawMessage(t, "hello"),
		Instructions:       mustRawMessage(t, "be precise"),
		PreviousResponseID: "resp_123",
		Store:              mustRawMessage(t, false),
	}

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, req)
	require.NoError(t, err)
	body := marshalConvertedResponseRequest(t, converted)

	require.Equal(t, "grok-4", gjson.GetBytes(body, "model").String())
	require.Equal(t, "system", gjson.GetBytes(body, "input.0.role").String())
	require.Equal(t, "be precise", gjson.GetBytes(body, "input.0.content").String())
	require.Equal(t, "user", gjson.GetBytes(body, "input.1.role").String())
	require.Equal(t, "hello", gjson.GetBytes(body, "input.1.content").String())
	require.False(t, gjson.GetBytes(body, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(body, "store").Exists())
}

func TestSetupRequestHeaderCodexCompatibilityMapsSessionID(t *testing.T) {
	c := newXAICodexCompatTestContext("codex-cli")
	c.Request.Header.Set("Session_id", "sess-123")
	info := newXAICodexCompatRelayInfo(true, relayconstant.RelayModeResponses)
	info.ApiKey = "xai-key"

	header := http.Header{}
	err := (&Adaptor{}).SetupRequestHeader(c, &header, info)

	require.NoError(t, err)
	require.Equal(t, "Bearer xai-key", header.Get("Authorization"))
	require.Equal(t, "sess-123", header.Get(xaiGrokConversationID))
}

func newXAICodexCompatTestContext(userAgent string) *gin.Context {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("User-Agent", userAgent)
	return c
}

func newXAICodexCompatRelayInfo(enabled bool, relayMode int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayMode,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "grok-4",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				XAICodexCompatibilityEnabled: enabled,
			},
		},
	}
}

func mustRawMessage(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

func marshalConvertedResponseRequest(t *testing.T, converted any) []byte {
	t.Helper()
	raw, err := common.Marshal(converted)
	require.NoError(t, err)
	return raw
}
