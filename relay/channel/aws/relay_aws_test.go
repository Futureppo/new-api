package aws

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoAwsClientRequest_AppliesRuntimeHeaderOverrideToAnthropicBeta(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName:           "claude-3-5-sonnet-20240620",
		IsStream:                  false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"anthropic-beta": "computer-use-2025-01-24",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:            "access-key|secret-key|us-east-1",
			UpstreamModelName: "claude-3-5-sonnet-20240620",
		},
	}

	requestBody := bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`)
	adaptor := &Adaptor{}

	_, err := doAwsClientRequest(ctx, info, adaptor, requestBody)
	require.NoError(t, err)

	awsReq, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
	require.True(t, ok)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(awsReq.Body, &payload))

	anthropicBeta, exists := payload["anthropic_beta"]
	require.True(t, exists)

	values, ok := anthropicBeta.([]any)
	require.True(t, ok)
	require.Equal(t, []any{"computer-use-2025-01-24"}, values)
}

func TestAwsAPIKeyUsesRuntimeSDKForStreamingAndNonStreaming(t *testing.T) {
	t.Parallel()

	for _, isStream := range []bool{false, true} {
		isStream := isStream
		t.Run(map[bool]string{false: "non-stream", true: "stream"}[isStream], func(t *testing.T) {
			t.Parallel()
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

			info := &relaycommon.RelayInfo{
				IsStream: isStream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiKey:            "bearer-token|us-east-1",
					UpstreamModelName: "claude-sonnet-4-6",
					ChannelOtherSettings: dto.ChannelOtherSettings{
						AwsKeyType: dto.AwsKeyTypeApiKey,
					},
				},
			}
			adaptor := &Adaptor{}
			requestURL, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			require.Empty(t, requestURL)

			_, err = doAwsClientRequest(ctx, info, adaptor, bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":8}`))
			require.NoError(t, err)
			token, err := adaptor.AwsClient.Options().BearerAuthTokenProvider.RetrieveBearerToken(context.Background())
			require.NoError(t, err)
			require.Equal(t, "bearer-token", token.Value)

			if isStream {
				request, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput)
				require.True(t, ok)
				require.Equal(t, "us.anthropic.claude-sonnet-4-6", aws.ToString(request.ModelId))
			} else {
				request, ok := adaptor.AwsReq.(*bedrockruntime.InvokeModelInput)
				require.True(t, ok)
				require.Equal(t, "us.anthropic.claude-sonnet-4-6", aws.ToString(request.ModelId))
			}
		})
	}
}

func TestAwsNativeInferenceProfileIsNotDoublePrefixed(t *testing.T) {
	t.Parallel()
	require.False(t, awsModelCanCrossRegion("us.anthropic.claude-sonnet-4-6", "us"))
	require.False(t, awsModelCanCrossRegion("arn:aws:bedrock:us-east-1:123:inference-profile/example", "us"))
}

func TestFormatRequestPreservesExplicitZeroValuesAndContextManagement(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	headers.Set("anthropic-beta", "context-management-2025-06-27")
	request, err := formatRequest(bytes.NewBufferString(`{
		"messages":[{"role":"user","content":"hello"}],
		"max_tokens":0,
		"temperature":0,
		"top_p":0,
		"top_k":0,
		"context_management":{"edits":[{"type":"clear_tool_uses_20250919"}]}
	}`), headers)
	require.NoError(t, err)
	require.NotNil(t, request.MaxTokens)
	require.Zero(t, *request.MaxTokens)
	require.NotNil(t, request.Temperature)
	require.Zero(t, *request.Temperature)
	require.NotNil(t, request.TopP)
	require.Zero(t, *request.TopP)
	require.NotNil(t, request.TopK)
	require.Zero(t, *request.TopK)
	require.JSONEq(t, `{"edits":[{"type":"clear_tool_uses_20250919"}]}`, string(request.ContextManagement))
	require.JSONEq(t, `["context-management-2025-06-27"]`, string(request.AnthropicBeta))

	payload, err := common.Marshal(request)
	require.NoError(t, err)
	var values map[string]any
	require.NoError(t, common.Unmarshal(payload, &values))
	require.Contains(t, values, "max_tokens")
	require.Contains(t, values, "temperature")
	require.Contains(t, values, "top_p")
	require.Contains(t, values, "top_k")
}
