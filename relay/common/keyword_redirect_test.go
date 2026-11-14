package common

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func keywordTestContext(t *testing.T, format types.RelayFormat, stream bool, rules map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	settings := dto.ChannelSettings{ShowErrorDetails: true, KeywordRedirect: &dto.ChannelKeywordRedirectSettings{Enabled: true, Rules: rules}}
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, settings)
	SetRelayInfo(c, &RelayInfo{RelayFormat: format, IsStream: stream, ChannelMeta: &ChannelMeta{ChannelId: 1, ChannelSetting: settings}})
	InstallRelayResponseWriter(c)
	SetConversationCapture(c, NewConversationCapture())
	if stream {
		c.Header("Content-Type", "text/event-stream")
	} else {
		c.Header("Content-Type", "application/json")
	}
	return c, recorder
}

func keywordWriteChunks(t *testing.T, c *gin.Context, input string, size int) {
	t.Helper()
	for start := 0; start < len(input); start += size {
		chunk := input[start:min(start+size, len(input))]
		AppendConversationClientResponse(c, []byte(chunk))
		n, err := c.Writer.WriteString(chunk)
		require.NoError(t, err)
		require.Equal(t, len(chunk), n)
		c.Writer.Flush()
	}
}

func keywordPayloads(t *testing.T, body string) []gjson.Result {
	t.Helper()
	var result []gjson.Result
	for len(body) != 0 {
		end := keywordFrameEnd([]byte(body))
		require.NotZero(t, end, body)
		frame := parseKeywordFrame([]byte(body[:end]))
		if gjson.ValidBytes(frame.payload) {
			result = append(result, gjson.ParseBytes(frame.payload))
		}
		body = body[end:]
	}
	return result
}

func keywordSSE(t *testing.T, event string, payload any) string {
	t.Helper()
	data, err := basecommon.Marshal(payload)
	require.NoError(t, err)
	if event == "" {
		return "data: " + string(data) + "\n\n"
	}
	return "event: " + event + "\ndata: " + string(data) + "\n\n"
}

func TestKeywordMatcherSemantics(t *testing.T) {
	m := newKeywordMatcher(map[string]string{"苹果": "橘子", "苹果汁": "茶", "a": "b", "b": "c", "删除": "", " line\n": "  ", "😀": "笑"})
	require.Equal(t, "橘子茶bcA  笑", m.replace("苹果苹果汁abA删除 line\n😀"))
	// The shorter exact match must wait when a longer keyword is still possible.
	n, _ := m.next("苹果", false)
	require.Zero(t, n)
	n, replacement := m.next("苹果", true)
	require.Equal(t, len("苹果"), n)
	require.Equal(t, "橘子", replacement)
}

func TestKeywordWriterInstallationAcrossOuterWrappers(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatOpenAI, false, map[string]string{"apple": "pear", "pear": "fruit"})
	c.Writer = &struct{ gin.ResponseWriter }{c.Writer}
	InstallRelayResponseWriter(c)
	keywordWriteChunks(t, c, `{"choices":[{"message":{"content":"apple"}}]}`, 7)
	require.NoError(t, FinalizeRelayResponse(c))
	require.Equal(t, "pear", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
	require.Equal(t, rec.Body.Bytes(), GetConversationCapture(c).Snapshot().ClientResponseBody)
}

func TestKeywordJSONProtocolsAndPartialWrites(t *testing.T) {
	tests := []struct {
		name   string
		format types.RelayFormat
		input  string
		want   map[string]string
	}{
		{"chat", types.RelayFormatOpenAI,
			`{"id":"苹果","model":"苹果","metadata":{"text":"苹果","n":9007199254740993},"choices":[{"message":{"role":"assistant","content":"苹果汁苹果","reasoning_content":"苹果","reasoning":"苹果","tool_calls":[{"function":{"name":"苹果","arguments":"苹果"}}]}}],"usage":{"completion_tokens":42}}`,
			map[string]string{"choices.0.message.content": "茶橘子", "choices.0.message.reasoning_content": "橘子", "choices.0.message.reasoning": "橘子", "choices.0.message.tool_calls.0.function.arguments": "苹果", "model": "苹果", "metadata.text": "苹果", "metadata.n": "9007199254740993", "usage.completion_tokens": "42"}},
		{"chat parts", types.RelayFormatOpenAI,
			`{"choices":[{"message":{"content":[{"type":"text","text":"苹果"},{"type":"image_url","image_url":{"url":"苹果"}}],"refusal":"苹果"}}]}`,
			map[string]string{"choices.0.message.content.0.text": "橘子", "choices.0.message.content.1.image_url.url": "苹果", "choices.0.message.refusal": "橘子"}},
		{"responses", types.RelayFormatOpenAIResponses,
			`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"苹果"}]},{"type":"reasoning","summary":[{"type":"summary_text","text":"苹果"}],"content":[{"type":"reasoning_text","text":"苹果"}],"encrypted_content":"苹果"},{"type":"function_call","arguments":"苹果"}]}`,
			map[string]string{"output.0.content.0.text": "橘子", "output.1.summary.0.text": "橘子", "output.1.content.0.text": "橘子", "output.1.encrypted_content": "苹果", "output.2.arguments": "苹果"}},
		{"claude", types.RelayFormatClaude,
			`{"role":"assistant","content":[{"type":"thinking","thinking":"苹果","signature":"sig"},{"type":"thinking","thinking":"苹果"},{"type":"text","text":"苹果"},{"type":"tool_use","input":{"text":"苹果"}},{"type":"redacted_thinking","data":"苹果"}]}`,
			map[string]string{"content.0.thinking": "苹果", "content.0.signature": "sig", "content.1.thinking": "橘子", "content.2.text": "橘子", "content.3.input.text": "苹果", "content.4.data": "苹果"}},
		{"gemini", types.RelayFormatGemini,
			`{"candidates":[{"content":{"parts":[{"thought":true,"text":"苹果"},{"thoughtSignature":"sig"},{"text":"苹果"},{"functionCall":{"args":{"text":"苹果"}}}]}},{"content":{"parts":[{"thought":true,"text":"苹果"},{"text":"苹果"}]}}]}`,
			map[string]string{"candidates.0.content.parts.0.text": "苹果", "candidates.0.content.parts.2.text": "橘子", "candidates.0.content.parts.3.functionCall.args.text": "苹果", "candidates.1.content.parts.0.text": "橘子", "candidates.1.content.parts.1.text": "橘子"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec := keywordTestContext(t, tt.format, false, map[string]string{"苹果": "橘子", "苹果汁": "茶"})
			c.Header("Content-Length", fmt.Sprint(len(tt.input)))
			keywordWriteChunks(t, c, tt.input, 7)
			require.NoError(t, FinalizeRelayResponse(c))
			require.True(t, gjson.Valid(rec.Body.String()), rec.Body.String())
			for path, want := range tt.want {
				require.Equal(t, want, gjson.Get(rec.Body.String(), path).String(), path)
			}
			require.Empty(t, rec.Result().Header.Get("Content-Length"))
			require.Equal(t, rec.Body.Bytes(), GetConversationCapture(c).Snapshot().ClientResponseBody)
		})
	}
}

func TestKeywordChatStreamEveryTextBoundary(t *testing.T) {
	original := "苹果汁苹果a删除😀z"
	runes := []rune(original)
	for cut := 1; cut < len(runes); cut++ {
		c, rec := keywordTestContext(t, types.RelayFormatOpenAI, true, map[string]string{"苹果": "橘子", "苹果汁": "茶", "a": "b", "b": "c", "删除": "", "😀": "🙂", "zebra": "x"})
		var input strings.Builder
		for _, text := range []string{string(runes[:cut]), string(runes[cut:])} {
			input.WriteString(keywordSSE(t, "", gin.H{"choices": []any{gin.H{"index": 0, "delta": gin.H{"content": text}}}}))
		}
		input.WriteString("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		keywordWriteChunks(t, c, input.String(), 1)
		require.NoError(t, FinalizeRelayResponse(c))
		var text strings.Builder
		for _, payload := range keywordPayloads(t, rec.Body.String()) {
			text.WriteString(payload.Get("choices.0.delta.content").String())
		}
		require.Equal(t, "茶橘子b🙂z", text.String(), "text boundary %d", cut)
		require.Equal(t, rec.Body.Bytes(), GetConversationCapture(c).Snapshot().ClientResponseBody)
	}
}

func TestKeywordSSEPreservesOrderAndSeparatesChoices(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatOpenAI, true, map[string]string{"abcd": "X"})
	first := "id: first\r\nevent: message\r\ndata: {\"choices\":[\r\ndata: {\"index\":0,\"delta\":{\"content\":\"ab\"}}]}\r\n\r\n"
	keywordWriteChunks(t, c, first, 3)
	require.Empty(t, rec.Body.String())
	second := ": ping\n\ndata: {\"choices\":[{\"index\":1,\"delta\":{\"content\":\"cd\"}}]}\n\n"
	keywordWriteChunks(t, c, second, 7)
	require.Empty(t, rec.Body.String())
	keywordWriteChunks(t, c, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"cd\"}}]}\n\ndata: [DONE]\n\n", 5)
	require.NoError(t, FinalizeRelayResponse(c))
	payloads := keywordPayloads(t, rec.Body.String())
	require.Len(t, payloads, 3)
	require.Equal(t, "X", payloads[0].Get("choices.0.delta.content").String())
	require.Equal(t, "cd", payloads[1].Get("choices.0.delta.content").String())
	require.Equal(t, "", payloads[2].Get("choices.0.delta.content").String())
	require.Contains(t, rec.Body.String(), "id: first\r\nevent: message\r\n")
	require.Contains(t, rec.Body.String(), "\r\n\r\n: ping\n\n")
}

func TestKeywordResponsesStreamSnapshotsAndReasoning(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatOpenAIResponses, true, map[string]string{"apple": "pear", "pear": "fruit"})
	input := ""
	for i, event := range []gin.H{
		{"type": "response.reasoning_summary_text.delta", "output_index": 0, "summary_index": 0, "delta": "app"},
		{"type": "response.reasoning_summary_text.delta", "output_index": 0, "summary_index": 0, "delta": "le"},
		{"type": "response.reasoning_summary_text.done", "output_index": 0, "summary_index": 0, "text": "apple"},
		{"type": "response.output_text.delta", "output_index": 1, "content_index": 0, "delta": "app"},
		{"type": "response.function_call_arguments.delta", "output_index": 2, "delta": "apple"},
		{"type": "response.output_text.delta", "output_index": 1, "content_index": 0, "delta": "le"},
		{"type": "response.output_text.done", "output_index": 1, "content_index": 0, "text": "apple"},
		{"type": "response.completed", "response": gin.H{"output": []any{gin.H{"type": "message", "role": "assistant", "content": []any{gin.H{"type": "output_text", "text": "apple"}}}}, "usage": gin.H{"output_tokens": 8}}},
	} {
		event["sequence_number"] = i
		input += keywordSSE(t, event["type"].(string), event)
	}
	keywordWriteChunks(t, c, input, 11)
	require.NoError(t, FinalizeRelayResponse(c))
	payloads := keywordPayloads(t, rec.Body.String())
	require.Len(t, payloads, 8)
	for i, payload := range payloads {
		require.EqualValues(t, i, payload.Get("sequence_number").Int())
	}
	require.Equal(t, "pear", payloads[0].Get("delta").String()+payloads[1].Get("delta").String())
	require.Equal(t, "pear", payloads[2].Get("text").String())
	require.Equal(t, "pear", payloads[3].Get("delta").String()+payloads[5].Get("delta").String())
	require.Equal(t, "apple", payloads[4].Get("delta").String())
	require.Equal(t, "pear", payloads[6].Get("text").String())
	require.Equal(t, "pear", payloads[7].Get("response.output.0.content.0.text").String())
	require.EqualValues(t, 8, payloads[7].Get("response.usage.output_tokens").Int())
}

func TestKeywordClaudeLateSignatureAndUnsignedBlocks(t *testing.T) {
	for _, signed := range []bool{false, true} {
		t.Run(fmt.Sprint(signed), func(t *testing.T) {
			c, rec := keywordTestContext(t, types.RelayFormatClaude, true, map[string]string{"apple": "pear"})
			input := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\n"
			input += "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"app\"}}\n\n"
			input += "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"le\"}}\n\n"
			keywordWriteChunks(t, c, input, 9)
			require.NotContains(t, rec.Body.String(), "thinking_delta")
			ending := ""
			if signed {
				ending = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"signed-apple\"}}\n\n"
			}
			ending += "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
			keywordWriteChunks(t, c, ending, 4)
			require.NoError(t, FinalizeRelayResponse(c))
			if signed {
				require.Equal(t, input+ending, rec.Body.String())
			} else {
				payloads := keywordPayloads(t, rec.Body.String())
				require.Equal(t, "pear", payloads[1].Get("delta.thinking").String()+payloads[2].Get("delta.thinking").String())
			}
		})
	}
}

func TestKeywordGeminiLateSignatureAndCrossFrameText(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatGemini, true, map[string]string{"apple": "pear"})
	for _, parts := range []string{
		`[{"thought":true,"text":"app"}]`,
		`[{"thought":true,"text":"le"}]`,
		`[{"thoughtSignature":"signed-apple"}]`,
		`[{"text":"app"}]`,
		`[{"text":"le"}]`,
	} {
		keywordWriteChunks(t, c, `data: {"candidates":[{"index":0,"content":{"parts":`+parts+`}}]}`+"\n\n", 7)
	}
	keywordWriteChunks(t, c, "data: {\"candidates\":[{\"index\":0,\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"totalTokenCount\":123}}\n\n", 7)
	require.NoError(t, FinalizeRelayResponse(c))
	payloads := keywordPayloads(t, rec.Body.String())
	require.Len(t, payloads, 6)
	path := "candidates.0.content.parts.0.text"
	require.Equal(t, "apple", payloads[0].Get(path).String()+payloads[1].Get(path).String())
	require.Equal(t, "signed-apple", payloads[2].Get("candidates.0.content.parts.0.thoughtSignature").String())
	require.Equal(t, "pear", payloads[3].Get(path).String()+payloads[4].Get(path).String())
	require.EqualValues(t, 123, payloads[5].Get("usageMetadata.totalTokenCount").Int())
}

func TestKeywordGeminiSignatureAfterAnswerStarts(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatGemini, true, map[string]string{"apple": "pear"})
	for _, parts := range []string{
		`[{"thought":true,"text":"apple"}]`,
		`[{"text":"apple"}]`,
		`[{"thoughtSignature":"late-signature"}]`,
	} {
		keywordWriteChunks(t, c, `data: {"candidates":[{"content":{"parts":`+parts+`}}]}`+"\n\n", 3)
	}
	require.NoError(t, FinalizeRelayResponse(c))
	payloads := keywordPayloads(t, rec.Body.String())
	require.Equal(t, "apple", payloads[0].Get("candidates.0.content.parts.0.text").String())
	require.Equal(t, "pear", payloads[1].Get("candidates.0.content.parts.0.text").String())
}

func TestKeywordFailedResponseSnapshotAndErrorPrivacy(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatOpenAIResponses, true, map[string]string{"apple": "pear", "server_error": "incorrect"})
	keywordWriteChunks(t, c, `event: response.failed
data: {"type":"response.failed","response":{"output":[{"type":"message","content":[{"type":"output_text","text":"apple"}]}],"error":{"code":"server_error","message":"apple"}}}

`, 7)
	require.NoError(t, FinalizeRelayResponse(c))
	payload := keywordPayloads(t, rec.Body.String())[0]
	require.Equal(t, "pear", payload.Get("response.output.0.content.0.text").String())
	require.Equal(t, "apple", payload.Get("response.error.message").String())
	require.Equal(t, "server_error", payload.Get("response.error.code").String())

	c, rec = keywordTestContext(t, types.RelayFormatOpenAI, true, map[string]string{"apple": "pear"})
	settings := GetRelayInfo(c).ChannelSetting
	settings.ShowErrorDetails = false
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, settings)
	keywordWriteChunks(t, c, "event: error\ndata: {\"code\":\"server_error\",\"message\":\"private apple\"}\n\n", 1)
	require.NoError(t, FinalizeRelayResponse(c))
	require.NotContains(t, rec.Body.String(), "private")
	require.Contains(t, rec.Body.String(), "server_error")
}

func TestKeywordIncompleteJSONAndBufferLimit(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatOpenAI, false, map[string]string{"apple": "pear"})
	keywordWriteChunks(t, c, `{"choices":[`, 1)
	require.ErrorContains(t, FinalizeRelayResponse(c), "incomplete JSON")
	require.Empty(t, rec.Body.String())

	c, _ = keywordTestContext(t, types.RelayFormatOpenAI, true, map[string]string{"apple": "pear"})
	_, err := c.Writer.Write(make([]byte, keywordRedirectBufferLimit+1))
	require.ErrorContains(t, err, "buffer limit")
	require.ErrorContains(t, FinalizeRelayResponse(c), "buffer limit")
}

func TestKeywordEOFFinalizationAndRetry(t *testing.T) {
	c, rec := keywordTestContext(t, types.RelayFormatOpenAI, true, map[string]string{"apple": "pear"})
	keywordWriteChunks(t, c, `data: {"choices":[{"delta":{"content":"app"}}]}`, 2)
	require.Empty(t, rec.Body.String())
	require.NoError(t, FinalizeRelayResponse(c))
	want := rec.Body.String()
	require.Equal(t, "app", keywordPayloads(t, want)[0].Get("choices.0.delta.content").String())
	require.NoError(t, FinalizeRelayResponse(c))
	require.Equal(t, want, rec.Body.String())

	c, rec = keywordTestContext(t, types.RelayFormatOpenAI, true, map[string]string{"apple": "pear"})
	keywordWriteChunks(t, c, `data: {"choices":[{"delta":{"content":"app"}}]}`+"\n\n", 3)
	settings := dto.ChannelSettings{ShowErrorDetails: true, KeywordRedirect: &dto.ChannelKeywordRedirectSettings{Enabled: true, Rules: map[string]string{"apple": "orange"}}}
	basecommon.SetContextKey(c, constant.ContextKeyChannelSetting, settings)
	GetRelayInfo(c).InitChannelMeta(c)
	keywordWriteChunks(t, c, `data: {"choices":[{"delta":{"content":"apple"}}]}`+"\n\ndata: [DONE]\n\n", 3)
	require.NoError(t, FinalizeRelayResponse(c))
	require.Equal(t, "orange", keywordPayloads(t, rec.Body.String())[0].Get("choices.0.delta.content").String())
	require.NotContains(t, rec.Body.String(), "pear")
}

func TestKeywordDisabledErrorsAndModelMapping(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIRealtime} {
		c, rec := keywordTestContext(t, format, false, map[string]string{"apple": "pear"})
		if format == types.RelayFormatOpenAI {
			GetRelayInfo(c).ChannelSetting.KeywordRedirect.Enabled = false
		}
		input := `{"choices":[{"message":{"content":"apple"}}]}`
		keywordWriteChunks(t, c, input, len(input))
		require.NoError(t, FinalizeRelayResponse(c))
		require.Equal(t, input, rec.Body.String())
	}
	c, rec := keywordTestContext(t, types.RelayFormatOpenAI, false, map[string]string{"apple": "pear"})
	c.Status(http.StatusBadRequest)
	input := `{"error":{"code":"apple","message":"apple"}}`
	keywordWriteChunks(t, c, input, len(input))
	require.Equal(t, input, rec.Body.String())

	c, rec = keywordTestContext(t, types.RelayFormatOpenAI, false, map[string]string{"apple": "pear"})
	info := GetRelayInfo(c)
	info.IsModelMapped, info.ChannelSetting.ModelMappingFullEnabled = true, true
	info.ClientModelName, info.UpstreamModelName, info.ModelMappingTargetName = "alias", "upstream", "upstream"
	keywordWriteChunks(t, c, `{"model":"upstream","choices":[{"message":{"content":"apple"}}]}`, 1)
	require.Equal(t, "alias", gjson.Get(rec.Body.String(), "model").String())
	require.Equal(t, "pear", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
}
