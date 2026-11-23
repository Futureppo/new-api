package relay

import (
	"encoding/json"
	"errors"
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
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/ir"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// failingAfterFirstWriteRecorder simulates a client that reads the first SSE chunk
// and then disconnects (subsequent Writes fail with broken pipe).
type failingAfterFirstWriteRecorder struct {
	*httptest.ResponseRecorder
	writes int
}

func (f *failingAfterFirstWriteRecorder) Write(p []byte) (int, error) {
	f.writes++
	if f.writes >= 2 {
		return 0, errors.New("broken pipe: client disconnected")
	}
	return f.ResponseRecorder.Write(p)
}

func TestIRRetryStateResetPreventsCrossChannelPollution(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	// Upstream returns HTTP 503 so attempt 1 fails pre-commit.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"type":"overloaded_error","message":"overloaded"}}`)
	}))
	defer server.Close()

	c, _ := newIRTestGinContext(http.MethodPost, "/v1/chat/completions")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAI,
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RequestURLPath:  "/v1/chat/completions",
		IsStream:        false,
		DisablePing:     false,
		OriginModelName: "claude-sonnet-4-5",
		ClientModelName: "claude-sonnet-4-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         11,
			ChannelType:       constant.ChannelTypeAnthropic,
			ApiType:           constant.APITypeAnthropic,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "sk-ant-fail",
			UpstreamModelName: "claude-sonnet-4-5",
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}]}`, &req))

	adaptor := &claude.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatOpenAIChat, &req)
	require.True(t, handled)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)

	// Pre-commit failure MUST restore RelayInfo so retry to Channel 2 is completely unpolluted.
	require.Equal(t, types.RelayFormatOpenAI, info.GetFinalRequestRelayFormat())
	require.Equal(t, []types.RelayFormat{types.RelayFormatOpenAI}, info.RequestConversionChain)
	require.False(t, info.IsStream)
	require.False(t, info.DisablePing)
	require.False(t, c.GetBool("ir_response_committed"))
}

func TestIRClientDisconnectBillsConsumedTokensInsteadOfRefunding(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_disc","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[],"usage":{"input_tokens":200,"output_tokens":0}}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Chunk one "}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Chunk two triggers disconnect"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	baseRec := httptest.NewRecorder()
	failRec := &failingAfterFirstWriteRecorder{ResponseRecorder: baseRec}
	c, _ := gin.CreateTestContext(failRec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		IsStream:        true,
		DisablePing:     true,
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

	var req dto.OpenAIResponsesRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-sonnet-4-5","stream":true,"input":"Tell a story"}`, &req))

	adaptor := &claude.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatOpenAIResponses, &req)
	require.True(t, handled)
	// Post-commit client disconnect MUST return apiErr == nil and settle consumed tokens (preventing 100% free-riding refund).
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.GreaterOrEqual(t, usage.PromptTokens, 200)
	require.True(t, c.GetBool("ir_response_committed"))
}

func TestIRImmediateStreamEventErrorReturnsRetryableErrorZeroBilling(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	// Upstream returns HTTP 200 text/event-stream with an immediate error event and no content.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1/responses")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		IsStream:        true,
		DisablePing:     true,
		OriginModelName: "claude-sonnet-4-5",
		ClientModelName: "claude-sonnet-4-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         43,
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
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-sonnet-4-5","stream":true,"input":"Hello"}`, &req))

	adaptor := &claude.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatOpenAIResponses, &req)
	require.True(t, handled)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "Overloaded")
	// SSE stream was NEVER committed, so controller can cleanly retry another channel!
	require.False(t, c.GetBool("ir_response_committed"))
	require.Empty(t, rec.Body.String())
}

func TestIRCodexNonStreamForcesUpstreamSSEAndAggregatesJSON(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	var gotAccept string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		raw, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(raw, &gotBody)

		// Real ChatGPT Codex upstream requires stream:true and always responds with SSE.
		if streamVal, ok := gotBody["stream"].(bool); !ok || !streamVal {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"detail":"Stream must be set to true"}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, strings.Join([]string{
			`event: response.created`,
			`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_cdx_1","model":"gpt-5.3-codex","status":"in_progress"}}`,
			``,
			`event: response.output_item.added`,
			`data: {"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"in_progress","content":[]}}`,
			``,
			`event: response.output_text.delta`,
			`data: {"type":"response.output_text.delta","sequence_number":2,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Codex aggregated "}`,
			``,
			`event: response.output_text.delta`,
			`data: {"type":"response.output_text.delta","sequence_number":3,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"answer!"}`,
			``,
			`event: response.completed`,
			`data: {"type":"response.completed","sequence_number":4,"response":{"id":"resp_cdx_1","model":"gpt-5.3-codex","status":"completed","usage":{"input_tokens":33,"output_tokens":9,"total_tokens":42}}}`,
			``,
		}, "\n"))
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1/messages")
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatClaude,
		RelayMode:       relayconstant.RelayModeUnknown,
		RequestURLPath:  "/v1/messages",
		IsStream:        false, // Client requested NON-STREAMING!
		OriginModelName: "gpt-5.3-codex",
		ClientModelName: "gpt-5.3-codex",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         101,
			ChannelType:       constant.ChannelTypeCodex,
			ApiType:           constant.APITypeCodex,
			ChannelBaseUrl:    server.URL,
			ApiKey:            `{"access_token":"tok_abc","account_id":"acct_123"}`,
			UpstreamModelName: "gpt-5.3-codex",
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gpt-5.3-codex","max_tokens":128,"messages":[{"role":"user","content":"Hi Codex"}]}`, &req))

	adaptor := &codex.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatAnthropic, &req)
	require.True(t, handled)
	require.Nil(t, apiErr)

	// Upstream received forced stream:true and Accept:text/event-stream.
	require.Equal(t, true, gotBody["stream"])
	require.Equal(t, "text/event-stream", gotAccept)

	// Client received clean non-streaming Anthropic JSON!
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var claudeResp map[string]any
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &claudeResp))
	require.Equal(t, "message", claudeResp["type"])
	require.Contains(t, rec.Body.String(), "Codex aggregated answer!")
	require.NotNil(t, usage)
	require.Equal(t, 33, usage.PromptTokens)
	require.Equal(t, 9, usage.CompletionTokens)
	// Client info.IsStream must remain false.
	require.False(t, info.IsStream)
}

func TestAzureClaudeURLDoesNotDuplicateChatCompletions(t *testing.T) {
	adaptor := &openai.Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatClaude,
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAzure,
			ChannelBaseUrl:    "https://demo.openai.azure.com",
			ApiVersion:        "2024-10-21",
			UpstreamModelName: "gpt-4o",
			ChannelCreateTime: constant.AzureNoRemoveDotTime + 100,
		},
	}
	gotURL, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://demo.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=2024-10-21", gotURL)
	require.NotContains(t, gotURL, "chat/completionschat/completions")
}

func TestIRModelSuffixAdaptationAndResponsesSystemPrompt(t *testing.T) {
	// 1. Claude -thinking suffix adaptation strips suffix and injects enabled thinking.
	infoClaude := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet-20250219-thinking",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-3-7-sonnet-20250219-thinking",
		},
	}
	outClaude, err := applyIRModelSuffixAdaptation([]byte(`{"model":"claude-3-7-sonnet-20250219-thinking","max_tokens":4096,"messages":[{"role":"user","content":"hi"}]}`), infoClaude, irTargetSpec{Format: ir.FormatAnthropic})
	require.NoError(t, err)
	require.Equal(t, "claude-3-7-sonnet-20250219", infoClaude.UpstreamModelName)
	var mClaude map[string]any
	require.NoError(t, common.Unmarshal(outClaude, &mClaude))
	require.Equal(t, "claude-3-7-sonnet-20250219", mClaude["model"])
	require.NotNil(t, mClaude["thinking"])

	// 2. Gemini -thinking-2048 suffix injects thinkingConfig.thinkingBudget=2048.
	prevGemThinking := model_setting.GetGeminiSettings().ThinkingAdapterEnabled
	model_setting.GetGeminiSettings().ThinkingAdapterEnabled = true
	t.Cleanup(func() { model_setting.GetGeminiSettings().ThinkingAdapterEnabled = prevGemThinking })
	infoGem := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-pro-thinking-2048",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-pro-thinking-2048",
		},
	}
	outGem, err := applyIRModelSuffixAdaptation([]byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`), infoGem, irTargetSpec{Format: ir.FormatGoogleGenAI})
	require.NoError(t, err)
	require.Contains(t, string(outGem), `"thinkingBudget":2048`)
	require.Contains(t, string(outGem), `"includeThoughts":true`)

	// 3. ResponsesHelper channel SystemPrompt injection.
	respReq := &dto.OpenAIResponsesRequest{
		Model:        "claude-sonnet-4-5",
		Instructions: json.RawMessage(`"User rules"`),
	}
	infoSys := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting: dto.ChannelSettings{
				SystemPrompt:         "Channel policy",
				SystemPromptOverride: true,
			},
		},
	}
	mergeChannelSystemPromptIntoResponses(respReq, infoSys)
	var mergedInstr string
	require.NoError(t, common.Unmarshal(respReq.Instructions, &mergedInstr))
	require.Equal(t, "Channel policy\nUser rules", mergedInstr)
}

func TestOpenCodeClaudeToGeminiAppendsRequestConversionAndBedrockVertexFixes(t *testing.T) {
	setIRToggleForTest(t, true)

	// 1. gemini.Adaptor.ConvertClaudeRequest MUST record RelayFormatGemini in RequestConversionChain (fixes OpenCode double billing).
	infoGem := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		UserId:      7,
		TokenId:     8,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         55,
			UpstreamModelName: "gemini-2.5-pro",
		},
	}
	infoGem.InitRequestConversionChain()
	c, _ := newIRTestGinContext(http.MethodPost, "/v1/messages")
	var claudeReq dto.ClaudeRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"gemini-2.5-pro","max_tokens":128,"messages":[{"role":"user","content":"hi"}]}`, &claudeReq))
	gemAdaptor := &gemini.Adaptor{}
	converted, err := gemAdaptor.ConvertClaudeRequest(c, infoGem, &claudeReq)
	require.NoError(t, err)
	require.NotNil(t, converted)
	require.Equal(t, types.RelayFormat(types.RelayFormatGemini), infoGem.GetFinalRequestRelayFormat())

	// 2. Bedrock PrepareRawClaudePayload must NOT clobber ParamOverride's anthropic_version.
	awsAdaptor := &awsChannel.Adaptor{}
	infoAWS := &relaycommon.RelayInfo{
		OriginModelName: "claude-sonnet-4-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "claude-sonnet-4-5",
		},
	}
	prepared, err := awsAdaptor.PrepareRawClaudePayload(c, infoAWS, []byte(`{"anthropic_version":"bedrock-custom-override","max_tokens":128,"messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	require.Contains(t, string(prepared), `"anthropic_version":"bedrock-custom-override"`)

	// 3. Vertex ResolveIRTarget must reject Speech/Transcribe models (gemini-2.5-flash-tts).
	vInfoTTS := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-flash-tts",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash-tts",
		},
	}
	vAdaptor := &vertex.Adaptor{}
	vAdaptor.Init(vInfoTTS)
	_, ok := vAdaptor.ResolveIRTarget(vInfoTTS)
	require.False(t, ok, "Vertex Speech model must not route through text IR")
}

func TestIRStreamUpstream400ClearsPreSetSSEHeadersAndSupportsRetry(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, true)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"invalid_request_error","message":"secret upstream detail","type":"invalid_request_error"}}`)
	}))
	defer server.Close()

	c, rec := newIRTestGinContext(http.MethodPost, "/v1beta/models/gpt-test:streamGenerateContent?alt=sse")
	common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{ShowErrorDetails: false})
	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		RelayMode:       relayconstant.RelayModeGemini,
		RequestURLPath:  "/v1beta/models/gpt-test:streamGenerateContent?alt=sse",
		IsStream:        true,
		OriginModelName: "gpt-test",
		ClientModelName: "gpt-test",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         77,
			ChannelType:       constant.ChannelTypeOpenAI,
			ApiType:           constant.APITypeOpenAI,
			ChannelBaseUrl:    server.URL,
			ApiKey:            "test-key",
			UpstreamModelName: "gpt-test",
		},
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	var req dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"text":"Reply OK"}]}]}`, &req))

	adaptor := &openai.Adaptor{}
	adaptor.Init(info)

	handled, usage, apiErr := tryIRCrossProtocolRelay(c, info, adaptor, ir.FormatGoogleGenAI, &req)
	require.True(t, handled)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)

	// Pre-set SSE headers from DoApiRequest must have been cleared cleanly.
	require.Empty(t, c.Writer.Header().Get("Content-Type"))
	require.False(t, c.GetBool("event_stream_headers_set"))

	// Writing JSON error now produces non-empty application/json (NOT 0-byte text/event-stream).
	c.JSON(apiErr.StatusCode, gin.H{"error": apiErr.ToOpenAIError()})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.NotEmpty(t, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "secret upstream detail")

	// Also verify SetEventStreamHeaders can be applied again on a fresh retry context after ClearEventStreamHeaders.
	c2, _ := newIRTestGinContext(http.MethodPost, "/v1/messages")
	helper.SetEventStreamHeaders(c2)
	require.Equal(t, "text/event-stream", c2.Writer.Header().Get("Content-Type"))
	helper.ClearEventStreamHeaders(c2)
	require.Empty(t, c2.Writer.Header().Get("Content-Type"))
	helper.SetEventStreamHeaders(c2)
	require.Equal(t, "text/event-stream", c2.Writer.Header().Get("Content-Type"))
}

func TestIRResponsesPolicyRoutingAndSuffixAdaptation(t *testing.T) {
	// Enable ChatCompletionsToResponsesPolicy for all channels matching gpt-6*.
	gs := model_setting.GetGlobalSettings()
	prevPolicy := gs.ChatCompletionsToResponsesPolicy
	gs.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-6.*"},
	}
	t.Cleanup(func() { gs.ChatCompletionsToResponsesPolicy = prevPolicy })

	// 1. vLLM / LiteLLM / DeepSeek / Ollama channels must route Gemini -> FormatOpenAIResponses when policy matches.
	for _, tc := range []struct {
		name        string
		channelType int
		apiType     int
	}{
		{"vllm", constant.ChannelTypeVLLM, constant.APITypeVLLM},
		{"litellm", constant.ChannelTypeLiteLLM, constant.APITypeLiteLLM},
		{"deepseek", constant.ChannelTypeDeepSeek, constant.APITypeDeepSeek},
		{"moonshot", constant.ChannelTypeMoonshot, constant.APITypeMoonshot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newIRTestGinContext(http.MethodPost, "/v1beta/models/gpt-6-astra:generateContent")
			info := &relaycommon.RelayInfo{
				RelayFormat:     types.RelayFormatGemini,
				RelayMode:       relayconstant.RelayModeGemini,
				OriginModelName: "gpt-6-astra",
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:         88,
					ChannelType:       tc.channelType,
					ApiType:           tc.apiType,
					UpstreamModelName: "gpt-6-astra",
				},
			}
			spec, ok := resolveTargetIRSpec(c, info, &openai.Adaptor{}, ir.FormatGoogleGenAI)
			require.True(t, ok)
			require.Equal(t, ir.FormatOpenAIResponses, spec.Format)
			require.Equal(t, relayconstant.RelayModeResponses, spec.RelayMode)
			require.Equal(t, "/v1/responses", spec.URLPath)
		})
	}

	// 2. Non-ChannelTypeOpenAI Responses target strips OpenAI reasoning effort suffix (e.g. gpt-5-high -> gpt-5 + effort=high).
	infoResp := &relaycommon.RelayInfo{
		OriginModelName: "gpt-5-high",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeAzure,
			UpstreamModelName: "gpt-5-high",
		},
	}
	outResp, err := applyIRModelSuffixAdaptation(
		[]byte(`{"model":"gpt-5-high","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`),
		infoResp,
		irTargetSpec{Format: ir.FormatOpenAIResponses},
	)
	require.NoError(t, err)
	require.Equal(t, "gpt-5", infoResp.UpstreamModelName)
	require.Equal(t, "high", infoResp.ReasoningEffort)
	var mResp map[string]any
	require.NoError(t, common.Unmarshal(outResp, &mResp))
	require.Equal(t, "gpt-5", mResp["model"])
	require.Equal(t, "high", mResp["reasoning"].(map[string]any)["effort"])
}

func TestGeminiHelperNonOpenAIChannelFallbackUsesResponsesPolicy(t *testing.T) {
	service.InitHttpClient()
	setIRToggleForTest(t, false) // Exercise GeminiHelper's non-IR fallback directly for non-OpenAI Responses channels.

	gs := model_setting.GetGlobalSettings()
	prevPolicy := gs.ChatCompletionsToResponsesPolicy
	gs.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled:       true,
		AllChannels:   true,
		ModelPatterns: []string{"^gpt-6-astra$"},
	}
	t.Cleanup(func() { gs.ChatCompletionsToResponsesPolicy = prevPolicy })

	// 1. Verify GeminiHelper routes non-OpenAI channel matching policy to /v1/responses (returning 400 avoids DB quota commit).
	var gotPath string
	var gotBody map[string]any
	probeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"probe stop"}}`)
	}))
	defer probeServer.Close()

	c, _ := newIRTestGinContext(http.MethodPost, "/v1beta/models/gpt-6-astra:generateContent")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenRouter)
	common.SetContextKey(c, constant.ContextKeyChannelId, 99)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, probeServer.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sk-test")
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-6-astra")

	var req dto.GeminiChatRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"contents":[{"role":"user","parts":[{"text":"Reply OK"}]}]}`, &req))

	info := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		RelayMode:       relayconstant.RelayModeGemini,
		RequestURLPath:  "/v1beta/models/gpt-6-astra:generateContent",
		OriginModelName: "gpt-6-astra",
		ClientModelName: "gpt-6-astra",
		Request:         &req,
	}
	info.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c, info)
	relaycommon.InstallRelayResponseWriter(c)

	apiErr := GeminiHelper(c, info)
	require.NotNil(t, apiErr)
	require.Equal(t, "/v1/responses", gotPath)
	require.Equal(t, "gpt-6-astra", gotBody["model"])
	require.NotNil(t, gotBody["input"])

	// 2. Verify full 200 Responses -> Gemini translation through chatCompletionsViaResponses.
	okServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_gem_1","object":"response","status":"completed","model":"gpt-6-astra","output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Gemini via Responses OK"}]}],"usage":{"input_tokens":5,"output_tokens":4,"total_tokens":9}}`)
	}))
	defer okServer.Close()

	c2, rec2 := newIRTestGinContext(http.MethodPost, "/v1beta/models/gpt-6-astra:generateContent")
	info2 := &relaycommon.RelayInfo{
		RelayFormat:     types.RelayFormatGemini,
		RelayMode:       relayconstant.RelayModeGemini,
		RequestURLPath:  "/v1beta/models/gpt-6-astra:generateContent",
		OriginModelName: "gpt-6-astra",
		ClientModelName: "gpt-6-astra",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         99,
			ChannelType:       constant.ChannelTypeOpenRouter,
			ApiType:           constant.APITypeOpenRouter,
			ChannelBaseUrl:    okServer.URL,
			ApiKey:            "sk-test",
			UpstreamModelName: "gpt-6-astra",
		},
	}
	info2.InitRequestConversionChain()
	relaycommon.SetRelayInfo(c2, info2)
	relaycommon.InstallRelayResponseWriter(c2)

	oaiReq, convErr := service.GeminiToOpenAIRequest(&req, info2)
	require.NoError(t, convErr)
	adaptor := &openai.Adaptor{}
	adaptor.Init(info2)
	usage, respErr := chatCompletionsViaResponses(c2, info2, adaptor, oaiReq)
	require.Nil(t, respErr)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.Contains(t, rec2.Body.String(), "Gemini via Responses OK")
}
