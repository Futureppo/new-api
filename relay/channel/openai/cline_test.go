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
