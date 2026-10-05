package mistral

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestMistralModerationRequest(t *testing.T) {
	for _, input := range []any{"hello", []string{"hello", "world"}, []any{"hello", "world"}} {
		request := &dto.GeneralOpenAIRequest{
			Model: "mistral-moderation-latest", Input: input,
			Stream: common.GetPointer(false), Temperature: common.GetPointer(0.0),
			ReasoningEffort: "high", Messages: []dto.Message{{Role: "user", Content: "ignore"}},
			Metadata: []byte(`{"zero":0,"enabled":false,"empty":null}`),
		}
		converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, testInfo(relayconstant.RelayModeModerations), request)
		require.NoError(t, err)
		body := decodeMap(t, converted)
		require.Len(t, body, 3)
		require.Equal(t, "mistral-moderation-latest", body["model"])
		require.Equal(t, false, body["metadata"].(map[string]any)["enabled"])
		if text, ok := input.(string); ok {
			require.Equal(t, text, body["input"])
		} else {
			require.Equal(t, []any{"hello", "world"}, body["input"])
		}
	}
	for _, input := range []any{nil, "", []string{}, []any{}, []any{"hello", ""}, []any{"hello", 12}, []any{map[string]any{"type": "image_url", "image_url": "https://example.com/image.png"}}} {
		_, err := (&Adaptor{}).convertModerationRequest(&dto.GeneralOpenAIRequest{Input: input})
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, err.(*types.NewAPIError).StatusCode)
	}
	_, err := (&Adaptor{}).convertModerationRequest(&dto.GeneralOpenAIRequest{Input: "hi", Stream: common.GetPointer(true)})
	require.ErrorContains(t, err, "streaming")
	for _, base := range []string{"https://api.mistral.ai", "https://api.mistral.ai/v1/"} {
		info := testInfo(relayconstant.RelayModeModerations)
		info.ChannelBaseUrl = base
		u, err := (&Adaptor{}).GetRequestURL(info)
		require.NoError(t, err)
		require.Equal(t, "https://api.mistral.ai/v1/moderations", u)
	}
}

func TestMistralModerationResponse(t *testing.T) {
	body := `{"id":"mod-1","model":"mistral-moderation-2603","future":false,"results":[{"categories":{"violence_and_threats":false,"financial":false},"category_scores":{"violence_and_threats":0,"financial":0.01}},{"categories":{"violence_and_threats":true,"financial":false},"category_scores":{"violence_and_threats":0.99,"financial":0}}],"usage":{"prompt_tokens":81,"completion_tokens":0,"total_tokens":142,"request_count":1}}`
	c, w := testContext()
	info := testInfo(relayconstant.RelayModeModerations)
	info.ChannelSetting.ForceFormat = true
	usage, apiErr := (&Adaptor{moderationInputs: 2}).DoResponse(c, mockResponse(body), info)
	require.Nil(t, apiErr)
	require.Equal(t, 81, usage.(*dto.Usage).PromptTokens)
	require.Zero(t, usage.(*dto.Usage).CompletionTokens)
	require.Equal(t, 142, usage.(*dto.Usage).TotalTokens)
	var response map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
	require.NotContains(t, response, "choices")
	require.Equal(t, false, response["future"])
	results := response["results"].([]any)
	require.Equal(t, false, results[0].(map[string]any)["flagged"])
	require.Equal(t, true, results[1].(map[string]any)["flagged"])
	require.Equal(t, float64(0), results[0].(map[string]any)["category_scores"].(map[string]any)["violence_and_threats"])
	require.Equal(t, float64(1), response["usage"].(map[string]any)["request_count"])

	c, w = testContext()
	info.SetEstimatePromptTokens(9)
	usage, apiErr = (&Adaptor{}).DoResponse(c, mockResponse(`{"id":"mod-2","model":"old-model","results":[{"categories":{"health":true},"category_scores":{"health":0.8}}]}`), info)
	require.Nil(t, apiErr)
	require.Equal(t, 9, usage.(*dto.Usage).PromptTokens)
	require.Zero(t, usage.(*dto.Usage).CompletionTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
	require.NotContains(t, w.Body.String(), "usage")
}

func TestMistralModerationRejectsMalformedResponse(t *testing.T) {
	for _, body := range []string{
		`null`, `{}`, `{"id":"m","model":"m","results":[]}`,
		`{"id":"m","model":"m","results":[{"category_scores":{"health":0.1}}]}`,
		`{"id":"m","model":"m","results":[{"categories":{"health":null},"category_scores":{"health":0.1}}]}`,
		`{"id":"m","model":"m","results":[{"categories":{"health":false},"category_scores":{"health":null}}]}`,
		`{"id":"m","model":"m","results":[{"categories":{"health":false},"category_scores":{"health":0.1}}],"usage":"invalid"}`,
		`{"object":"error","message":"bad","type":"invalid_request","code":"3051"}`,
	} {
		c, w := testContext()
		_, apiErr := (&Adaptor{moderationInputs: 1}).DoResponse(c, mockResponse(body), testInfo(relayconstant.RelayModeModerations))
		require.NotNil(t, apiErr)
		require.Empty(t, w.Body.String(), "invalid response must not be presented as a safe result")
	}
	c, w := testContext()
	_, apiErr := (&Adaptor{moderationInputs: 2}).DoResponse(c, mockResponse(`{"id":"m","model":"m","results":[{"categories":{"health":false},"category_scores":{"health":0.1}}]}`), testInfo(relayconstant.RelayModeModerations))
	require.NotNil(t, apiErr)
	require.Empty(t, w.Body.String(), "batch results must match the number of submitted inputs")
}

func TestMistralModerationFinalBodyAndUpstreamError(t *testing.T) {
	service.InitHttpClient()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"object":"error","message":"rate limited","type":"rate_limited","code":"1300"}`)
	}))
	defer upstream.Close()
	c, _ := testContext()
	info := testInfo(relayconstant.RelayModeModerations)
	info.ChannelBaseUrl = upstream.URL
	a := &Adaptor{moderationInputs: 1}
	resp, err := a.DoRequest(c, info, strings.NewReader(`{"model":"mistral-moderation-latest","input":["a","b"]}`))
	require.NoError(t, err)
	require.Equal(t, 2, a.moderationInputs)
	httpResp := resp.(*http.Response)
	require.Equal(t, "10", httpResp.Header.Get("Retry-After"))
	apiErr := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
	require.Equal(t, 429, apiErr.StatusCode)
	require.Equal(t, "rate_limited", apiErr.ToOpenAIError().Type)
	_, err = a.DoRequest(c, info, strings.NewReader(`{"model":"m","input":"hi","stream":true}`))
	require.ErrorContains(t, err, "streaming")
}
