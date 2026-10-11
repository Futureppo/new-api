package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const decisionsResponse = `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":"p","probability":0},{"type":"choice","name":"c","choice":false,"confidence":0,"probabilities":[{"value":false,"probability":0},{"value":"false","probability":1}]},{"type":"score","name":"s","score":0,"confidence":0,"probabilities":[{"value":0,"label":"low","probability":1}]},{"type":"refusal","name":null}],"usage":{"input_tokens":1000,"output_tokens":0,"total_tokens":1000,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":0}},"future":{"flag":false,"value":0}}`

func TestDecisionsResponsePreservesAnswersAndZeroUsage(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"decision-id"}}, Body: io.NopCloser(strings.NewReader(decisionsResponse))}
	usage, apiErr := OpenAIDecisionsHandler(c, resp, &relaycommon.RelayInfo{})
	require.Nil(t, apiErr)
	require.Equal(t, 1000, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Equal(t, 1000, usage.TotalTokens)
	require.NotNil(t, usage.PromptTokensDetails.CacheWriteTokens)
	require.Zero(t, *usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, decisionsResponse, w.Body.String())
}

func TestDecisionsRejectsMalformedResponsesBeforeWriting(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `data: {"model":"gpt-6-luna"}`, `{"error":{"message":"bad request"}}`,
		strings.Replace(decisionsResponse, `"input_tokens":1000`, `"input_tokens":null`, 1),
		strings.Replace(decisionsResponse, `"output_tokens":0`, `"output_tokens":null`, 1),
		strings.Replace(decisionsResponse, `"total_tokens":1000`, `"total_tokens":999`, 1),
		strings.Replace(decisionsResponse, `"input_tokens":1000`, `"input_tokens":-1`, 1),
		strings.Replace(decisionsResponse, `"input_tokens":1000`, `"input_tokens":"1000"`, 1),
		strings.Replace(decisionsResponse, `"probability":0`, `"probability":null`, 1),
		strings.Replace(decisionsResponse, `"probability":0`, `"probability":2`, 1),
		strings.Replace(decisionsResponse, `"choice":false`, `"choice":null`, 1),
		strings.Replace(decisionsResponse, `"score":0`, `"score":null`, 1),
		strings.Replace(decisionsResponse, `"cached_tokens":0`, `"cached_tokens":1001`, 1),
		strings.Replace(decisionsResponse, `"reasoning_tokens":0`, `"reasoning_tokens":1`, 1),
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		usage, apiErr := OpenAIDecisionsHandler(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, &relaycommon.RelayInfo{})
		require.Nil(t, usage, body)
		require.NotNil(t, apiErr, body)
		require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
		require.False(t, c.Writer.Written(), body)
	}
}
