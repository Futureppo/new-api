package selfhost

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testInfo(channelType int, responses, stream bool) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: channelType, UpstreamModelName: "qwen3.8-27b-fp8"}, OriginModelName: "qwen3.8-27b-fp8", RelayMode: relayconstant.RelayModeChatCompletions, RelayFormat: types.RelayFormatOpenAI, IsStream: stream, ShouldIncludeUsage: true, DisablePing: true, StartTime: time.Now()}
	if responses {
		info.RelayMode = relayconstant.RelayModeResponses
		info.RelayFormat = types.RelayFormatOpenAIResponses
	}
	return info
}

func TestAdapterAuthAndModelNames(t *testing.T) {
	service.InitHttpClient()
	for _, key := range []string{"", "test-key"} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/prefix/v1/chat/completions", r.URL.Path)
			want := ""
			if key != "" {
				want = "Bearer " + key
			}
			require.Equal(t, want, r.Header.Get("Authorization"))
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{}`)
		}))
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Request.Header.Set("Authorization", "Bearer downstream-secret")
		c.Request.Header.Set("Content-Type", "application/json")
		info := testInfo(constant.ChannelTypeVLLM, false, false)
		info.ApiKey = key
		info.ChannelBaseUrl = upstream.URL + "/prefix/v1/chat/completions"
		a := &VLLMAdaptor{}
		a.Init(info)
		request := &dto.GeneralOpenAIRequest{Model: "gpt-6-astra-high"}
		converted, err := a.ConvertOpenAIRequest(c, info, request)
		require.NoError(t, err)
		require.Same(t, request, converted)
		require.Equal(t, "gpt-6-astra-high", request.Model)
		res, err := a.DoRequest(c, info, strings.NewReader(`{"model":"qwen3.8-27b-fp8","reasoning_effort":"high"}`))
		require.NoError(t, err)
		res.(*http.Response).Body.Close()
		require.Equal(t, "medium", info.ReasoningEffort)
		upstream.Close()
	}
}

func TestAdapterStreamToolsUsageAndErrors(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	chat := []string{
		`{"id":"chat1","model":"qwen3.8-27b-fp8","choices":[{"index":0,"delta":{"reasoning_content":"think"},"finish_reason":null}]}`,
		`{"id":"chat1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"lookup","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`{"id":"chat1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Beijing\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"id":"chat1","choices":[],"usage":{"prompt_tokens":12,"completion_tokens":8,"total_tokens":20,"completion_tokens_details":{"reasoning_tokens":3}}}`,
		`[DONE]`,
	}
	responses := []string{
		`{"type":"response.reasoning_text.delta","delta":"think"}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item1","call_id":"call1","name":"lookup","arguments":""}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item1","output_index":0,"delta":"{\"city\":\"Beijing\"}"}`,
		`{"type":"response.completed","response":{"id":"resp1","status":"completed","output":[],"usage":{"input_tokens":12,"output_tokens":8,"total_tokens":20,"output_tokens_details":{"reasoning_tokens":3}}}}`,
	}
	for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeLiteLLM} {
		for _, isResponses := range []bool{false, true} {
			for _, failed := range []bool{false, true} {
				info := testInfo(channelType, isResponses, true)
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				events := append([]string(nil), chat...)
				if isResponses {
					events = append([]string(nil), responses...)
				}
				if failed {
					events = events[:1]
					if isResponses {
						events = append(events, `{"type":"error","message":"upstream unavailable"}`)
					} else {
						events = append(events, `{"error":{"message":"upstream unavailable","type":null,"code":"503"}}`)
					}
				}
				raw := "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
				a := &Adaptor{}
				a.Init(info)
				usage, apiErr := a.DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(raw))}, info)
				require.Nil(t, apiErr)
				if failed {
					require.Contains(t, w.Body.String(), "upstream unavailable")
					require.True(t, info.StreamStatus.HasErrors())
					require.NotContains(t, w.Body.String(), "[DONE]")
					require.NotContains(t, w.Body.String(), "response.completed")
					continue
				}
				require.Equal(t, 20, usage.(*dto.Usage).TotalTokens)
				require.Equal(t, 3, usage.(*dto.Usage).CompletionTokenDetails.ReasoningTokens)
				require.Contains(t, w.Body.String(), "call1")
				require.Contains(t, w.Body.String(), "lookup")
				require.Contains(t, w.Body.String(), "Beijing")
				require.Contains(t, w.Body.String(), "think")
				if isResponses {
					require.Contains(t, w.Body.String(), "response.completed")
				} else {
					require.Contains(t, w.Body.String(), "[DONE]")
				}
			}
		}
	}
}

func TestAdapterTruncatedStreamAndErrorEnvelope(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 3
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	for _, responses := range []bool{false, true} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		info := testInfo(constant.ChannelTypeLiteLLM, responses, true)
		data := `{"choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`
		if responses {
			data = `{"type":"response.output_text.delta","delta":"partial"}`
		}
		a := &Adaptor{}
		a.Init(info)
		_, apiErr := a.DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: " + data + "\n\n"))}, info)
		require.Nil(t, apiErr)
		require.True(t, info.StreamStatus.HasErrors())
		require.NotContains(t, w.Body.String(), "[DONE]")
		require.NotContains(t, w.Body.String(), "response.completed")
		info = testInfo(constant.ChannelTypeLiteLLM, responses, false)
		_, apiErr = a.DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"backend failed","type":null}}`))}, info)
		require.NotNil(t, apiErr)
		require.Equal(t, 502, apiErr.StatusCode)
		require.Contains(t, apiErr.Error(), "backend failed")
	}
}
