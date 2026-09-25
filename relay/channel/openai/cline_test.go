package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/cline"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func clineTestInfo(stream bool) *relaycommon.RelayInfo {
	info := kiloTestInfo(stream)
	info.ChannelType = constant.ChannelTypeCline
	info.ChannelBaseUrl = constant.ChannelBaseURLs[constant.ChannelTypeCline]
	info.UpstreamModelName = "cline-free/a"
	return info
}

func TestClineRequestURL(t *testing.T) {
	info := clineTestInfo(false)
	a := &Adaptor{}
	a.Init(info)
	require.Equal(t, "Cline", a.GetChannelName())
	require.Empty(t, a.GetModelList())
	for _, base := range []string{"", "https://api.cline.bot/api", "https://api.cline.bot/api/", " https://api.cline.bot/api/v1/ "} {
		info.ChannelBaseUrl = base
		url, err := a.GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, "https://api.cline.bot/api/v1/chat/completions", url)
	}
	info.ChannelBaseUrl = "https://proxy.example/custom/v1/"
	url, err := a.GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://proxy.example/custom/v1/chat/completions", url)
	for _, mode := range []int{relayconstant.RelayModeResponses, relayconstant.RelayModeEmbeddings, relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeRealtime} {
		info.RelayMode = mode
		_, err := a.GetRequestURL(info)
		require.Error(t, err)
	}
}

func TestClineRequestParametersAndAuthentication(t *testing.T) {
	service.InitHttpClient()
	payload := `{"model":"openai/provider-model","messages":[{"role":"user","content":"test"}],"max_tokens":0,"temperature":0,"top_p":0,"seed":0,"stream":true,"stream_options":{"include_usage":true},"parallel_tool_calls":false,"reasoning":{"enabled":false},"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer saved-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, payload, string(body))
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer upstream.Close()
	info := clineTestInfo(true)
	info.ChannelBaseUrl = upstream.URL + "/api/v1/"
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(payload, &request))
	a := &Adaptor{}
	converted, err := a.ConvertOpenAIRequest(nil, info, &request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	raw, err := a.DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	raw.(*http.Response).Body.Close()
}

func TestClineResponses(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	completion := `{"id":"cline-test","model":"cline-free/a","choices":[{"index":0,"message":{"role":"assistant","content":"OK","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`
	streamBody := "data: " + `{"id":"cline-test","model":"cline-free/a","choices":[{"index":0,"delta":{"content":"OK","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":null}]}` + "\n\ndata: " + `{"id":"cline-test","model":"cline-free/a","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}` + "\n\ndata: [DONE]\n\n"
	for _, tc := range []struct {
		name, body string
		stream     bool
	}{
		{"wrapped", `{"success":true,"data":` + completion + `}`, false},
		{"plain", completion, false},
		{"stream", streamBody, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			usage, apiErr := (&Adaptor{}).DoResponse(c, resp, clineTestInfo(tc.stream))
			require.Nil(t, apiErr)
			require.Equal(t, 10, usage.(*dto.Usage).TotalTokens)
			require.Contains(t, w.Body.String(), "lookup")
			require.NotContains(t, w.Body.String(), `"success"`)
			if tc.stream {
				require.Contains(t, w.Body.String(), "[DONE]")
			} else {
				var response dto.OpenAITextResponse
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
				require.Len(t, response.Choices, 1)
			}
		})
	}
	for _, body := range []string{`{"success":false}`, `{"success":true}`, `{"success":true,"data":null}`, `{"error":{"message":"unavailable","code":503}}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		_, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, clineTestInfo(false))
		require.NotNil(t, apiErr, body)
	}
	// Errors after partial SSE output must not become a successful [DONE].
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := "data: " + `{"id":"x","choices":[{"index":0,"delta":{"content":"partial"}}]}` + "\n\ndata: " + `{"error":{"message":"disconnected","code":502}}` + "\n\n"
	_, _ = (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, clineTestInfo(true))
	require.NotContains(t, w.Body.String(), "[DONE]")
}

func TestClineClientHeadersOnWire(t *testing.T) {
	service.InitHttpClient()
	for _, tc := range []struct {
		name, passthrough, userAgentName, userAgent string
		test, runtime, override, stream             bool
	}{
		{name: "other client"},
		{name: "channel test", test: true},
		{name: "wildcard", passthrough: "*"},
		{name: "regex", passthrough: "re:.*"},
		{name: "runtime", passthrough: "*", runtime: true},
		{name: "explicit override", passthrough: "*", override: true},
		{name: "explicit UA", userAgentName: "User-Agent", userAgent: "OtherClient/1.0"},
		{name: "lowercase UA", userAgentName: "user-agent", userAgent: "OtherClient/1.0"},
		{name: "mixed case UA", userAgentName: "uSeR-aGeNt", userAgent: "OtherClient/1.0"},
		{name: "empty UA", userAgentName: "User-Agent"},
		{name: "client UA placeholder", userAgentName: "User-Agent", userAgent: "{client_header:User-Agent}"},
		{name: "runtime UA", runtime: true, userAgentName: "User-Agent", userAgent: "RuntimeClient/1.0"},
		{name: "channel test UA", test: true, userAgentName: "User-Agent", userAgent: "OtherClient/1.0"},
		{name: "stream UA", stream: true, userAgentName: "User-Agent", userAgent: "OtherClient/1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := make(chan http.Header, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				captured <- r.Header.Clone()
				_, _ = io.WriteString(w, `{}`)
			}))
			defer upstream.Close()
			info := clineTestInfo(tc.stream)
			info.ChannelBaseUrl = upstream.URL
			info.IsChannelTest = tc.test
			info.HeadersOverride = map[string]any{}
			if tc.passthrough != "" {
				info.HeadersOverride[tc.passthrough] = ""
			}
			if tc.override {
				info.HeadersOverride["X-CLIENT-VERSION"] = "custom-version"
				info.HeadersOverride["Authorization"] = "Bearer override-key"
			}
			if tc.userAgentName != "" {
				info.HeadersOverride[tc.userAgentName] = tc.userAgent
			}
			if tc.runtime {
				info.UseRuntimeHeadersOverride = true
				info.RuntimeHeadersOverride = info.HeadersOverride
				info.HeadersOverride = map[string]any{"User-Agent": "stale-agent"}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Request.Header.Set("Authorization", "Bearer downstream-key")
			c.Request.Header.Set("Cookie", "downstream-cookie")
			c.Request.Header.Set("User-Agent", "OtherClient/1.0")
			c.Request.Header.Set("X-CLIENT-TYPE", "other-client")
			c.Request.Header.Set("X-CLIENT-VERSION", "old-version")
			c.Request.Header.Set("X-CORE-VERSION", "old-core")
			if !tc.test {
				c.Request.Header.Set("X-Task-ID", "conversation-1")
			}
			a := &Adaptor{}
			a.Init(info)
			raw, err := a.DoRequest(c, info, strings.NewReader(`{}`))
			require.NoError(t, err)
			raw.(*http.Response).Body.Close()
			got := <-captured
			require.Equal(t, []string{cline.UserAgent}, got.Values("User-Agent"))
			for name, expected := range map[string]string{
				"HTTP-Referer": "https://cline.bot", "X-Title": "Cline",
				"User-Agent": "Cline/" + cline.ClientVersion, "X-IS-MULTIROOT": "false",
				"X-CLIENT-TYPE": "cline-cli", "X-PLATFORM": "cli",
				"X-PLATFORM-VERSION": cline.ClientVersion, "X-CORE-VERSION": cline.CoreVersion,
				"Content-Type": "application/json",
			} {
				require.Equal(t, expected, got.Get(name), name)
			}
			if tc.override {
				require.Equal(t, "custom-version", got.Get("X-CLIENT-VERSION"))
				require.Equal(t, "Bearer override-key", got.Get("Authorization"))
			} else {
				require.Equal(t, cline.ClientVersion, got.Get("X-CLIENT-VERSION"))
				require.Equal(t, "Bearer saved-key", got.Get("Authorization"))
			}
			require.Empty(t, got.Get("Cookie"))
			if tc.test {
				require.Regexp(t, `^\d{13}_[a-z0-9]{5}$`, got.Get("X-Task-ID"))
			} else {
				require.Equal(t, "conversation-1", got.Get("X-Task-ID"))
			}
			retryHeader := make(http.Header)
			require.NoError(t, a.SetupRequestHeader(c, &retryHeader, info))
			require.Equal(t, got.Get("X-Task-ID"), retryHeader.Get("X-Task-ID"))
		})
	}
	// Adding a filter to the shared adaptor must not alter other channels.
	headers := map[string]string{"user-agent": "another-client", "x-client-type": "other"}
	(&Adaptor{}).FilterHeaderPassthrough(headers, kiloTestInfo(false))
	require.Len(t, headers, 2)
}

func TestClineSDKTokenLimitConversion(t *testing.T) {
	for _, modelID := range []string{"openai/o3-mini", "openai/o1", "openai/o4-mini", "openai/gpt-5-chat"} {
		request := &dto.GeneralOpenAIRequest{Model: modelID, MaxTokens: common.GetPointer(uint(0))}
		_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, clineTestInfo(false), request)
		require.NoError(t, err)
		require.Nil(t, request.MaxTokens)
		require.NotNil(t, request.MaxCompletionTokens)
		require.Zero(t, *request.MaxCompletionTokens)
		require.Equal(t, modelID, request.Model)
		request.MaxTokens = common.GetPointer(uint(20))
		_, err = (&Adaptor{}).ConvertOpenAIRequest(nil, clineTestInfo(false), request)
		require.NoError(t, err)
		require.Zero(t, *request.MaxCompletionTokens, "explicit zero must win")
	}
	for _, modelID := range []string{"openai/gpt-4o", "cline-free/deepseek-v4.1-flash", "vendor/yolo1", "vendor/gpt-50"} {
		request := &dto.GeneralOpenAIRequest{Model: modelID, MaxTokens: common.GetPointer(uint(0))}
		_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, clineTestInfo(false), request)
		require.NoError(t, err)
		require.NotNil(t, request.MaxTokens)
		require.Nil(t, request.MaxCompletionTokens)
		require.Equal(t, modelID, request.Model)
	}
}
