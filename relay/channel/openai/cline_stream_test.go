package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func clineSSE(chunks ...string) string {
	return "data: " + strings.Join(chunks, "\n\ndata: ") + "\n\n"
}

func TestClineForcesUpstreamStream(t *testing.T) {
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-stream", true: "stream"}[stream], func(t *testing.T) {
			captured := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				captured <- body
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, clineSSE(
					`{"id":"test","model":"cline-free/a","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`,
					`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10}}`, "[DONE]"))
			}))
			defer upstream.Close()
			info := clineTestInfo(stream)
			info.ChannelBaseUrl = upstream.URL
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			// The final request body may come from passthrough or parameter overrides.
			body := `{"model":"cline-free/a","messages":[{"role":"user","content":"hi"}],"stream":false,"stream_options":{"include_usage":false,"include_obfuscation":false},"temperature":0,"parallel_tool_calls":false,"vendor_option":{"enabled":false}}`
			a := &Adaptor{}
			raw, err := a.DoRequest(c, info, strings.NewReader(body))
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, common.Unmarshal(<-captured, &got))
			require.Equal(t, true, got["stream"])
			require.Equal(t, map[string]any{"include_usage": true, "include_obfuscation": false}, got["stream_options"])
			require.Equal(t, float64(0), got["temperature"])
			require.Equal(t, false, got["parallel_tool_calls"])
			require.Equal(t, map[string]any{"enabled": false}, got["vendor_option"])
			require.Equal(t, stream, info.IsStream)
			usage, apiErr := a.DoResponse(c, raw.(*http.Response), info)
			require.Nil(t, apiErr)
			require.Equal(t, 10, usage.(*dto.Usage).TotalTokens)
			if stream {
				require.Contains(t, w.Body.String(), "[DONE]")
			} else {
				require.Equal(t, "application/json", w.Header().Get("Content-Type"))
				require.Empty(t, w.Header().Get("Transfer-Encoding"))
				var response dto.OpenAITextResponse
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
				require.Equal(t, "OK", response.Choices[0].Message.StringContent())
				require.Equal(t, "chat.completion", response.Object)
			}
		})
	}
}

func TestClineStreamAggregation(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	body := clineSSE(
		`{"id":"test","model":"provider/model","created":123,"system_fingerprint":"fp-test","choices":[{"index":1,"delta":{"role":"assistant","content":"Second"}},{"index":0,"delta":{"role":"assistant","reasoning":"Think ","reasoning_details":[{"index":0,"type":"reasoning.text","text":"Think ","format":"unknown"}],"tool_calls":[{"index":1,"id":"call-b","type":"function","function":{"name":"other","arguments":"{"}},{"index":0,"id":"call-a","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning":"done","reasoning_details":[{"index":0,"type":"reasoning.text","text":"done","format":"unknown"}],"content":"O","tool_calls":[{"index":0,"function":{"arguments":"\"hi\"}"}},{"index":1,"function":{"arguments":"}"}}]},"logprobs":{"content":[{"token":"O"}]}},{"index":1,"delta":{"content":" choice"},"finish_reason":"stop"}]}`,
		`{"choices":[{"index":0,"delta":{"content":"K"},"finish_reason":"tool_calls","logprobs":{"content":[{"token":"K"}]}}]}`,
		`{"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":3,"total_tokens":10,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`, "[DONE]")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := clineTestInfo(false)
	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, nil)
	info.StreamStatus.RecordError("previous attempt failed")
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	usage, apiErr := (&Adaptor{}).DoResponse(c, resp, info)
	require.Nil(t, apiErr)
	require.Equal(t, 10, usage.(*dto.Usage).TotalTokens)
	var response dto.OpenAITextResponse
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.Len(t, response.Choices, 2)
	require.Equal(t, "OK", response.Choices[0].Message.StringContent())
	require.Equal(t, "Think done", response.Choices[0].Message.Reasoning)
	require.Equal(t, "Second choice", response.Choices[1].Message.StringContent())
	var calls []dto.ToolCallResponse
	require.NoError(t, common.Unmarshal(response.Choices[0].Message.ToolCalls, &calls))
	require.Len(t, calls, 2)
	require.Equal(t, "call-a", calls[0].ID)
	require.Equal(t, "lookup", calls[0].Function.Name)
	require.JSONEq(t, `{"q":"hi"}`, calls[0].Function.Arguments)
	require.Nil(t, calls[0].Index)
	require.Equal(t, "call-b", calls[1].ID)
	require.JSONEq(t, `{}`, calls[1].Function.Arguments)
	require.Contains(t, w.Body.String(), `"system_fingerprint":"fp-test"`)
	require.Contains(t, w.Body.String(), `"text":"Think done"`)
	require.Contains(t, w.Body.String(), `"cached_tokens":2`)
	require.Contains(t, w.Body.String(), `"reasoning_tokens":1`)
	var logprobs map[string][]any
	require.NoError(t, common.Unmarshal(response.Choices[0].Logprobs, &logprobs))
	require.Len(t, logprobs["content"], 2)
}

func TestClineStreamAggregationFailures(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	partial := `{"choices":[{"index":0,"delta":{"content":"partial"}}]}`
	for _, tc := range []struct {
		name, body      string
		cancel, timeout bool
	}{
		{name: "empty", body: clineSSE("[DONE]")},
		{name: "truncated", body: clineSSE(partial)},
		{name: "premature done", body: clineSSE(partial, "[DONE]")},
		{name: "upstream error", body: clineSSE(partial, `{"error":{"message":"upstream failed","code":503}}`, "[DONE]")},
		{name: "invalid JSON", body: clineSSE(partial, "not json")},
		{name: "invalid chunk", body: clineSSE(`{}`, "[DONE]")},
		{name: "cancelled", cancel: true},
		{name: "timeout", timeout: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			body := io.NopCloser(strings.NewReader(tc.body))
			if tc.timeout {
				reader, writer := io.Pipe()
				defer writer.Close()
				body = reader
			}
			info := clineTestInfo(false)
			info.DisablePing = false
			_, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, info)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.False(t, c.Writer.Written())
			require.Empty(t, w.Body.String())
			require.Empty(t, w.Header().Get("Content-Type"))
		})
	}
}
