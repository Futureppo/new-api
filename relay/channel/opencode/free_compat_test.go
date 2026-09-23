package opencode

// Adapted from piexian/new-api (AGPL-3.0), commit afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f.
// https://github.com/piexian/new-api/tree/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f/relay/channel/opencode

import (
	"bytes"
	"context"
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

const freeChatFixture = "data: {\"id\":\"chat_test\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"mimo-v2.5-free\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"think\",\"content\":\"O\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"K\"},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7,\"prompt_tokens_details\":{\"cached_tokens\":1}}}\n\n" + "data: [DONE]\n\n"

const freeResponsesFixture = `event: response.created
data: {"type":"response.created","response":{"id":"resp_test","model":"muse-spark-1.3-contributor-free","created_at":1}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_test","delta":"OK"}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_test","object":"response","model":"muse-spark-1.3-contributor-free","status":"completed","created_at":1,"output":[{"id":"msg_test","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7,"input_tokens_details":{"cached_tokens":1}},"extra":false}}

`

func TestFreeShapePreservesToolsAndZeroFields(t *testing.T) {
	for _, mode := range []constant.OpenCodeEndpoint{constant.OpenCodeEndpointChat, constant.OpenCodeEndpointResponses} {
		a := &Adaptor{freeRequestMode: mode}
		tools := `[{"type":"function","function":{"name":"bash","description":"caller","parameters":{"type":"object","properties":{"timeout":{"default":0}},"additionalProperties":false}}}]`
		if mode == constant.OpenCodeEndpointResponses {
			tools = `[{"type":"function","name":"bash","parameters":{"type":"object","properties":{"timeout":{"default":0}},"additionalProperties":false}}]`
		}
		body := []byte(`{"tools":` + tools + `,"stream":false,"store":false,"temperature":0,"large":9007199254740993,"unknown":null,"tool_choice":"auto"}`)
		shaped, injected, err := a.shapeFreeRequest(body)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(shaped, "stream").Bool())
		require.Equal(t, gjson.Parse(tools).Array()[0].Raw, gjson.GetBytes(shaped, "tools.0").Raw)
		require.Len(t, gjson.GetBytes(shaped, "tools").Array(), 5)
		require.False(t, injected["bash"])
		require.True(t, injected["read"])
		require.Equal(t, "9007199254740993", gjson.GetBytes(shaped, "large").Raw)
		require.Equal(t, "false", gjson.GetBytes(shaped, "store").Raw)
		require.Equal(t, "0", gjson.GetBytes(shaped, "temperature").Raw)
		require.Equal(t, "null", gjson.GetBytes(shaped, "unknown").Raw)
		require.Equal(t, "auto", gjson.GetBytes(shaped, "tool_choice").String())
	}
	for _, mode := range []constant.OpenCodeEndpoint{constant.OpenCodeEndpointChat, constant.OpenCodeEndpointResponses} {
		a := &Adaptor{freeRequestMode: mode}
		body, _, err := a.shapeFreeRequest([]byte(`{"model":"test"}`))
		require.NoError(t, err)
		if mode == constant.OpenCodeEndpointChat {
			require.Equal(t, "none", gjson.GetBytes(body, "tool_choice").String())
			require.True(t, gjson.GetBytes(body, "stream_options.include_usage").Bool())
		} else {
			require.False(t, gjson.GetBytes(body, "tool_choice").Exists())
			require.False(t, gjson.GetBytes(body, "stream_options").Exists())
		}
	}
}

func TestFreeCompatibilityScope(t *testing.T) {
	for _, tc := range []struct {
		model  string
		format types.RelayFormat
		mode   int
		want   bool
	}{
		{"mimo-v2.5-free", types.RelayFormatOpenAI, relayconstant.RelayModeUnknown, true},
		{"muse-spark-1.3-contributor-free", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses, true},
		{"big-pickle", types.RelayFormatOpenAI, relayconstant.RelayModeUnknown, true},
		{"mimo-v2.5", types.RelayFormatOpenAI, relayconstant.RelayModeUnknown, false},
		{"omen-alpha", types.RelayFormatOpenAI, relayconstant.RelayModeUnknown, false},
		{"jev-1.13-free", types.RelayFormatTypeSafe, relayconstant.RelayModeTypeSafeSystemOne, false},
		{"jev-1.13-free", types.RelayFormatOpenAI, relayconstant.RelayModeUnknown, false},
		{"claude-free", types.RelayFormatClaude, relayconstant.RelayModeUnknown, false},
		{"muse-spark-1.3-contributor-free", types.RelayFormatOpenAIResponsesCompaction, relayconstant.RelayModeResponsesCompact, false},
	} {
		info := &relaycommon.RelayInfo{RelayFormat: tc.format, RelayMode: tc.mode, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenCode, ChannelBaseUrl: constant.ChannelBaseURLs[constant.ChannelTypeOpenCode], UpstreamModelName: tc.model}}
		a := &Adaptor{}
		a.Init(info)
		require.Equal(t, tc.want, a.needsFreeCompatibility(info), tc.model)
	}
}

func TestFreeFullAdaptorPaths(t *testing.T) {
	service.InitHttpClient()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = old })
	for _, upstreamResponses := range []bool{false, true} {
		for _, clientResponses := range []bool{false, true} {
			if clientResponses && !upstreamResponses {
				continue
			}
			for _, stream := range []bool{false, true} {
				t.Run(strings.Join([]string{boolLabel(upstreamResponses), boolLabel(clientResponses), boolLabel(stream)}, "-"), func(t *testing.T) {
					sent := make(chan []byte, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, _ := io.ReadAll(r.Body)
						sent <- body
						w.Header().Set("Content-Type", "text/event-stream")
						if upstreamResponses {
							_, _ = io.WriteString(w, freeResponsesFixture)
						} else {
							_, _ = io.WriteString(w, freeChatFixture)
						}
					}))
					defer server.Close()
					model := "mimo-v2.5-free"
					if upstreamResponses {
						model = "muse-spark-1.3-contributor-free"
					}
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					c.Request.Header.Set("Content-Type", "application/json")
					info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeUnknown, IsStream: stream, DisablePing: true, ShouldIncludeUsage: true, ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenCode, ChannelBaseUrl: server.URL, UpstreamModelName: model, ApiKey: "test"}}
					if clientResponses {
						info.RelayFormat = types.RelayFormatOpenAIResponses
						info.RelayMode = relayconstant.RelayModeResponses
					}
					a := &Adaptor{}
					a.Init(info)
					var value any
					var err error
					if clientResponses {
						request := dto.OpenAIResponsesRequest{}
						raw, _ := common.Marshal(map[string]any{"model": model, "input": "Say OK", "stream": stream})
						require.NoError(t, common.Unmarshal(raw, &request))
						value, err = a.ConvertOpenAIResponsesRequest(c, info, request)
					} else {
						request := dto.GeneralOpenAIRequest{}
						raw, _ := common.Marshal(map[string]any{"model": model, "messages": []any{map[string]any{"role": "user", "content": "Say OK"}}, "stream": stream})
						require.NoError(t, common.Unmarshal(raw, &request))
						value, err = a.ConvertOpenAIRequest(c, info, &request)
						if upstreamResponses {
							var converted *dto.OpenAIResponsesRequest
							converted, err = service.ChatCompletionsRequestToResponsesRequest(&request)
							require.NoError(t, err)
							info.RelayMode = relayconstant.RelayModeResponses
							value, err = a.ConvertOpenAIResponsesRequest(c, info, *converted)
						}
					}
					require.NoError(t, err)
					body, err := common.Marshal(value)
					require.NoError(t, err)
					reply, err := a.DoRequest(c, info, bytes.NewReader(body))
					require.NoError(t, err)
					sentBody := <-sent
					require.True(t, gjson.GetBytes(sentBody, "stream").Bool())
					require.Len(t, gjson.GetBytes(sentBody, "tools").Array(), 5)
					resp := reply.(*http.Response)
					require.Equal(t, stream, info.IsStream)
					if !stream {
						require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
					}
					var usage any
					var apiErr *types.NewAPIError
					if upstreamResponses && !clientResponses {
						usage, apiErr = a.DoResponsesToChatResponse(c, resp, info)
					} else {
						usage, apiErr = a.DoResponse(c, resp, info)
					}
					require.Nil(t, apiErr)
					require.Equal(t, 7, usage.(*dto.Usage).TotalTokens)
					if !stream {
						require.True(t, gjson.Valid(recorder.Body.String()))
						if clientResponses {
							require.Equal(t, "response", gjson.Get(recorder.Body.String(), "object").String())
						} else {
							require.Equal(t, "OK", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
						}
					} else {
						require.Contains(t, recorder.Body.String(), "data:")
					}
				})
			}
		}
	}
}
func boolLabel(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestFreeStreamRejectsUnsafeOrIncompleteEvents(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		mode       constant.OpenCodeEndpoint
	}{
		{"truncated", strings.ReplaceAll(freeChatFixture, "data: [DONE]\n\n", ""), constant.OpenCodeEndpointChat},
		{"no choices", "data: [DONE]\n\n", constant.OpenCodeEndpointChat},
		{"malformed", "data: broken\n\n", constant.OpenCodeEndpointChat},
		{"error", `data: {"error":{"message":"secret upstream text"}}` + "\n\n", constant.OpenCodeEndpointChat},
		{"synthetic", `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"bash","arguments":"{}"}}]}}]}` + "\n\n", constant.OpenCodeEndpointChat},
		{"split synthetic", `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"ba"}}]}}]}` + "\n\n" + `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"sh","arguments":"{}"}}]}}]}` + "\n\n", constant.OpenCodeEndpointChat},
		{"split legacy synthetic", `data: {"choices":[{"index":0,"delta":{"function_call":{"name":"ba"}}}]}` + "\n\n" + `data: {"choices":[{"index":0,"delta":{"function_call":{"name":"sh","arguments":"{}"}}}]}` + "\n\n", constant.OpenCodeEndpointChat},
		{"responses synthetic", `data: {"type":"response.output_item.added","item":{"type":"function_call","name":"bash","arguments":"{}"}}` + "\n\n", constant.OpenCodeEndpointResponses},
		{"responses failure", `data: {"type":"response.failed","response":{"error":{"message":"secret upstream text"}}}` + "\n\n", constant.OpenCodeEndpointResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(tc.body)), tc.mode, map[string]bool{"bash": true}, time.Second)
			defer s.Close()
			body, err := io.ReadAll(s)
			require.Error(t, err)
			require.NotContains(t, string(body), `"arguments":"{}"`)
			require.NotContains(t, err.Error(), "secret upstream text")
		})
	}
}

func TestFreeStreamTimeoutAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		reader, writer := io.Pipe()
		defer writer.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		timeout := 20 * time.Millisecond
		if cancelled {
			timeout = time.Hour
			cancel()
		}
		s := newFreeStream(ctx, reader, constant.OpenCodeEndpointChat, nil, timeout)
		_, err := io.ReadAll(s)
		require.Error(t, err)
		require.NoError(t, s.Close())
	}
}

func TestFreeStreamMultilineAndCallerTools(t *testing.T) {
	body := `data: {"id":"chat_test","choices":[
data: {"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"bash","arguments":"{\"x\":"}}]},"finish_reason":null}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"0}"}}]},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}

data: [DONE]

`
	s := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(body)), constant.OpenCodeEndpointChat, map[string]bool{"read": true}, time.Second)
	defer s.Close()
	result, err := collapseFreeStream(s)
	require.NoError(t, err)
	require.Equal(t, "bash", gjson.GetBytes(result, "choices.0.message.tool_calls.0.function.name").String())
	require.Equal(t, `{"x":0}`, gjson.GetBytes(result, "choices.0.message.tool_calls.0.function.arguments").String())
	require.Equal(t, int64(7), gjson.GetBytes(result, "usage.total_tokens").Int())
}
