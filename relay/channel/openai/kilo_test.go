package openai

import (
	"bytes"
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

func kiloTestInfo(stream bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeKilo,
			ChannelBaseUrl: constant.ChannelBaseURLs[constant.ChannelTypeKilo],
			ApiKey:         "saved-key", SupportStreamOptions: true, UpstreamModelName: "openrouter/free",
		},
		RelayMode: relayconstant.RelayModeChatCompletions, RequestURLPath: "/v1/chat/completions",
		RelayFormat: types.RelayFormatOpenAI, IsStream: stream, ShouldIncludeUsage: true, StartTime: time.Now(), DisablePing: true,
	}
}

func TestKiloRequestURLAndUnsupportedModes(t *testing.T) {
	info := kiloTestInfo(false)
	a := &Adaptor{}
	a.Init(info)
	require.Equal(t, "Kilo", a.GetChannelName())
	require.Empty(t, a.GetModelList())
	for _, base := range []string{info.ChannelBaseUrl, info.ChannelBaseUrl + "/", "https://proxy.example/gateway/"} {
		info.ChannelBaseUrl = base
		url, err := a.GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, strings.TrimRight(base, "/")+"/chat/completions", url)
	}
	for _, mode := range []int{relayconstant.RelayModeResponses, relayconstant.RelayModeEmbeddings, relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeRealtime} {
		info.RelayMode = mode
		_, err := a.GetRequestURL(info)
		require.Error(t, err)
	}
}

func TestKiloRequestPreservesParametersAndAuthentication(t *testing.T) {
	service.InitHttpClient()
	for _, anonymous := range []bool{true, false} {
		t.Run(map[bool]string{true: "anonymous", false: "key"}[anonymous], func(t *testing.T) {
			payload := `{"model":"openrouter/free","messages":[{"role":"system","content":"test"}],"max_tokens":0,"temperature":0,"top_p":0,"seed":0,"stream":true,"stream_options":{"include_usage":true},"parallel_tool_calls":false,"reasoning":{"enabled":false},"reasoning_effort":"none","tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto"}`
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(payload), &request))
			info := kiloTestInfo(true)
			info.ChannelOtherSettings.KiloAnonymousEnabled = anonymous
			if anonymous {
				info.HeadersOverride = map[string]any{"authorization": "Bearer override-key"}
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/gateway/chat/completions", r.URL.Path)
				if anonymous {
					require.Empty(t, r.Header.Get("Authorization"))
				} else {
					require.Equal(t, "Bearer saved-key", r.Header.Get("Authorization"))
				}
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.JSONEq(t, payload, string(body))
				_, _ = w.Write([]byte(`{"id":"ok"}`))
			}))
			defer upstream.Close()
			info.ChannelBaseUrl = upstream.URL + "/gateway/"
			converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, &request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("Authorization", "Bearer downstream-secret")
			raw, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))
			require.NoError(t, err)
			require.NoError(t, raw.(*http.Response).Body.Close())
		})
	}
}

func TestKiloResponsesAndUsage(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
			body := `{"id":"kilo-test","model":"openrouter/free","choices":[{"index":0,"message":{"role":"assistant","content":"OK","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`
			if stream {
				body = "data: " + `{"id":"kilo-test","model":"openrouter/free","choices":[{"index":0,"delta":{"content":"OK","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"kilo-test","model":"openrouter/free","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: " + `{"id":"kilo-test","model":"openrouter/free","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}` + "\n\ndata: [DONE]\n\n"
			}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, kiloTestInfo(stream))
			require.Nil(t, apiErr)
			require.Equal(t, 7, usage.(*dto.Usage).PromptTokens)
			require.Equal(t, 3, usage.(*dto.Usage).CompletionTokens)
			require.Contains(t, w.Body.String(), "lookup")
			if stream {
				require.Contains(t, w.Body.String(), "[DONE]")
				require.Contains(t, w.Body.String(), `"total_tokens":10`)
			}
		})
	}
}

func TestKiloErrorsWithoutType(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	_, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"unavailable","code":503}}`))}, kiloTestInfo(false))
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	w := httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := "data: " + `{"id":"kilo-test","choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\ndata: " + `{"error":{"message":"disconnected","code":502},"choices":[{"index":0,"delta":{},"finish_reason":"error"}]}` + "\n\n"
	_, _ = (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, kiloTestInfo(true))
	require.Contains(t, w.Body.String(), "disconnected")
	require.NotContains(t, w.Body.String(), "[DONE]")
}
