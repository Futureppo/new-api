package bilibili_web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/bilibili"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const textSSE = "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\",\"reasoning_content\":\"\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":23,\"completion_tokens\":2,\"total_tokens\":25}}\n\ndata: [DONE]\n\n"

func testInfo(responses, stream bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeBilibiliWeb, UpstreamModelName: DefaultModel},
		OriginModelName: "translation-alias", RelayMode: relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI, IsStream: stream, ShouldIncludeUsage: true,
		DisablePing: true, StartTime: time.Now(),
		PriceData: types.PriceData{UsePrice: true, ModelPrice: 0.01},
	}
	if responses {
		info.RelayMode = relayconstant.RelayModeResponses
		info.RelayFormat = types.RelayFormatOpenAIResponses
	}
	return info
}

func testContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	// A downstream gateway token must never be reused for upstream authorization.
	c.Request.Header.Set("Authorization", "Bearer downstream-secret")
	return c, w
}

func TestURLNormalization(t *testing.T) {
	for _, suffix := range []string{"", "/", "/v1", "/v1/", "/v1/chat/completions", "/v1/responses/"} {
		got, err := webURL("https://proxy.example/prefix"+suffix, "/v1/chat/completions")
		require.NoError(t, err)
		u, err := url.Parse(got)
		require.NoError(t, err)
		require.Equal(t, "/prefix/", u.Path)
		require.Equal(t, "/v1/chat/completions", u.Query().Get("p"))
	}
	got, err := webURL("", "/api/echo/result?job_id="+url.QueryEscape("a+b &c"))
	require.NoError(t, err)
	u, err := url.Parse(got)
	require.NoError(t, err)
	require.Equal(t, "index-translate.bilibili.com", u.Host)
	nested, err := url.Parse(u.Query().Get("p"))
	require.NoError(t, err)
	require.Equal(t, "a+b &c", nested.Query().Get("job_id"))
	require.Empty(t, u.Query().Get("job_id"))
	for _, bad := range []string{"ftp://host", "https://user:pass@host", "https://host/?p=/site/index.html", "https://host/#x"} {
		_, err = webURL(bad, "/v1/chat/completions")
		require.Error(t, err, bad)
	}
	info := testInfo(false, false)
	info.RelayMode = relayconstant.RelayModeEmbeddings
	_, err = (&Adaptor{}).GetRequestURL(info)
	require.Error(t, err)
}

func TestTextProtocolsAndModels(t *testing.T) {
	service.InitHttpClient()
	for _, model := range ModelList[:3] {
		for _, responses := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/responses=%t/stream=%t", model, responses, stream), func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/prefix/", r.URL.Path)
						require.Equal(t, "/v1/chat/completions", r.URL.Query().Get("p"))
						require.Empty(t, r.Header.Get("Authorization"))
						require.Equal(t, bilibili.UserAgent, r.Header.Get("User-Agent"))
						body, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						require.Equal(t, model, gjson.GetBytes(body, "model").String())
						require.Equal(t, "0", gjson.GetBytes(body, "temperature").Raw)
						require.Equal(t, "false", gjson.GetBytes(body, "chat_template_kwargs.enable_thinking").Raw)
						require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
						require.True(t, gjson.GetBytes(body, "stream").Bool())
						require.Equal(t, "Translate faithfully", gjson.GetBytes(body, "messages.0.content").String())
						require.Equal(t, "你好", gjson.GetBytes(body, "messages.1.content").String())
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, textSSE)
					}))
					defer server.Close()
					info := testInfo(responses, stream)
					info.ChannelBaseUrl = server.URL + "/prefix/v1/"
					a := &Adaptor{}
					a.Init(info)
					c, w := testContext()
					raw := fmt.Sprintf(`{"model":%q,"messages":[{"role":"system","content":"Translate faithfully"},{"role":"user","content":"你好"}],"stream":%t}`, model, stream)
					if responses {
						raw = fmt.Sprintf(`{"model":%q,"instructions":"Translate faithfully","input":"你好","stream":%t,"metadata":{"job":"test"}}`, model, stream)
					}
					result, err := a.DoRequest(c, info, strings.NewReader(raw))
					require.NoError(t, err)
					resp := result.(*http.Response)
					if !stream {
						require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
					}
					usage, apiErr := a.DoResponse(c, resp, info)
					require.Nil(t, apiErr)
					require.Equal(t, 25, usage.(*dto.Usage).TotalTokens)
					require.Equal(t, stream, info.IsStream)
					if stream {
						require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
						var events []string
						var sequence int64
						for _, line := range strings.Split(w.Body.String(), "\n") {
							if !strings.HasPrefix(line, "data: {") {
								continue
							}
							payload := strings.TrimPrefix(line, "data: ")
							require.True(t, gjson.Valid(payload))
							if responses {
								events = append(events, gjson.Get(payload, "type").String())
								require.Equal(t, sequence, gjson.Get(payload, "sequence_number").Int())
								sequence++
							}
						}
						if responses {
							require.Equal(t, []string{"response.created", "response.in_progress", "response.output_item.added", "response.content_part.added",
								"response.output_text.delta", "response.output_text.delta", "response.output_text.done",
								"response.content_part.done", "response.output_item.done", "response.completed"}, events)
							require.Contains(t, w.Body.String(), `"input_tokens":23`)
						} else {
							require.Contains(t, w.Body.String(), `"total_tokens":25`)
							require.True(t, strings.HasSuffix(w.Body.String(), "data: [DONE]\n\n"))
						}
					} else if responses {
						require.Equal(t, "Hello world", gjson.Get(w.Body.String(), "output.0.content.0.text").String())
						require.Equal(t, "completed", gjson.Get(w.Body.String(), "status").String())
						require.Equal(t, "test", gjson.Get(w.Body.String(), "metadata.job").String())
					} else {
						require.Equal(t, "Hello world", gjson.Get(w.Body.String(), "choices.0.message.content").String())
					}
					require.Contains(t, w.Body.String(), "translation-alias")
				})
			}
		}
	}
}

func TestAuthenticationOverridesProxyAndExplicitValues(t *testing.T) {
	service.InitHttpClient()
	for _, override := range []bool{false, true} {
		info := testInfo(false, false)
		info.ApiKey = " upstream-key "
		expectedAuth, expectedAgent := "Bearer upstream-key", bilibili.UserAgent
		if override {
			info.HeadersOverride = map[string]any{"Authorization": "Bearer admin-key", "User-Agent": "admin-agent", "X-Test": "override"}
			expectedAuth, expectedAgent = "Bearer admin-key", "admin-agent"
		}
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "unreachable.invalid", r.URL.Host)
			require.Equal(t, expectedAuth, r.Header.Get("Authorization"))
			require.Equal(t, expectedAgent, r.Header.Get("User-Agent"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			for _, key := range []string{"temperature", "top_p", "max_tokens", "seed", "frequency_penalty"} {
				require.Equal(t, "0", gjson.GetBytes(body, key).Raw, key)
			}
			require.Equal(t, "false", gjson.GetBytes(body, "chat_template_kwargs.enable_thinking").Raw)
			require.Equal(t, "false", gjson.GetBytes(body, "chat_template_kwargs.custom").Raw)
			require.False(t, gjson.GetBytes(body, "max_completion_tokens").Exists())
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, textSSE)
		}))
		info.ChannelBaseUrl = "http://unreachable.invalid"
		info.ChannelSetting.Proxy = proxy.URL
		a := &Adaptor{}
		a.Init(info)
		c, _ := testContext()
		resp, err := a.DoRequest(c, info, strings.NewReader(`{"model":"index-mt-9b-syllable","messages":[{"role":"user","content":"hi"}],"temperature":0,"top_p":0,"max_completion_tokens":0,"seed":0,"frequency_penalty":0,"chat_template_kwargs":{"enable_thinking":false,"custom":false}}`))
		require.NoError(t, err)
		resp.(*http.Response).Body.Close()
		proxy.Close()
	}
}

func TestResponseErrorsAndIncomplete(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		status       int
		sse, success bool
	}{
		{"truncated", strings.ReplaceAll(textSSE, "data: [DONE]\n\n", ""), 200, true, false},
		{"error", "data: {\"error\":{\"message\":\"busy\"}}\n\ndata: [DONE]", 200, true, false},
		{"bad-json", "data: broken\n\ndata: [DONE]", 200, true, false},
		{"no-finish", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\ndata: [DONE]", 200, true, false},
		{"no-text", `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`, 200, false, false},
		{"http-error", `{"error":{"message":"busy"}}`, 429, false, false},
		{"json", `{"choices":[{"message":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`, 200, false, true},
		{"estimated", `{"choices":[{"message":{"content":"Hello world"},"finish_reason":"stop"}]}`, 200, false, true},
		{"length", strings.ReplaceAll(textSSE, `"stop"`, `"length"`), 200, true, true},
	} {
		for _, responses := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/responses=%t/stream=%t", tc.name, responses, stream), func(t *testing.T) {
					a := &Adaptor{}
					info := testInfo(responses, stream)
					a.Init(info)
					a.upstreamSSE = tc.sse
					c, w := testContext()
					usage, apiErr := a.DoResponse(c, &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, info)
					if !tc.success {
						require.NotNil(t, apiErr)
						require.Nil(t, usage)
						require.NotContains(t, w.Body.String(), "data: [DONE]")
						require.NotContains(t, w.Body.String(), "response.completed")
						if w.Body.Len() > 0 {
							require.True(t, c.GetBool("bilibili_web_response_committed"))
							require.False(t, info.StreamStatus.IsNormalEnd())
						}
						return
					}
					require.Nil(t, apiErr)
					if tc.name == "json" {
						require.Zero(t, usage.(*dto.Usage).TotalTokens)
						require.Empty(t, usage.(*dto.Usage).UsageSource, "explicit zero usage must not be replaced by an estimate")
					} else if tc.name == "estimated" {
						require.Positive(t, usage.(*dto.Usage).CompletionTokens)
						require.Equal(t, "bilibili_web_estimated", usage.(*dto.Usage).UsageSource)
					} else if responses {
						require.Contains(t, w.Body.String(), `"incomplete"`)
						require.Contains(t, w.Body.String(), `"max_output_tokens"`)
					}
				})
			}
		}
	}
}

func TestResponsesConversionAndUnsupportedInput(t *testing.T) {
	req, _, err := responsesToChat([]byte(`{"model":"index-nailong-9b","instructions":"Translate","input":[{"role":"user","content":[{"type":"input_text","text":"你好"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}],"temperature":0,"top_p":0,"max_output_tokens":0,"store":false,"background":false,"parallel_tool_calls":false,"tools":[],"text":{"format":{"type":"text"}}}`))
	require.NoError(t, err)
	require.NoError(t, validateTextRequest(req))
	require.Len(t, req.Messages, 3)
	require.Equal(t, "Hello", req.Messages[2].Content)
	require.NotNil(t, req.MaxTokens)
	require.Zero(t, *req.MaxTokens)
	require.NotNil(t, req.Temperature)
	for _, extra := range []string{`"store":true`, `"background":true`, `"previous_response_id":"resp_old"`, `"conversation":"conv_old"`,
		`"tools":[{"type":"web_search"}]`, `"text":{"format":{"type":"json_object"}}`, `"text":{"format":{"type":"text"},"verbosity":"high"}`,
		`"input":[{"type":"function_call_output","output":"hi"}]`, `"input":[{"role":"user","content":[{"type":"input_audio","data":"x"}]}]`} {
		_, _, err = responsesToChat([]byte(`{"model":"index-nailong-9b","input":"hi",` + extra + `}`))
		require.Error(t, err, extra)
	}
	for _, raw := range []string{
		`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/x.png"}}]}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"n":2}`,
		`{"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function"}]}`,
		`{"messages":[{"role":"user","content":"hi"}],"chat_template_kwargs":{"enable_thinking":true}}`,
		`{"messages":[{"role":"tool","content":"hi"}]}`,
	} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(raw, &req))
		require.Error(t, validateTextRequest(&req), raw)
	}
}

func TestEchoValidation(t *testing.T) {
	for name, mutate := range map[string]func(*dto.GeneralOpenAIRequest, *relaycommon.RelayInfo){
		"stream":  func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) { r.Stream = common.GetPointer(true) },
		"pricing": func(_ *dto.GeneralOpenAIRequest, i *relaycommon.RelayInfo) { i.PriceData.UsePrice = false },
		"same-language": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			r.BilibiliWeb = []byte(`{"source_language":"en","target_language":"en"}`)
		},
		"empty-language": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			r.BilibiliWeb = []byte(`{"source_language":""}`)
		},
		"language": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			r.BilibiliWeb = []byte(`{"source_language":"fr"}`)
		},
		"voice":           func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) { r.Audio = []byte(`{"voice":"alloy"}`) },
		"output-format":   func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) { r.Audio = []byte(`{"format":"mp3"}`) },
		"no-audio-output": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) { r.Modalities = []byte(`["text"]`) },
		"multiple-inputs": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			r.Messages = append(r.Messages, r.Messages[0])
		},
		"prompt": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			r.Messages[0].Content = "Translate this audio"
		},
		"temperature": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) { r.Temperature = common.GetPointer(0.0) },
		"top-k":       func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) { r.TopK = common.GetPointer(0) },
		"invalid-audio": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			r.Messages[0].Content = []any{map[string]any{"type": "input_audio", "input_audio": map[string]string{"format": "wav", "data": base64.StdEncoding.EncodeToString([]byte("invalid"))}}}
		},
		"too-long": func(r *dto.GeneralOpenAIRequest, _ *relaycommon.RelayInfo) {
			// A real 11-second mono PCM WAV; no external codec or network fixture.
			audio := make([]byte, 44+11*16000*2)
			copy(audio, "RIFF")
			binary.LittleEndian.PutUint32(audio[4:], uint32(len(audio)-8))
			copy(audio[8:], "WAVEfmt ")
			binary.LittleEndian.PutUint32(audio[16:], 16)
			binary.LittleEndian.PutUint16(audio[20:], 1)
			binary.LittleEndian.PutUint16(audio[22:], 1)
			binary.LittleEndian.PutUint32(audio[24:], 16000)
			binary.LittleEndian.PutUint32(audio[28:], 32000)
			binary.LittleEndian.PutUint16(audio[32:], 2)
			binary.LittleEndian.PutUint16(audio[34:], 16)
			copy(audio[36:], "data")
			binary.LittleEndian.PutUint32(audio[40:], uint32(len(audio)-44))
			r.Messages[0].Content = []any{map[string]any{"type": "input_audio", "input_audio": map[string]string{"format": "wav", "data": base64.StdEncoding.EncodeToString(audio)}}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			req, info := EchoTestRequest("index-echo-2b-s2st"), testInfo(false, false)
			mutate(req, info)
			c, _ := testContext()
			_, err := prepareEcho(c, info, req)
			require.Error(t, err)
		})
	}
	req, info := EchoTestRequest("index-echo-9b-s2st"), testInfo(false, false)
	req.BilibiliWeb = nil
	info.ChannelOtherSettings.BilibiliWebSourceLanguage = "ja"
	info.ChannelOtherSettings.BilibiliWebTargetLanguage = "ko"
	c, _ := testContext()
	input, err := prepareEcho(c, info, req)
	require.NoError(t, err)
	require.Equal(t, "ja", input.source)
	require.Equal(t, "ko", input.target)
	require.Equal(t, "9b", input.model)
}

func TestEchoUploadPollingAndFailure(t *testing.T) {
	service.InitHttpClient()
	for _, outcome := range []string{"success-2b", "success-9b", "nested-error", "submit-error", "bad-wav", "timeout", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			var submits, polls atomic.Int32
			c, w := testContext()
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer custom", r.Header.Get("Authorization"))
				require.Equal(t, "custom-agent", r.Header.Get("User-Agent"))
				path := r.URL.Query().Get("p")
				w.Header().Set("Content-Type", "application/json")
				if path == "/api/echo/s2st" {
					submits.Add(1)
					require.Equal(t, http.MethodPost, r.Method)
					require.NoError(t, r.ParseMultipartForm(8<<20))
					defer r.MultipartForm.RemoveAll()
					require.Equal(t, "en", r.FormValue("src"))
					require.Equal(t, "zh", r.FormValue("lang"))
					wantModel := "2b"
					if outcome == "success-9b" {
						wantModel = "9b"
					}
					require.Equal(t, wantModel, r.FormValue("model"))
					file, header, err := r.FormFile("file")
					require.NoError(t, err)
					data, err := io.ReadAll(file)
					file.Close()
					require.NoError(t, err)
					require.Equal(t, "audio.wav", header.Filename)
					require.Equal(t, testAudio, data)
					if outcome == "submit-error" {
						_, _ = io.WriteString(w, `{"ok":false,"error":"unavailable"}`)
					} else {
						_, _ = io.WriteString(w, `{"ok":true,"job_id":"a+b &c"}`)
					}
					return
				}
				poll := polls.Add(1)
				u, err := url.Parse(path)
				require.NoError(t, err)
				require.Equal(t, "/api/echo/result", u.Path)
				require.Equal(t, "a+b &c", u.Query().Get("job_id"))
				require.Equal(t, http.MethodGet, r.Method)
				if outcome == "canceled" {
					cancel()
					return
				}
				if poll == 1 {
					w.WriteHeader(404)
					return
				}
				if poll == 2 || outcome == "timeout" {
					_, _ = io.WriteString(w, `{"ok":true,"status":"running"}`)
					return
				}
				if outcome == "nested-error" {
					_, _ = io.WriteString(w, `{"ok":true,"status":"done","echo":{"ok":false,"error":"ConnectError"}}`)
					return
				}
				audio := testAudio
				if outcome == "bad-wav" {
					audio = []byte("RIFF0000WAVE")
				}
				body, err := common.Marshal(map[string]any{"ok": true, "status": "done", "echo": map[string]any{
					"ok": true, "text": "你好，早上好。", "audio_b64": base64.StdEncoding.EncodeToString(audio),
				}})
				require.NoError(t, err)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			info := testInfo(false, false)
			info.ChannelBaseUrl = server.URL
			info.HeadersOverride = map[string]any{"Authorization": "Bearer custom", "User-Agent": "custom-agent", "Content-Type": "wrong"}
			a := &Adaptor{pollInterval: time.Millisecond, requestTimeout: 200 * time.Millisecond}
			a.Init(info)
			model := "index-echo-2b-s2st"
			if outcome == "success-9b" {
				model = "index-echo-9b-s2st"
			}
			body, err := common.Marshal(EchoTestRequest(model))
			require.NoError(t, err)
			resp, err := a.DoRequest(c, info, bytes.NewReader(body))
			require.EqualValues(t, 1, submits.Load())
			if !strings.HasPrefix(outcome, "success") {
				require.Error(t, err)
				require.Nil(t, resp)
				require.False(t, info.NonTokenUsage)
				require.False(t, a.echoCompleted)
				return
			}
			require.NoError(t, err)
			usage, apiErr := a.DoResponse(c, resp.(*http.Response), info)
			require.Nil(t, apiErr)
			require.True(t, info.NonTokenUsage)
			require.Zero(t, usage.(*dto.Usage).TotalTokens)
			require.Equal(t, "bilibili_web_per_request", usage.(*dto.Usage).UsageSource)
			require.False(t, gjson.Get(w.Body.String(), "usage").Exists())
			require.Equal(t, "你好，早上好。", gjson.Get(w.Body.String(), "choices.0.message.audio.transcript").String())
			audio, err := base64.StdEncoding.DecodeString(gjson.Get(w.Body.String(), "choices.0.message.audio.data").String())
			require.NoError(t, err)
			require.Equal(t, testAudio, audio)
			require.EqualValues(t, 3, polls.Load())
		})
	}
}
