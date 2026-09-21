package typesafe

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTypeSafeURLAndHeaders(t *testing.T) {
	a := &Adaptor{}
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeTypeSafeSystemOne, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeTypeSafe, ApiKey: "upstream-key"}}
	for _, base := range []string{"", "https://api.typesafe.ai", "https://api.typesafe.ai/", "https://api.typesafe.ai/v1", "https://api.typesafe.ai/v1/"} {
		info.ChannelBaseUrl = base
		url, err := a.GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, "https://api.typesafe.ai/v1/systemone", url)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", SystemOnePath, nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	header := http.Header{}
	require.NoError(t, a.SetupRequestHeader(c, &header, info))
	require.Equal(t, "Bearer upstream-key", header.Get("Authorization"))
	require.Equal(t, "application/json", header.Get("Accept"))
	info.RelayMode = relayconstant.RelayModeChatCompletions
	_, err := a.GetRequestURL(info)
	require.Error(t, err)
	_, err = a.ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{})
	require.Error(t, err)
}

func TestTypeSafeResponseUsageAndBufferedErrors(t *testing.T) {
	for _, status := range []int{401, 422, 429, 529} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		body := `{"detail":[{"loc":["body","questions"],"msg":"invalid question"}]}`
		resp := &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"3"}}, Body: io.NopCloser(strings.NewReader(body))}
		usage, apiErr := (&Adaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{})
		require.Nil(t, usage)
		require.NotNil(t, apiErr)
		require.Equal(t, status, apiErr.StatusCode)
		require.False(t, c.Writer.Written(), "upstream errors must not commit before retry")
		require.True(t, WriteUpstreamError(c, apiErr))
		require.Equal(t, status, w.Code)
		require.Equal(t, body, w.Body.String())
		require.Equal(t, "3", w.Header().Get("Retry-After"))
	}
	for _, counts := range []string{`{"input_tokens":100,"output_tokens":20}`, `{"input_tokens":0,"output_tokens":0}`} {
		body := `{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0}},"usage":` + counts + `,"extra":false}`
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		usage, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, &relaycommon.RelayInfo{})
		require.Nil(t, apiErr)
		require.Equal(t, body, w.Body.String())
		u := usage.(*dto.Usage)
		require.Equal(t, u.PromptTokens+u.CompletionTokens, u.TotalTokens)
	}
}

func TestTypeSafeRejectsMalformedSuccess(t *testing.T) {
	for _, body := range []string{
		`not-json`, `null`, `{}`, `{"model":"jev","answers":{}}`,
		`{"model":"jev","answers":{},"usage":{"input_tokens":1}}`,
		`{"model":"jev","answers":{},"usage":{"input_tokens":1,"output_tokens":null}}`,
		`{"model":"jev","answers":{},"usage":{"input_tokens":-1,"output_tokens":0}}`,
		`{"model":"jev","answers":{},"usage":{"input_tokens":1.5,"output_tokens":0}}`,
		`{"model":"jev","answers":{},"usage":{"input_tokens":9223372036854775807,"output_tokens":1}}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		usage, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, &relaycommon.RelayInfo{})
		require.Nil(t, usage)
		require.Equal(t, 502, apiErr.StatusCode)
		require.False(t, c.Writer.Written())
	}
}
