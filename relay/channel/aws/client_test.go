package aws

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrockTypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestParseAwsCredential(t *testing.T) {
	t.Parallel()

	t.Run("api key", func(t *testing.T) {
		credential, err := parseAwsCredential(" bearer-token | us-east-1 ", dto.AwsKeyTypeApiKey)
		require.NoError(t, err)
		require.Equal(t, awsCredentialModeAPIKey, credential.mode)
		require.Equal(t, "bearer-token", credential.apiKey)
		require.Equal(t, "us-east-1", credential.region)
	})

	t.Run("ak sk", func(t *testing.T) {
		credential, err := parseAwsCredential("access|secret|us-west-2", dto.AwsKeyTypeAKSK)
		require.NoError(t, err)
		require.Equal(t, awsCredentialModeAKSK, credential.mode)
		require.Equal(t, "access", credential.accessKeyID)
		require.Equal(t, "secret", credential.secretAccessKey)
		require.Equal(t, "us-west-2", credential.region)
	})

	t.Run("legacy inference", func(t *testing.T) {
		apiCredential, err := parseAwsCredential("token|eu-west-1", "")
		require.NoError(t, err)
		require.Equal(t, awsCredentialModeAPIKey, apiCredential.mode)

		akskCredential, err := parseAwsCredential("access|secret|ap-southeast-1", "")
		require.NoError(t, err)
		require.Equal(t, awsCredentialModeAKSK, akskCredential.mode)
	})

	t.Run("invalid values", func(t *testing.T) {
		_, err := parseAwsCredential("token|not-a-region", dto.AwsKeyTypeApiKey)
		require.EqualError(t, err, "invalid AWS region")

		_, err = parseAwsCredential("access|secret|us-east-1", dto.AwsKeyTypeApiKey)
		require.EqualError(t, err, "invalid AWS API key, expected APIKey|Region")

		_, err = parseAwsCredential("token|us-east-1", dto.AwsKeyTypeAKSK)
		require.EqualError(t, err, "invalid AWS access key, expected AccessKey|SecretAccessKey|Region")
	})
}

func TestNewAwsRuntimeClientUsesSelectedAuthentication(t *testing.T) {
	t.Parallel()

	apiCredential, err := parseAwsCredential("only-the-token|us-east-1", dto.AwsKeyTypeApiKey)
	require.NoError(t, err)
	apiClient := newAwsRuntimeClient(apiCredential, http.DefaultClient)
	token, err := apiClient.Options().BearerAuthTokenProvider.RetrieveBearerToken(context.Background())
	require.NoError(t, err)
	require.Equal(t, "only-the-token", token.Value)
	require.Nil(t, apiClient.Options().Credentials)

	akskCredential, err := parseAwsCredential("access|secret|us-east-1", dto.AwsKeyTypeAKSK)
	require.NoError(t, err)
	akskClient := newAwsRuntimeClient(akskCredential, http.DefaultClient)
	credentials, err := akskClient.Options().Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	require.Equal(t, "access", credentials.AccessKeyID)
	require.Equal(t, "secret", credentials.SecretAccessKey)
	require.Nil(t, akskClient.Options().BearerAuthTokenProvider)
}

func TestNewAwsHTTPClientAlwaysReturnsAClient(t *testing.T) {
	t.Parallel()
	httpClient, err := newAwsHTTPClient("")
	require.NoError(t, err)
	require.NotNil(t, httpClient)
}

func TestAwsSDKClientsSendExpectedAuthorization(t *testing.T) {
	t.Parallel()

	newCaptureClient := func(t *testing.T, responseBody string, capturedAuthorization *string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			*capturedAuthorization = request.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(responseBody)),
				Request:    request,
			}, nil
		})}
	}

	t.Run("api key control plane bearer", func(t *testing.T) {
		credential, err := parseAwsCredential("only-the-token|us-east-1", dto.AwsKeyTypeApiKey)
		require.NoError(t, err)
		authorization := ""
		client := newAwsBedrockClient(
			credential,
			newCaptureClient(t, `{"modelSummaries":[]}`, &authorization),
			func(options *bedrock.Options) { options.BaseEndpoint = aws.String("https://bedrock.test") },
		)
		_, err = client.ListFoundationModels(context.Background(), &bedrock.ListFoundationModelsInput{
			ByProvider:       aws.String("Anthropic"),
			ByOutputModality: bedrockTypes.ModelModalityText,
			ByInferenceType:  bedrockTypes.InferenceTypeOnDemand,
		})
		require.NoError(t, err)
		require.Equal(t, "Bearer only-the-token", authorization)
	})

	t.Run("api key runtime bearer", func(t *testing.T) {
		credential, err := parseAwsCredential("only-the-token|us-east-1", dto.AwsKeyTypeApiKey)
		require.NoError(t, err)
		authorization := ""
		client := newAwsRuntimeClient(
			credential,
			newCaptureClient(t, `{}`, &authorization),
			func(options *bedrockruntime.Options) {
				options.BaseEndpoint = aws.String("https://bedrock-runtime.test")
			},
		)
		_, err = client.InvokeModel(context.Background(), &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String("us.anthropic.claude-sonnet-4-6"),
			ContentType: aws.String("application/json"),
			Body:        []byte(`{}`),
		})
		require.NoError(t, err)
		require.Equal(t, "Bearer only-the-token", authorization)
	})

	t.Run("ak sk sigv4", func(t *testing.T) {
		credential, err := parseAwsCredential("access|secret|us-east-1", dto.AwsKeyTypeAKSK)
		require.NoError(t, err)
		authorization := ""
		client := newAwsBedrockClient(
			credential,
			newCaptureClient(t, `{"modelSummaries":[]}`, &authorization),
			func(options *bedrock.Options) { options.BaseEndpoint = aws.String("https://bedrock.test") },
		)
		_, err = client.ListFoundationModels(context.Background(), &bedrock.ListFoundationModelsInput{})
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(authorization, "AWS4-HMAC-SHA256 "))
		require.Contains(t, authorization, "Credential=access/")
	})
}
