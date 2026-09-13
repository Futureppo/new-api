package mistral

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testInfo(mode int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: mode, RelayFormat: types.RelayFormatOpenAI, StartTime: time.Now(), DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMistral, ChannelBaseUrl: "https://api.mistral.ai", UpstreamModelName: "ministral-3b-latest"},
	}
}

func testContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	return c, w
}

func decodeMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, common.Unmarshal(data, &result))
	return result
}

func TestMistralRequestPreservesParametersAndZero(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"mistral-small-latest","messages":[{"role":"developer","content":"system"},{"role":"user","content":""}],"stream":false,"stream_options":{"include_usage":true},"max_tokens":99,"max_completion_tokens":0,"temperature":0,"top_p":0.5,"seed":0,"n":1,"stop":["END"],"frequency_penalty":0,"presence_penalty":0,"parallel_tool_calls":false,"response_format":{"type":"json_schema","json_schema":{"name":"test","schema":{"type":"object"},"strict":false}},"prediction":{"type":"content","content":"old"},"metadata":{"label":"test"}}`, &request))
	converted, err := requestOpenAI2Mistral(&request)
	require.NoError(t, err)
	body := decodeMap(t, converted)
	for _, key := range []string{"max_tokens", "temperature", "random_seed", "frequency_penalty", "presence_penalty"} {
		require.Equal(t, float64(0), body[key], key)
	}
	require.Equal(t, false, body["parallel_tool_calls"])
	require.Equal(t, false, body["stream"])
	require.Equal(t, []any{"END"}, body["stop"])
	require.Equal(t, float64(1), body["n"])
	require.Contains(t, body, "prediction")
	require.Contains(t, body, "metadata")
	require.Contains(t, body, "response_format")
	for _, key := range []string{"seed", "stream_options", "max_completion_tokens"} {
		require.NotContains(t, body, key)
	}
	require.Equal(t, "system", converted.Messages[0].Role)
	require.Equal(t, "", converted.Messages[1].Content)
	require.Equal(t, "developer", request.Messages[0].Role)
	minimal, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Model: "m", Messages: []dto.Message{{Role: "user", Content: "hi"}}})
	require.NoError(t, err)
	for _, key := range []string{"max_tokens", "temperature", "random_seed", "parallel_tool_calls"} {
		require.NotContains(t, decodeMap(t, minimal), key)
	}
	for _, seed := range []float64{-1, 0.5, 1e30} {
		_, err := requestOpenAI2Mistral(&dto.GeneralOpenAIRequest{Seed: &seed})
		require.Error(t, err)
	}
}

func TestMistralHistoryAndMultimodal(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"low"}},{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]},{"role":"assistant","content":null,"tool_calls":[{"id":"call_long_1","type":"function","function":{"name":"weather","arguments":"{\"city\":\"Paris\"}"}},{"id":"abcdef123","type":"function","function":{"name":"weather","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_long_1","name":"weather","content":"sunny"},{"role":"tool","tool_call_id":"abcdef123","content":"rain"}]}`, &request))
	original := decodeMap(t, &request)
	converted, err := requestOpenAI2Mistral(&request)
	require.NoError(t, err)
	require.Equal(t, original, decodeMap(t, &request), "client history was mutated")
	require.Equal(t, request.Messages[0].Content, converted.Messages[0].Content)
	require.Nil(t, converted.Messages[1].Content)
	calls := converted.Messages[1].ParseToolCalls()
	require.Len(t, calls, 2)
	require.Regexp(t, mistralToolCallIdRegexp, calls[0].ID)
	require.Equal(t, calls[0].ID, converted.Messages[2].ToolCallId)
	require.Equal(t, "abcdef123", calls[1].ID)
	require.Equal(t, "weather", *converted.Messages[2].Name)
	// A valid ID can collide with the first hash candidate; it must stay reserved.
	request.Messages[3].ToolCallId = calls[0].ID
	converted, err = requestOpenAI2Mistral(&request)
	require.NoError(t, err)
	require.NotEqual(t, calls[0].ID, converted.Messages[1].ParseToolCalls()[0].ID)
}

func TestMistralURLsAndUnsupportedEndpoints(t *testing.T) {
	a := &Adaptor{}
	for _, base := range []string{"https://api.mistral.ai", "https://api.mistral.ai/", "https://api.mistral.ai/v1", "https://api.mistral.ai/v1/"} {
		for _, tc := range []struct {
			mode int
			path string
		}{{relayconstant.RelayModeChatCompletions, "/v1/chat/completions"}, {relayconstant.RelayModeEmbeddings, "/v1/embeddings"}, {relayconstant.RelayModeAudioTranscription, "/v1/audio/transcriptions"}} {
			info := testInfo(tc.mode)
			info.ChannelBaseUrl = base
			target, err := a.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, "https://api.mistral.ai"+tc.path, target)
		}
	}
	for _, mode := range []int{relayconstant.RelayModeResponses, relayconstant.RelayModeCompletions, relayconstant.RelayModeAudioSpeech, relayconstant.RelayModeAudioTranslation} {
		_, err := a.GetRequestURL(testInfo(mode))
		require.Error(t, err)
	}
	_, err := a.ConvertClaudeRequest(nil, nil, nil)
	require.Error(t, err)
	_, err = a.ConvertRerankRequest(nil, 0, dto.RerankRequest{})
	require.Error(t, err)
}

func TestMistralErrorEnvelopePreservesStatusAndDetails(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		status             int
		body, expectedType string
	}{
		{429, `{"object":"error","message":"Rate limit exceeded","type":"rate_limited","code":"1300","param":null}`, "rate_limited"},
		{403, `{"object":"error","message":"tier restricted","type":"tier_not_allowed","code":"1910"}`, "tier_not_allowed"},
		{422, `{"detail":[{"loc":["body","messages",0],"msg":"bad role","input":"private text"}]}`, "invalid_request_error"},
		{422, `{"object":"error","type":"invalid_request_error","message":{"detail":[{"loc":["timestamp_granularities"],"msg":"at most one item","input":"private text"}]} }`, "invalid_request_error"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		info := testInfo(relayconstant.RelayModeChatCompletions)
		info.ChannelBaseUrl = server.URL
		c, _ := testContext()
		result, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader("{}"))
		require.NoError(t, err)
		resp := result.(*http.Response)
		require.Equal(t, tc.status, resp.StatusCode)
		require.Equal(t, "30", resp.Header.Get("Retry-After"))
		apiErr := service.RelayErrorHandler(c.Request.Context(), resp, false)
		require.Equal(t, tc.expectedType, apiErr.ToOpenAIError().Type)
		require.NotContains(t, apiErr.Error(), "private text")
		if tc.status == 429 {
			require.Equal(t, "1300", apiErr.ToOpenAIError().Code)
		}
		server.Close()
	}
}
