package aws

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/smithy-go/auth/bearer"
)

type awsCredentialMode int

const (
	awsCredentialModeAPIKey awsCredentialMode = iota + 1
	awsCredentialModeAKSK
)

type awsCredential struct {
	mode            awsCredentialMode
	apiKey          string
	accessKeyID     string
	secretAccessKey string
	region          string
}

var awsRegionPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+-\d+$`)

func parseAwsCredential(rawKey string, keyType dto.AwsKeyType) (awsCredential, error) {
	parts := strings.Split(strings.TrimSpace(rawKey), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	if keyType == "" {
		switch len(parts) {
		case 2:
			keyType = dto.AwsKeyTypeApiKey
		case 3:
			keyType = dto.AwsKeyTypeAKSK
		}
	}

	var credential awsCredential
	switch keyType {
	case dto.AwsKeyTypeApiKey:
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return credential, fmt.Errorf("invalid AWS API key, expected APIKey|Region")
		}
		credential = awsCredential{
			mode:   awsCredentialModeAPIKey,
			apiKey: parts[0],
			region: parts[1],
		}
	case dto.AwsKeyTypeAKSK:
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return credential, fmt.Errorf("invalid AWS access key, expected AccessKey|SecretAccessKey|Region")
		}
		credential = awsCredential{
			mode:            awsCredentialModeAKSK,
			accessKeyID:     parts[0],
			secretAccessKey: parts[1],
			region:          parts[2],
		}
	default:
		return credential, fmt.Errorf("unsupported AWS key type %q", keyType)
	}

	if !awsRegionPattern.MatchString(credential.region) {
		return awsCredential{}, fmt.Errorf("invalid AWS region")
	}
	return credential, nil
}

func newAwsHTTPClient(proxy string) (*http.Client, error) {
	if strings.TrimSpace(proxy) == "" {
		httpClient := service.GetHttpClient()
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		return httpClient, nil
	}
	httpClient, err := service.NewProxyHttpClient(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return httpClient, nil
}

func newAwsRuntimeClient(credential awsCredential, httpClient *http.Client, optionFns ...func(*bedrockruntime.Options)) *bedrockruntime.Client {
	options := bedrockruntime.Options{
		Region:     credential.region,
		HTTPClient: httpClient,
	}
	if credential.mode == awsCredentialModeAPIKey {
		options.BearerAuthTokenProvider = bearer.StaticTokenProvider{
			Token: bearer.Token{Value: credential.apiKey},
		}
	} else {
		options.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			credential.accessKeyID,
			credential.secretAccessKey,
			"",
		))
	}
	for _, optionFn := range optionFns {
		optionFn(&options)
	}
	return bedrockruntime.New(options)
}

func newAwsBedrockClient(credential awsCredential, httpClient *http.Client, optionFns ...func(*bedrock.Options)) *bedrock.Client {
	options := bedrock.Options{
		Region:     credential.region,
		HTTPClient: httpClient,
	}
	if credential.mode == awsCredentialModeAPIKey {
		options.BearerAuthTokenProvider = bearer.StaticTokenProvider{
			Token: bearer.Token{Value: credential.apiKey},
		}
	} else {
		options.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			credential.accessKeyID,
			credential.secretAccessKey,
			"",
		))
	}
	for _, optionFn := range optionFns {
		optionFn(&options)
	}
	return bedrock.New(options)
}
