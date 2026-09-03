package openai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	projectcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/modal"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModalAdaptorMetadata(t *testing.T) {
	adaptor := &Adaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeModal}})

	require.Equal(t, modal.ChannelName, adaptor.GetChannelName())
	require.Empty(t, adaptor.GetModelList())
}

func TestModalChatCompletionRequest(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}

	const (
		modelName = "orcarouter/Qwen3.8-27B-Uncensored-FP8"
		proxyKey  = "modal-token-id.modal-token-secret"
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer "+proxyKey, r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"model":"`+modelName+`"`)
		require.Contains(t, string(body), `"stream":false`)
		require.Contains(t, string(body), `"max_tokens":2048`)
		require.NotContains(t, string(body), "max_completion_tokens")
		require.Contains(t, string(body), `"temperature":0.3`)
		require.Contains(t, string(body), `"reasoning_effort":"none"`)
		require.NotContains(t, string(body), "stream_options")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-modal"}`))
	}))
	defer upstream.Close()

	stream := false
	temperature := 0.3
	topP := 0.9
	maxTokens := uint(2048)
	request := &dto.GeneralOpenAIRequest{
		Model:           modelName,
		Stream:          &stream,
		StreamOptions:   &dto.StreamOptions{IncludeUsage: true},
		MaxTokens:       &maxTokens,
		ReasoningEffort: "none",
		Temperature:     &temperature,
		TopP:            &topP,
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeModal,
			ChannelBaseUrl:       upstream.URL + "/v1/chat/completions",
			ApiKey:               proxyKey,
			SupportStreamOptions: false,
			UpstreamModelName:    modelName,
		},
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	convertedRequest, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Nil(t, convertedRequest.StreamOptions)

	body, err := projectcommon.Marshal(convertedRequest)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("Content-Type", "application/json")

	rawResp, err := (&Adaptor{}).DoRequest(c, info, bytes.NewReader(body))
	require.NoError(t, err)
	resp, ok := rawResp.(*http.Response)
	require.True(t, ok)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}

func TestModalAdvancedRequestPreservesOpenAICompatibleFields(t *testing.T) {
	stream := true
	maxTokens := uint(2048)
	temperature := 0.3
	topP := 0.9
	reasoning := json.RawMessage(`{"enabled":true}`)
	jsonSchema := json.RawMessage(`{
		"name":"person_info",
		"strict":true,
		"schema":{
			"type":"object",
			"properties":{
				"name":{"type":"string"},
				"age":{"type":"integer"},
				"city":{"type":"string"}
			},
			"required":["name","age","city"],
			"additionalProperties":false
		}
	}`)
	request := &dto.GeneralOpenAIRequest{
		Model: "orcarouter/Qwen3.8-27B-Uncensored-FP8",
		Messages: []dto.Message{
			{Role: "system", Content: "You are a concise technical assistant."},
			{Role: "user", Content: "Extract the name, age, and city: Ada, 36, London."},
		},
		Temperature:   &temperature,
		MaxTokens:     &maxTokens,
		TopP:          &topP,
		Stream:        &stream,
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
		Reasoning:     reasoning,
		ResponseFormat: &dto.ResponseFormat{
			Type:       "json_schema",
			JsonSchema: jsonSchema,
		},
		Tools: []dto.ToolCallRequest{
			{
				Type: "function",
				Function: dto.FunctionRequest{
					Name:        "get_weather",
					Description: "Get the current weather for a city.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []string{"city"},
					},
				},
			},
		},
		ToolChoice: "auto",
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeModal,
			SupportStreamOptions: false,
			UpstreamModelName:    request.Model,
		},
	}

	converted, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	got, ok := converted.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, &maxTokens, got.MaxTokens)
	require.Nil(t, got.MaxCompletionTokens)
	require.Equal(t, &temperature, got.Temperature)
	require.Equal(t, &topP, got.TopP)
	require.Equal(t, "system", got.Messages[0].Role)
	require.JSONEq(t, string(reasoning), string(got.Reasoning))
	require.Equal(t, "json_schema", got.ResponseFormat.Type)
	require.JSONEq(t, string(jsonSchema), string(got.ResponseFormat.JsonSchema))
	require.Len(t, got.Tools, 1)
	require.Equal(t, "get_weather", got.Tools[0].Function.Name)
	require.Equal(t, "auto", got.ToolChoice)
	require.Nil(t, got.StreamOptions)
}
