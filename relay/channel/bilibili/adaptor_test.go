package bilibili

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
	"github.com/tidwall/gjson"
)

func testInfo(responses, stream bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeBilibili, UpstreamModelName: DefaultModel},
		OriginModelName: DefaultModel, RelayMode: relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI, IsStream: stream, ShouldIncludeUsage: true,
		DisablePing: true, StartTime: time.Now(),
	}
	if responses {
		info.RelayMode = relayconstant.RelayModeResponses
		info.RelayFormat = types.RelayFormatOpenAIResponses
	}
	return info
}

func TestURLsAndUnsupportedModes(t *testing.T) {
	a := &Adaptor{}
	for _, responses := range []bool{false, true} {
		info := testInfo(responses, false)
		path := "/v1/chat/completions"
		if responses {
			path = "/v1/responses"
		}
		for _, suffix := range []string{"", "/", "/v1", "/v1/", "/v1/chat/completions", "/v1/responses/", "/v1/models"} {
			info.ChannelBaseUrl = "https://proxy.example/prefix" + suffix
			got, err := a.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, "https://proxy.example/prefix"+path, got)
		}
		info.ChannelBaseUrl = ""
		got, err := a.GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, constant.ChannelBaseURLs[constant.ChannelTypeBilibili]+path, got)
	}
	for _, base := range []string{"/v1", "ftp://host", "https://user:pass@host", "https://host?x=1", "https://host/#fragment"} {
		require.Error(t, ValidateBaseURL(base), base)
	}
	info := testInfo(false, false)
	for _, mode := range []int{relayconstant.RelayModeCompletions, relayconstant.RelayModeEmbeddings, relayconstant.RelayModeResponsesCompact, relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeAudioSpeech} {
		info.RelayMode = mode
		_, err := a.GetRequestURL(info)
		require.Error(t, err)
		apiErr, ok := err.(*types.NewAPIError)
		require.True(t, ok)
		require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	}
}

func TestTranslationRequestDefaultsAndExplicitValues(t *testing.T) {
	a := &Adaptor{}
	for _, responses := range []bool{false, true} {
		for _, params := range []string{
			``,
			`,"temperature":0,"top_p":0,"stream":false,"stream_options":{"include_usage":false},"chat_template_kwargs":{"enable_thinking":false,"custom":0}`,
			`,"temperature":0.8,"chat_template_kwargs":{"enable_thinking":true}`,
			`,"chat_template_kwargs":{"custom":false}`,
		} {
			raw := `{"model":"translation-alias","messages":[{"role":"user","content":[{"type":"text","text":"Translate this image"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]` + params + `}`
			if responses {
				raw = strings.Replace(raw, `"messages":`, `"input":`, 1)
			}
			var converted any
			var err error
			if responses {
				var request dto.OpenAIResponsesRequest
				require.NoError(t, common.Unmarshal([]byte(raw), &request))
				converted, err = a.ConvertOpenAIResponsesRequest(nil, testInfo(true, false), request)
			} else {
				var request dto.GeneralOpenAIRequest
				require.NoError(t, common.Unmarshal([]byte(raw), &request))
				converted, err = a.ConvertOpenAIRequest(nil, testInfo(false, false), &request)
			}
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			require.Equal(t, "translation-alias", gjson.GetBytes(body, "model").String())
			require.Contains(t, string(body), "Translate this image")
			require.Contains(t, string(body), "data:image/png;base64,aGVsbG8=")
			for _, field := range []string{"temperature", "top_p", "stream", "stream_options.include_usage", "chat_template_kwargs.enable_thinking", "chat_template_kwargs.custom"} {
				if original := gjson.Get(raw, field); original.Exists() {
					require.Equal(t, original.Raw, gjson.GetBytes(body, field).Raw, field)
				}
			}
			if !gjson.Get(raw, "temperature").Exists() {
				require.Equal(t, "0", gjson.GetBytes(body, "temperature").Raw)
			}
			if !gjson.Get(raw, "chat_template_kwargs.enable_thinking").Exists() {
				require.Equal(t, "false", gjson.GetBytes(body, "chat_template_kwargs.enable_thinking").Raw)
			}
		}
	}
	_, err := a.ConvertOpenAIRequest(nil, nil, nil)
	require.Error(t, err)
	for _, raw := range []string{`[]`, `"bad"`, `false`} {
		_, err = a.ConvertOpenAIRequest(nil, nil, &dto.GeneralOpenAIRequest{ChatTemplateKwargs: []byte(raw)})
		require.Error(t, err)
	}
}

func TestRequestAuthenticationAndOverrides(t *testing.T) {
	service.InitHttpClient()
	for _, responses := range []bool{false, true} {
		for _, key := range []string{"", "  ", "test-key"} {
			for _, override := range []bool{false, true} {
				info := testInfo(responses, false)
				info.ApiKey = key
				wantAuth, wantAgent := "", UserAgent
				if strings.TrimSpace(key) != "" {
					wantAuth = "Bearer test-key"
				}
				if override {
					info.HeadersOverride = map[string]any{"Authorization": "Bearer override-key", "User-Agent": "Custom-Client"}
					wantAuth, wantAgent = "Bearer override-key", "Custom-Client"
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					require.Equal(t, wantAuth, r.Header.Get("Authorization"))
					if wantAuth == "" {
						require.NotContains(t, r.Header, "Authorization")
					}
					require.Equal(t, wantAgent, r.Header.Get("User-Agent"))
					path := "/v1/chat/completions"
					if responses {
						path = "/v1/responses"
					}
					require.Equal(t, "/prefix"+path, r.URL.Path)
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"model":"mapped-model","temperature":0.7,"chat_template_kwargs":{"enable_thinking":true}}`, string(body))
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit_error"}}`)
				}))
				info.ChannelBaseUrl = upstream.URL + "/prefix/v1"
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				c.Request.Header.Set("Content-Type", "application/json")
				c.Request.Header.Set("Authorization", "Bearer downstream-secret")
				a := &Adaptor{}
				a.Init(info)
				// The sender receives the final body after parameter overrides.
				response, err := a.DoRequest(c, info, bytes.NewBufferString(`{"model":"mapped-model","temperature":0.7,"chat_template_kwargs":{"enable_thinking":true}}`))
				require.NoError(t, err)
				require.Equal(t, http.StatusTooManyRequests, response.(*http.Response).StatusCode)
				response.(*http.Response).Body.Close()
				upstream.Close()
			}
		}
	}
}

func TestResponsesAndUsage(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	for _, responses := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			info := testInfo(responses, stream)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			body := `{"id":"chat1","model":"Index-Translate-35B-A3B","choices":[{"index":0,"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":23,"completion_tokens":2,"total_tokens":25}}`
			if responses {
				body = `{"id":"resp1","object":"response","status":"completed","model":"Index-Translate-35B-A3B","output":[{"id":"msg1","type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],"usage":{"input_tokens":23,"output_tokens":2,"total_tokens":25}}`
			}
			contentType := "application/json"
			if stream {
				contentType = "text/event-stream"
				if responses {
					body = "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + body + "}\n\n"
				} else {
					body = "data: {\"id\":\"chat1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"id\":\"chat1\",\"choices\":[],\"usage\":{\"prompt_tokens\":23,\"completion_tokens\":2,\"total_tokens\":25}}\n\ndata: [DONE]\n\n"
				}
			}
			a := &Adaptor{}
			a.Init(info)
			usage, apiErr := a.DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}, info)
			require.Nil(t, apiErr)
			require.Equal(t, 25, usage.(*dto.Usage).TotalTokens)
			require.Equal(t, 23, usage.(*dto.Usage).PromptTokens)
			require.Equal(t, 2, usage.(*dto.Usage).CompletionTokens)
			require.Contains(t, w.Body.String(), "Hello")
			if stream {
				if responses {
					require.Contains(t, w.Body.String(), "response.completed")
				} else {
					require.Contains(t, w.Body.String(), "[DONE]")
				}
			}
		}
	}
}

func TestStreamErrorsDoNotBecomeSuccessfulTranslations(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	for _, responses := range []bool{false, true} {
		info := testInfo(responses, true)
		info.ChannelSetting.ForceFormat = true
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		body := "data: {\"error\":{\"message\":\"translation unavailable\",\"type\":\"server_error\"}}\n\n"
		if responses {
			body = "event: error\ndata: {\"type\":\"error\",\"message\":\"translation unavailable\"}\n\n"
		}
		a := &Adaptor{}
		a.Init(info)
		_, apiErr := a.DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, info)
		require.Nil(t, apiErr)
		require.True(t, info.StreamStatus.HasErrors())
		require.Contains(t, w.Body.String(), "translation unavailable")
		require.NotContains(t, w.Body.String(), "[DONE]")
		require.NotContains(t, w.Body.String(), "response.completed")
	}
}

func TestTruncatedChatStreamDoesNotEmitDone(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	info := testInfo(false, true)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	a := &Adaptor{}
	a.Init(info)
	body := "data: {\"id\":\"chat1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n"
	_, apiErr := a.DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, apiErr)
	require.True(t, info.StreamStatus.HasErrors())
	require.Contains(t, w.Body.String(), "partial")
	require.NotContains(t, w.Body.String(), "[DONE]")
}

func TestErrorBodyWithSuccessStatusIsRejected(t *testing.T) {
	for _, responses := range []bool{false, true} {
		info := testInfo(responses, false)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		a := &Adaptor{}
		a.Init(info)
		_, apiErr := a.DoResponse(c, &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"error":{"message":"backend failed"}}`)),
		}, info)
		require.NotNil(t, apiErr)
		require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
		require.Contains(t, apiErr.Error(), "backend failed")
	}
}
