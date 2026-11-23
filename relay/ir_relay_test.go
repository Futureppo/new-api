package relay

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	awsChannel "github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relay/channel/opencode"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/ir"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setIRToggleForTest(t *testing.T, enabled bool) {
	t.Helper()
	prev := model_setting.IsIRConversionEnabled()
	val := "false"
	if enabled {
		val = "true"
	}
	require.NoError(t, config.UpdateConfigFromMap(model_setting.GetGlobalSettings(), map[string]string{
		"ir_conversion_enabled": val,
	}))
	t.Cleanup(func() {
		restore := "false"
		if prev {
			restore = "true"
		}
		_ = config.UpdateConfigFromMap(model_setting.GetGlobalSettings(), map[string]string{
			"ir_conversion_enabled": restore,
		})
	})
}

func newIRTestGinContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec
}

func TestIRCrossProtocolResponsesToClaudeStreamingAndUsage(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	var gotPath string
	var gotBody []byte
	var gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-api-key")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_upstream_1","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":80,"cache_read_input_tokens":20,"cache_creation_input_tokens":10,"output_tokens":0}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello from Claude!"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		IsStream:        true,
		OriginModelName: "claude-sonnet-4-5",
		ClientModelName: "claude-sonnet-4-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         42,
			ChannelType:       constant.ChannelTypeAnthropic,
			ApiType:           constant.APITypeAnthropic,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "sk-ant-test",
			UpstreamModelName: "claude-sonnet-4-5",
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-sonnet-4-5","stream":true,"instructions":"Be brief.","input":"Say hi"}`, &req))

	adaptor := &claude.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatOpenAIResponses, &req)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.Equal(t, "/v1/messages", gotPath)
	require.Equal(t, "sk-ant-test", gotAPIKey)
	require.Contains(t, string(gotBody), `"Be brief."`)
	require.Contains(t, string(gotBody), `"Say hi"`)

	// Client received canonical OpenAI Responses SSE frames.
	wire := rec.Body.String()
	require.Contains(t, wire, "event: response.created")
	require.Contains(t, wire, "event: response.output_text.delta")
	require.Contains(t, wire, "Hello from Claude!")
	require.Contains(t, wire, "event: response.completed")

	// Billing usage preserved exact Anthropic-exclusive semantics and cache fields!
	require.NotNil(t, usage)
	require.Equal(t, "anthropic", usage.UsageSemantic)
	require.Equal(t, 80, usage.PromptTokens)
	require.Equal(t, 12, usage.CompletionTokens)
	require.Equal(t, 20, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 10, usage.PromptTokensDetails.GetCacheCreationTokens())
	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
	// Client RelayFormat must remain untouched.
	require.Equal(t, types.RelayFormat(types.RelayFormatOpenAIResponses), info.RelayFormat)
}

func TestIRCrossProtocolClaudeToGeminiDirectWithoutDoubleHop(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"responseId": "gem_resp_1",
			"modelVersion": "gemini-2.5-pro",
			"candidates": [{
				"index": 0,
				"content": {
					"role": "model",
					"parts": [
						{"text": "Reasoning step", "thought": true},
						{"text": "Final answer from Gemini"}
					]
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 50, "candidatesTokenCount": 10, "thoughtsTokenCount": 5, "totalTokenCount": 65}
		}`)
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1/messages")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		RelayMode:       relayconstant.RelayModeUnknown,
		RequestURLPath:  "/v1/messages",
		IsStream:        false,
		OriginModelName: "gemini-2.5-pro",
		ClientModelName: "gemini-2.5-pro",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         77,
			ChannelType:       constant.ChannelTypeGemini,
			ApiType:           constant.APITypeGemini,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "gem-key",
			UpstreamModelName: "gemini-2.5-pro",
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-pro","max_tokens":512,"system":"Act as helper","messages":[{"role":"user","content":"Hello Gemini"}]}`, &req))

	adaptor := &gemini.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatAnthropic, &req)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.Contains(t, gotPath, "/models/gemini-2.5-pro:generateContent")
	require.Contains(t, string(gotBody), `"systemInstruction"`)
	require.Contains(t, string(gotBody), `"Hello Gemini"`)

	var claudeResp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &claudeResp))
	require.Equal(t, "message", claudeResp["type"])
	require.Contains(t, rec.Body.String(), "Final answer from Gemini")

	require.NotNil(t, usage)
	require.Equal(t, "openai", usage.UsageSemantic)
	require.Equal(t, 50, usage.PromptTokens)
	require.Equal(t, 15, usage.CompletionTokens)
	require.Equal(t, 5, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestIRCrossProtocolVertexGeminiAppliesShimAndAuthHeader(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	var gotAPIKey string
	var gotPath string
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.URL.Query().Get("key")
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"responseId": "v_1",
			"candidates": [{"index": 0, "content": {"role": "model", "parts": [{"text": "Vertex OK"}]}, "finishReason": "STOP"}],
			"usageMetadata": {"promptTokenCount": 20, "candidatesTokenCount": 5, "totalTokenCount": 25}
		}`)
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		IsStream:        false,
		OriginModelName: "gemini-2.5-pro",
		ClientModelName: "gemini-2.5-pro",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         88,
			ChannelType:       constant.ChannelTypeVertexAi,
			ApiType:           constant.APITypeVertexAi,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "vertex-express-key",
			UpstreamModelName: "gemini-2.5-pro",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				VertexKeyType: dto.VertexKeyTypeAPIKey,
			},
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-pro","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Ping Vertex"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"tool_a","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`, &req))

	adaptor := &vertex.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatOpenAIResponses, &req)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.Equal(t, "vertex-express-key", gotAPIKey)
	require.Contains(t, gotPath, ":generateContent")
	// VertexGeminiShim stripped functionResponse.id while injecting Gemini 3 dummy thoughtSignature on functionCall.
	require.Contains(t, string(gotBody), ir.GoogleDummyThoughtSignature)
	require.NotContains(t, string(gotBody), `"functionResponse":{"id"`)
	require.Contains(t, rec.Body.String(), "Vertex OK")
	require.NotNil(t, usage)
	require.Equal(t, 20, usage.PromptTokens)
	require.Equal(t, 5, usage.CompletionTokens)
}

func TestIRToggleDisabledFallsBackCleanly(t *testing.T) {
	setIRToggleForTest(t, false)
	c, _ := newIRTestGinContext(http.MethodPost, "/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAnthropic,
			ApiType:           constant.APITypeAnthropic,
			UpstreamModelName: "claude-sonnet-4-5",
		},
	}
	adaptor := &claude.Adaptor{}
	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatOpenAIResponses, &dto.OpenAIResponsesRequest{Model: "claude-sonnet-4-5"})
	require.False(t, handled)
	require.Nil(t, usage)
	require.Nil(t, apiErr)
}

func TestIRBedrockTargetResolutionAndPayloadPreparation(t *testing.T) {
	setIRToggleForTest(t, true)
	c, _ := newIRTestGinContext(http.MethodPost, "/v1/responses")
	c.Request.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAIResponses,
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAws,
			ApiType:           constant.APITypeAws,
			UpstreamModelName: "claude-sonnet-4-5-20250929",
		},
	}
	adaptor := &awsChannel.Adaptor{}
	spec, ok := resolveTargetIRSpec(c, info, adaptor, ir.FormatOpenAIResponses)
	require.True(t, ok)
	require.True(t, spec.IsBedrockClaude)
	require.Equal(t, ir.FormatAnthropic, spec.Format)
	require.Equal(t, ir.BedrockClaudeShim, spec.Shim)

	prepared, err := adaptor.PrepareRawClaudePayload(c, info, []byte(`{"model":"claude-sonnet-4-5","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(prepared, &m))
	require.Equal(t, "bedrock-2023-05-31", m["anthropic_version"])
	require.NotContains(t, m, "model")
	require.NotContains(t, m, "stream")
	require.Contains(t, string(prepared), "prompt-caching-2024-07-31")
}

func TestIRCrossProtocolGeminiAndClaudeToCodex(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	var gotPath string
	var gotAccountID string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAccountID = r.Header.Get("chatgpt-account-id")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "resp_codex_1",
			"object": "response",
			"status": "completed",
			"model": "gpt-6-astra",
			"output": [{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "Codex OK"}]}],
			"usage": {"input_tokens": 18, "output_tokens": 6, "total_tokens": 24}
		}`)
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1beta/models/gpt-6-astra:generateContent")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		RelayMode:       relayconstant.RelayModeGemini,
		RequestURLPath:  "/v1beta/models/gpt-6-astra:generateContent",
		IsStream:        false,
		OriginModelName: "gpt-6-astra",
		ClientModelName: "gpt-6-astra",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         99,
			ChannelType:       constant.ChannelTypeCodex,
			ApiType:           constant.APITypeCodex,
			ChannelBaseUrl:    server.URL,
			ApiKey:            `{"access_token":"tok_abc","account_id":"acct_123"}`,
			UpstreamModelName: "gpt-6-astra",
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"text":"Ping Codex"}]}],"generationConfig":{"maxOutputTokens":64,"temperature":0.5}}`, &req))

	adaptor := &codex.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatGoogleGenAI, &req)
	require.True(t, handled)
	require.Nil(t, apiErr)
	require.Equal(t, "/backend-api/codex/responses", gotPath)
	require.Equal(t, "acct_123", gotAccountID)
	require.Equal(t, false, gotBody["store"])
	require.Contains(t, gotBody, "instructions")
	require.NotContains(t, gotBody, "max_output_tokens")
	require.NotContains(t, gotBody, "temperature")
	require.Contains(t, rec.Body.String(), "Codex OK")
	require.NotNil(t, usage)
	require.Equal(t, 18, usage.PromptTokens)
	require.Equal(t, 6, usage.CompletionTokens)
}

func TestIRRoutingCoversOpenRouterAndOpenCodeGeminiResponses(t *testing.T) {
	setIRToggleForTest(t, true)
	c, _ := newIRTestGinContext(http.MethodPost, "/v1beta/models/gpt-6-astra:generateContent")

	orInfo := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		RelayMode:   relayconstant.RelayModeGemini,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenRouter,
			ApiType:           constant.APITypeOpenRouter,
			UpstreamModelName: "openai/gpt-5",
		},
	}
	spec, ok := resolveTargetIRSpec(c, orInfo, &openai.Adaptor{}, ir.FormatGoogleGenAI)
	require.True(t, ok)
	require.Equal(t, ir.FormatOpenAIChat, spec.Format)

	ocInfo := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatGemini,
		RelayMode:   relayconstant.RelayModeGemini,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenCode,
			ApiType:           constant.APITypeOpenCode,
			UpstreamModelName: "gpt-5.6-sol",
		},
	}
	ocSpec, ok := resolveTargetIRSpec(c, ocInfo, &opencode.Adaptor{}, ir.FormatGoogleGenAI)
	require.True(t, ok)
	require.Equal(t, ir.FormatOpenAIResponses, ocSpec.Format)
}
