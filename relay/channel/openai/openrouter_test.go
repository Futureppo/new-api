package openai

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
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
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func openRouterTestInfo(mode int, format types.RelayFormat) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RerankerInfo: &relaycommon.RerankerInfo{},
		ChannelMeta:  &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: "https://openrouter.ai/api", ApiKey: "saved-key", SupportStreamOptions: true, UpstreamModelName: "vendor/model"},
		RelayMode:    mode, RelayFormat: format, StartTime: time.Now(), DisablePing: true,
	}
}

func openRouterTestContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenRouter)
	return c, w
}

func TestOpenRouterURLsAndNativeMessages(t *testing.T) {
	for _, tc := range []struct {
		mode   int
		format types.RelayFormat
		path   string
	}{
		{relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI, "chat/completions"},
		{relayconstant.RelayModeUnknown, types.RelayFormatClaude, "messages"},
		{relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses, "responses"},
		{relayconstant.RelayModeEmbeddings, types.RelayFormatEmbedding, "embeddings"},
		{relayconstant.RelayModeRerank, types.RelayFormatRerank, "rerank"},
		{relayconstant.RelayModeImagesGenerations, types.RelayFormatOpenAIImage, "images"},
		{relayconstant.RelayModeImagesEdits, types.RelayFormatOpenAIImage, "images"},
		{relayconstant.RelayModeAudioSpeech, types.RelayFormatOpenAIAudio, "audio/speech"},
		{relayconstant.RelayModeAudioTranscription, types.RelayFormatOpenAIAudio, "audio/transcriptions"},
	} {
		for _, base := range []string{"https://openrouter.ai/api", "https://openrouter.ai/api/v1/", "https://proxy.example/prefix/v1"} {
			info := openRouterTestInfo(tc.mode, tc.format)
			info.ChannelBaseUrl = base
			url, err := (&Adaptor{}).GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")+"/v1/"+tc.path, url)
		}
	}
	for _, mode := range []int{relayconstant.RelayModeResponsesCompact, relayconstant.RelayModeAudioTranslation, relayconstant.RelayModeRealtime} {
		_, err := (&Adaptor{}).GetRequestURL(openRouterTestInfo(mode, types.RelayFormatOpenAI))
		require.Error(t, err)
	}
	c, _ := openRouterTestContext("")
	info := openRouterTestInfo(0, types.RelayFormatClaude)
	req := &dto.ClaudeRequest{Model: "anthropic/claude", MaxTokens: common.GetPointer(uint(0)), Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}}}
	got, err := (&Adaptor{}).ConvertClaudeRequest(c, info, req)
	require.NoError(t, err)
	require.Same(t, req, got)
	require.Equal(t, types.RelayFormat(types.RelayFormatClaude), info.GetFinalRequestRelayFormat())
	info.ChannelOtherSettings.OpenRouterEnterprise = common.GetPointer(true)
	info.RequestURLPath = "/v1/messages"
	url, err := (&Adaptor{}).GetRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://openrouter.ai/api/v1/chat/completions", url)
	got, err = (&Adaptor{}).ConvertClaudeRequest(c, info, req)
	require.NoError(t, err)
	require.IsType(t, &dto.GeneralOpenAIRequest{}, got)
}

func TestOpenRouterChatParametersAndReasoning(t *testing.T) {
	body := `{"model":"vendor/model","messages":[{"role":"assistant","content":null,"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque"}],"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}],"temperature":0,"top_p":0,"seed":0,"max_tokens":0,"parallel_tool_calls":false,"stream_options":{"include_usage":false},"provider":{"allow_fallbacks":false},"plugins":[{"id":"web"}],"session_id":"s","trace":{"trace_id":"t"},"reasoning_effort":"none"}`
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(body), &req))
	info := openRouterTestInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	c, _ := openRouterTestContext(body)
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &req)
	require.NoError(t, err)
	b, err := common.Marshal(converted)
	require.NoError(t, err)
	b, err = validateOpenRouterBody(info, b)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, common.Unmarshal(b, &got))
	for _, key := range []string{"temperature", "top_p", "seed", "max_tokens"} {
		require.Equal(t, float64(0), got[key])
	}
	require.Equal(t, false, got["parallel_tool_calls"])
	require.Equal(t, "none", got["reasoning"].(map[string]any)["effort"])
	require.Contains(t, string(b), "reasoning.encrypted")
	require.Contains(t, string(b), `"allow_fallbacks":false`)
	require.NotNil(t, got["stream_options"])
	_, err = validateOpenRouterBody(info, []byte(`{"model":"m","reasoning":{"effort":"high"},"reasoning_effort":"none"}`))
	require.ErrorContains(t, err, "conflicts")
	info.UpstreamModelName = "vendor/model-thinking"
	req.Reasoning = []byte(`{"enabled":false,"max_tokens":0,"exclude":false}`)
	req.ReasoningEffort = ""
	_, err = convertOpenRouterChat(info, &req)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":false,"max_tokens":0,"exclude":false}`, string(req.Reasoning))
}

func TestOpenRouterFinalValidationAndAuth(t *testing.T) {
	service.InitHttpClient()
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer saved-key", r.Header.Get("Authorization"))
		require.Equal(t, "New API", r.Header.Get("X-OpenRouter-Title"))
		require.Equal(t, "https://www.newapi.ai", r.Header.Get("HTTP-Referer"))
		body, _ := io.ReadAll(r.Body)
		received = string(body)
		_, _ = io.WriteString(w, `{"id":"r","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}`)
	}))
	defer server.Close()
	info := openRouterTestInfo(relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses)
	info.ChannelBaseUrl = server.URL + "/api/v1/"
	c, _ := openRouterTestContext("")
	for _, body := range []string{`{"model":"m","store":true}`, `{"model":"m","background":true}`, `{"model":"m","previous_response_id":""}`, `{"model":"m","previous_response_id":"r"}`} {
		var req dto.OpenAIResponsesRequest
		require.NoError(t, common.Unmarshal([]byte(body), &req))
		copy, err := common.DeepCopy(&req)
		require.NoError(t, err)
		b, err := common.Marshal(copy)
		require.NoError(t, err)
		_, err = (&Adaptor{}).DoRequest(c, info, bytes.NewReader(b))
		require.Error(t, err)
		require.Empty(t, received)
	}
	resp, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{"model":"mapped","input":"hi","store":false,"background":false,"previous_response_id":null,"provider":{"only":["test"]}}`))
	require.NoError(t, err)
	require.Contains(t, received, `"model":"mapped"`)
	usage, apiErr := (&Adaptor{}).DoResponse(c, resp.(*http.Response), info)
	require.Nil(t, apiErr)
	require.Zero(t, usage.(*dto.Usage).TotalTokens)
}

func TestOpenRouterJSONResponses(t *testing.T) {
	for _, tc := range []struct {
		name           string
		mode           int
		format         types.RelayFormat
		body           string
		prompt, output int
	}{
		{"embedding base64", relayconstant.RelayModeEmbeddings, types.RelayFormatEmbedding, `{"object":"list","data":[{"index":0,"embedding":"AAAAAA=="}],"usage":{"prompt_tokens":3,"total_tokens":3},"provider":"test"}`, 3, 0},
		{"messages cache", 0, types.RelayFormatClaude, `{"type":"message","content":[{"type":"thinking","thinking":"x","signature":"opaque"}],"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}`, 10, 2},
		{"responses", relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses, `{"id":"r","output":[],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`, 5, 2},
		{"transcription duration", relayconstant.RelayModeAudioTranscription, types.RelayFormatOpenAIAudio, `{"text":"hello","segments":[{"speaker":0}],"usage":{"seconds":6,"cost":0.1}}`, 100, 0},
		{"transcription zero", relayconstant.RelayModeAudioTranscription, types.RelayFormatOpenAIAudio, `{"text":"","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"seconds":6}}`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := openRouterTestInfo(tc.mode, tc.format)
			c, w := openRouterTestContext("")
			u, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, info)
			require.Nil(t, e)
			usage := u.(*dto.Usage)
			require.Equal(t, tc.prompt, usage.PromptTokens)
			require.Equal(t, tc.output, usage.CompletionTokens)
			require.JSONEq(t, tc.body, w.Body.String())
			if tc.format == types.RelayFormatClaude {
				require.Equal(t, openRouterNativeUsage, usage.UsageSource)
				require.Equal(t, 80, usage.PromptTokensDetails.CachedTokens)
			}
		})
	}
	for _, returnDocs := range []bool{false, true} {
		info := openRouterTestInfo(relayconstant.RelayModeRerank, types.RelayFormatRerank)
		info.ReturnDocuments = returnDocs
		c, w := openRouterTestContext("")
		body := `{"id":"rank","model":"m","provider":"p","results":[{"index":1,"document":{"text":"b","image":"https://example.com/x.png"},"relevance_score":0.9}],"usage":{"total_tokens":30,"search_units":1,"cost":0.01}}`
		u, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, info)
		require.Nil(t, e)
		require.Equal(t, 30, u.(*dto.Usage).PromptTokens)
		require.Contains(t, w.Body.String(), `"search_units":1`)
		require.Contains(t, w.Body.String(), `"provider":"p"`)
		require.Equal(t, returnDocs, strings.Contains(w.Body.String(), `"document"`))
	}
}

func TestOpenRouterStreamsPreserveFramesAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name           string
		mode           int
		format         types.RelayFormat
		body           string
		prompt, output int
	}{
		{"chat final usage", relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI, ": OPENROUTER PROCESSING\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ok\",\"reasoning_details\":[{\"data\":\"opaque\"}]},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\ndata: [DONE]\n\n", 3, 2},
		{"messages", 0, types.RelayFormatClaude, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0,\"cache_read_input_tokens\":40}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", 10, 2},
		{"responses", relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses, "id: 3\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":1,\"total_tokens\":3},\"output\":[]}}\n\n", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := openRouterTestContext("")
			info := openRouterTestInfo(tc.mode, tc.format)
			u, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, info)
			require.Nil(t, e)
			require.Equal(t, tc.prompt, u.(*dto.Usage).PromptTokens)
			require.Equal(t, tc.output, u.(*dto.Usage).CompletionTokens)
			require.Equal(t, tc.body, w.Body.String())
			require.False(t, info.StreamStatus.HasErrors())
		})
	}
}

func TestOpenRouterErrorsAndIncompleteImages(t *testing.T) {
	for _, body := range []string{`{"error":{"code":503,"message":"unavailable"}}`, `{"error":{"code":"provider_error","message":"failed"}}`} {
		c, w := openRouterTestContext("")
		_, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, openRouterTestInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI))
		require.NotNil(t, e)
		require.GreaterOrEqual(t, e.StatusCode, 400)
		require.Empty(t, w.Body.String())
	}
	for _, body := range []string{
		"data: {\"error\":{\"code\":503,\"message\":\"failed\"},\"choices\":[{\"finish_reason\":\"error\"}]}\n\n",
		"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"x\"}\n\ndata: [DONE]\n\n",
		"data: {\"type\":\"image_generation.completed\",\"b64_json\":\"x\",\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n",
	} {
		c, w := openRouterTestContext("")
		info := openRouterTestInfo(relayconstant.RelayModeImagesGenerations, types.RelayFormatOpenAIImage)
		_, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, info)
		require.NotNil(t, e)
		require.True(t, types.IsSkipRetryError(e))
		require.True(t, info.StreamStatus.HasErrors())
		require.True(t, c.GetBool("openrouter_response_committed"))
		require.Equal(t, body, w.Body.String())
		require.Zero(t, c.GetInt("openrouter_completed_images"))
	}
}

func TestOpenRouterImagesAndAudioMultipart(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{true: "image", false: "audio"}[image], func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			require.NoError(t, writer.WriteField("model", "alias"))
			require.NoError(t, writer.WriteField("provider", `{"options":{"test":{"enabled":false}}}`))
			field, filename := "file", "audio.wav"
			format, mode := types.RelayFormat(types.RelayFormatOpenAIAudio), relayconstant.RelayModeAudioTranscription
			if image {
				field, filename = "image[]", "image.png"
				format, mode = types.RelayFormatOpenAIImage, relayconstant.RelayModeImagesEdits
				require.NoError(t, writer.WriteField("prompt", "edit"))
				require.NoError(t, writer.WriteField("seed", "0"))
				require.NoError(t, writer.WriteField("output_compression", "0"))
			} else {
				require.NoError(t, writer.WriteField("temperature", "0"))
				require.NoError(t, writer.WriteField("diarize", "false"))
				require.NoError(t, writer.WriteField("timestamp_granularities[]", "word"))
			}
			part, err := writer.CreateFormFile(field, filename)
			require.NoError(t, err)
			_, err = part.Write([]byte("fixture"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			c, _ := openRouterTestContext("")
			c.Request = httptest.NewRequest("POST", "/v1/images/edits", bytes.NewReader(body.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			defer common.CleanupBodyStorage(c)
			req, err := helper.GetAndValidateRequest(c, format)
			require.NoError(t, err)
			req.SetModelName("mapped")
			info := openRouterTestInfo(mode, format)
			var data []byte
			if image {
				converted, e := (&Adaptor{}).ConvertImageRequest(c, info, *req.(*dto.ImageRequest))
				require.NoError(t, e)
				data, err = common.Marshal(converted)
			} else {
				reader, e := (&Adaptor{}).ConvertAudioRequest(c, info, *req.(*dto.AudioRequest))
				require.NoError(t, e)
				data, err = io.ReadAll(reader)
			}
			require.NoError(t, err)
			require.Contains(t, string(data), `"model":"mapped"`)
			require.Contains(t, string(data), `"enabled":false`)
			if image {
				require.Contains(t, string(data), "data:image/png;base64,")
				require.Contains(t, string(data), `"seed":0`)
				require.Contains(t, string(data), `"output_compression":0`)
			} else {
				require.Contains(t, string(data), `"input_audio"`)
				require.Contains(t, string(data), `"temperature":0`)
				require.Contains(t, string(data), `"diarize":false`)
				require.Contains(t, string(data), `"timestamp_granularities":["word"]`)
			}
		})
	}
}

func TestOpenRouterImageJSONFallbackAndCount(t *testing.T) {
	for _, priced := range []bool{false, true} {
		info := openRouterTestInfo(relayconstant.RelayModeImagesGenerations, types.RelayFormatOpenAIImage)
		info.IsStream = true
		info.PriceData.UsePrice = priced
		c, w := openRouterTestContext("")
		body := `{"created":1,"data":[{"b64_json":"a"},{"b64_json":"b"}],"usage":{"prompt_tokens":0,"completion_tokens":20,"total_tokens":20}}`
		u, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, info)
		require.Nil(t, e)
		require.False(t, info.IsStream)
		require.Equal(t, 20, u.(*dto.Usage).TotalTokens)
		require.JSONEq(t, body, w.Body.String())
		require.Equal(t, 2, c.GetInt("openrouter_completed_images"))
		if priced {
			require.Equal(t, float64(2), info.PriceData.OtherRatios["n"])
		} else {
			require.NotContains(t, info.PriceData.OtherRatios, "n")
		}
	}
}

func TestOpenRouterSpeechTurnsAndBinaryOutput(t *testing.T) {
	var req dto.AudioRequest
	body := `{"model":"tts","input":[{"text":"Hi","voice":"a"},{"text":"Hello","voice":"b"}],"provider":{"zdr":false},"speed":0}`
	require.NoError(t, common.Unmarshal([]byte(body), &req))
	require.Equal(t, "Hi\nHello", req.Input)
	copy, err := common.DeepCopy(&req)
	require.NoError(t, err)
	require.Len(t, copy.InputTurns, 2)
	c, w := openRouterTestContext("")
	info := openRouterTestInfo(relayconstant.RelayModeAudioSpeech, types.RelayFormatOpenAIAudio)
	info.Request = copy
	info.ChannelOtherSettings.AllowSpeed = true
	reader, err := (&Adaptor{}).ConvertAudioRequest(c, info, *copy)
	require.NoError(t, err)
	b, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.JSONEq(t, body, string(b))
	pcm := bytes.Repeat([]byte{1, 2}, 24000)
	u, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"audio/pcm"}, "X-Generation-Id": {"gen-test"}}, Body: io.NopCloser(bytes.NewReader(pcm))}, info)
	require.Nil(t, e)
	require.Equal(t, pcm, w.Body.Bytes())
	require.Equal(t, "gen-test", w.Header().Get("X-Generation-Id"))
	require.Equal(t, 17, u.(*dto.Usage).CompletionTokenDetails.AudioTokens)
}

func TestOpenRouterToolOptionsAndFormatting(t *testing.T) {
	var req dto.GeneralOpenAIRequest
	body := `{"model":"m","tools":[{"type":"function","defer_loading":false,"cache_control":{"type":"ephemeral"},"function":{"name":"f","strict":false,"defer_loading":false}},{"type":"openrouter:datetime_v2","parameters":{"enabled":false}}]}`
	require.NoError(t, common.Unmarshal([]byte(body), &req))
	copy, err := common.DeepCopy(&req)
	require.NoError(t, err)
	raw, err := common.Marshal(copy)
	require.NoError(t, err)
	require.JSONEq(t, body, string(raw))
	c, w := openRouterTestContext("")
	info := openRouterTestInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	info.ChannelSetting.ThinkingToContent = true
	(&Adaptor{}).Init(info)
	stream := "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning\":\"check\",\"reasoning_details\":[{\"data\":\"opaque\"}]}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n"
	_, e := openRouterResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, info)
	require.Nil(t, e)
	require.Contains(t, w.Body.String(), `\u003cthink\u003e\ncheck`)
	require.Contains(t, w.Body.String(), "opaque")
	require.Contains(t, w.Body.String(), `\n\u003c/think\u003e\n`)
}

func TestOpenRouterEnterpriseCompatibility(t *testing.T) {
	info := openRouterTestInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	info.ChannelOtherSettings.OpenRouterEnterprise = common.GetPointer(true)
	c, w := openRouterTestContext("")
	converted, err := (&Adaptor{}).ConvertOpenAIRequest(c, info, &dto.GeneralOpenAIRequest{Model: "m", ReasoningEffort: "none"})
	require.NoError(t, err)
	raw, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"usage":{"include":true}`)
	require.Contains(t, string(raw), `"effort":"none"`)
	body := `{"success":true,"data":{"choices":[{"message":{"content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}}`
	u, e := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, e)
	require.Equal(t, 3, u.(*dto.Usage).TotalTokens)
	require.NotContains(t, w.Body.String(), "success")
	require.Contains(t, w.Body.String(), "OK")
}

func TestOpenRouterCanceledStream(t *testing.T) {
	c, _ := openRouterTestContext("")
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	info := openRouterTestInfo(relayconstant.RelayModeChatCompletions, types.RelayFormatOpenAI)
	_, e := openRouterResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {}\n\n"))}, info)
	require.NotNil(t, e)
	require.True(t, types.IsSkipRetryError(e))
	require.True(t, info.StreamStatus.HasErrors())
}
