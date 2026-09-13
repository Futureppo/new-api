package mistral

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func mockResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestMistralEmbeddingFormats(t *testing.T) {
	for _, format := range []string{"float", "base64"} {
		a := &Adaptor{}
		converted, err := a.ConvertEmbeddingRequest(nil, nil, dto.EmbeddingRequest{Model: "codestral-embed", Input: []any{"a", "b"}, Dimensions: common.GetPointer(2), EncodingFormat: format})
		require.NoError(t, err)
		body := decodeMap(t, converted)
		require.Equal(t, float64(2), body["output_dimension"])
		require.NotContains(t, body, "dimensions")
		require.Equal(t, "float", body["encoding_format"])
		c, w := testContext()
		info := testInfo(relayconstant.RelayModeEmbeddings)
		info.ChannelSetting.ForceFormat = true
		usage, apiErr := a.DoResponse(c, mockResponse(`{"object":"list","model":"codestral-embed","data":[{"object":"embedding","index":0,"embedding":[1.25,-2.5]},{"object":"embedding","index":1,"embedding":[0,0.5]}],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`), info)
		require.Nil(t, apiErr)
		require.Equal(t, 5, usage.(*dto.Usage).PromptTokens)
		var result map[string]any
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
		require.NotContains(t, result, "choices")
		vector := result["data"].([]any)[0].(map[string]any)["embedding"]
		if format == "base64" {
			encoded, err := base64.StdEncoding.DecodeString(vector.(string))
			require.NoError(t, err)
			require.Len(t, encoded, 8)
			require.Equal(t, float32(1.25), math.Float32frombits(binary.LittleEndian.Uint32(encoded)))
			require.Equal(t, float32(-2.5), math.Float32frombits(binary.LittleEndian.Uint32(encoded[4:])))
		} else {
			require.Equal(t, []any{1.25, -2.5}, vector)
		}
	}
	for _, input := range []any{nil, []any{}, []any{1, 2}, []any{"text", 1}} {
		_, err := (&Adaptor{}).ConvertEmbeddingRequest(nil, nil, dto.EmbeddingRequest{Input: input})
		require.Error(t, err)
	}
}

func TestMistralAudioUsageNormalization(t *testing.T) {
	for _, prompt := range []int{6, 381} {
		usage := &dto.Usage{PromptTokens: prompt, CompletionTokens: 17, TotalTokens: 398, PromptTokensDetails: dto.InputTokenDetails{AudioTokens: 375, CachedTokens: 1}}
		normalizeUsage(usage)
		normalizeUsage(usage)
		require.Equal(t, 381, usage.PromptTokens)
		require.Equal(t, 398, usage.TotalTokens)
		require.Equal(t, 6, usage.PromptTokensDetails.TextTokens)
		require.Equal(t, 17, usage.CompletionTokenDetails.TextTokens)
		require.Equal(t, 1, usage.PromptTokensDetails.CachedTokens)
		params := service.BuildTieredTokenParams(usage, false, map[string]bool{"ai": true})
		require.Equal(t, float64(6), params.P)
		require.Equal(t, float64(17), params.C)
		require.Equal(t, float64(375), params.AI)
		require.Equal(t, float64(381), params.Len)
		require.Equal(t, float64(381), service.BuildTieredTokenParams(usage, false, nil).P)
	}
}

func TestMistralStreamTerminalToolsThinkingAndUsage(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	input := "data: " + `{"id":"chat-1","object":"chat.completion.chunk","created":123,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"reason"}]}]},"finish_reason":null}]}` + "\n\n" +
		"data: " + `{"id":"chat-1","object":"chat.completion.chunk","created":123,"model":"m","choices":[{"index":0,"delta":{"content":"answer","tool_calls":[{"index":0,"id":"abc123XYZ","type":"function","function":{"name":"f","arguments":{"x":1}}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":6,"completion_tokens":17,"total_tokens":398,"prompt_tokens_details":{"audio_tokens":375}}}` + "\n\ndata: [DONE]\n\n"
	for _, include := range []bool{true, false} {
		c, w := testContext()
		info := testInfo(relayconstant.RelayModeChatCompletions)
		info.IsStream = true
		info.ShouldIncludeUsage = include
		info.ChannelSetting.ForceFormat = true
		result, apiErr := (&Adaptor{}).DoResponse(c, mockResponse(input), info)
		require.Nil(t, apiErr)
		require.Equal(t, 381, result.(*dto.Usage).PromptTokens)
		require.Equal(t, 1, strings.Count(w.Body.String(), "[DONE]"))
		require.Contains(t, w.Body.String(), "reasoning_content")
		require.Contains(t, w.Body.String(), "abc123XYZ")
		require.Contains(t, w.Body.String(), `"arguments":"{\"x\":1}"`)
		chunks := []map[string]any{}
		for _, line := range strings.Split(w.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: {") {
				continue
			}
			var chunk map[string]any
			require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &chunk))
			chunks = append(chunks, chunk)
		}
		expected := 2
		if include {
			expected = 3
		}
		require.Len(t, chunks, expected)
		require.Nil(t, chunks[1]["usage"])
		require.Equal(t, "tool_calls", chunks[1]["choices"].([]any)[0].(map[string]any)["finish_reason"])
		if include {
			require.Empty(t, chunks[2]["choices"])
			require.NotNil(t, chunks[2]["usage"])
		}
	}
}

func TestMistralStreamErrorsAndTruncation(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	first := "data: " + `{"id":"chat-1","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}` + "\n\n"
	for _, end := range []string{
		"data: " + `{"object":"error","message":"capacity","type":"rate_limited","code":"1300"}` + "\n\n",
		"data: {broken\n\n", "", "data: [DONE]\n\n",
		"data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"error"}]}` + "\n\ndata: [DONE]\n\n",
	} {
		c, w := testContext()
		info := testInfo(relayconstant.RelayModeChatCompletions)
		info.IsStream = true
		_, apiErr := (&Adaptor{}).DoResponse(c, mockResponse(first+end), info)
		require.Nil(t, apiErr, "a partial stream must not trigger an HTTP retry")
		require.Contains(t, w.Body.String(), `"error"`)
		require.NotContains(t, w.Body.String(), "[DONE]")
		require.True(t, info.StreamStatus.HasErrors())
	}
}

func TestMistralStreamClientCancellationClosesUpstream(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	c, w := testContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	reader, writer := io.Pipe()
	defer writer.Close()
	resp := mockResponse("")
	resp.Body = reader
	info := testInfo(relayconstant.RelayModeChatCompletions)
	info.IsStream = true
	finished := make(chan struct{})
	go func() { defer close(finished); _, _ = (&Adaptor{}).DoResponse(c, resp, info) }()
	cancel()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not close after client cancellation")
	}
	require.NotContains(t, w.Body.String(), "[DONE]")
	_, err := writer.Write([]byte("data: test\n"))
	require.Error(t, err)
}

func TestMistralStreamToolArgumentDeltasAndMultipleChoices(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	first := "data: " + `{"id":"chat-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"abc123XYZ","type":"function","function":{"name":"weather","arguments":"{\"city\":"}}]},"finish_reason":null},{"index":1,"delta":{"content":"second"},"finish_reason":null}]}` + "\n\n"
	terminal := "data: " + `{"id":"chat-1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":10,"total_tokens":15}}` + "\n\n"
	secondEnd := "data: " + `{"id":"chat-1","choices":[{"index":1,"delta":{},"finish_reason":"stop"}]}` + "\n\n"
	for _, complete := range []bool{true, false} {
		c, w := testContext()
		info := testInfo(relayconstant.RelayModeChatCompletions)
		info.IsStream = true
		info.Request = &dto.GeneralOpenAIRequest{N: common.GetPointer(2)}
		input := first + terminal
		if complete {
			input += secondEnd
		}
		_, apiErr := (&Adaptor{}).DoResponse(c, mockResponse(input+"data: [DONE]\n\n"), info)
		require.Nil(t, apiErr)
		require.Equal(t, complete, strings.Contains(w.Body.String(), "[DONE]"))
		require.Equal(t, !complete, strings.Contains(w.Body.String(), `"error":`))
		var arguments strings.Builder
		for _, line := range strings.Split(w.Body.String(), "\n") {
			if !strings.HasPrefix(line, "data: {") {
				continue
			}
			var chunk dto.ChatCompletionsStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &chunk))
			for _, choice := range chunk.Choices {
				for _, call := range choice.Delta.ToolCalls {
					arguments.WriteString(call.Function.Arguments)
				}
			}
		}
		require.JSONEq(t, `{"city":"Paris"}`, arguments.String())
	}
}
