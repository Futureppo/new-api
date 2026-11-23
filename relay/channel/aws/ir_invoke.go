package aws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockruntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// IsBedrockClaudeModel reports whether the upstream model uses Bedrock's Anthropic Messages API
// (as opposed to Amazon Nova's schema).
func (a *Adaptor) IsBedrockClaudeModel(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	return !isNovaModel(getAwsModelID(info.UpstreamModelName))
}

// PrepareRawClaudePayload converts URL-sourced images to base64, injects Bedrock's
// anthropic_version and anthropic_beta header values, and strips model/stream fields.
func (a *Adaptor) PrepareRawClaudePayload(c *gin.Context, info *relaycommon.RelayInfo, claudeJSON []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(claudeJSON, &payload); err != nil {
		return nil, errors.Wrap(err, "unmarshal bedrock claude payload failed")
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	if err := inlineBedrockURLImages(c, payload); err != nil {
		return nil, err
	}

	requestHeader := http.Header{}
	_ = a.SetupRequestHeader(c, &requestHeader, info)
	if headerOverride, err := channel.ResolveHeaderOverride(info, c); err == nil {
		for key, value := range headerOverride {
			requestHeader.Set(key, value)
		}
	} else {
		return nil, err
	}

	if betaHeader := requestHeader.Get("anthropic-beta"); betaHeader != "" {
		existing := make(map[string]struct{})
		var betas []string
		if rawBetas, ok := payload["anthropic_beta"].([]any); ok {
			for _, item := range rawBetas {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					s = strings.TrimSpace(s)
					if _, seen := existing[s]; !seen {
						existing[s] = struct{}{}
						betas = append(betas, s)
					}
				}
			}
		}
		for _, part := range strings.Split(betaHeader, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, seen := existing[part]; !seen {
				existing[part] = struct{}{}
				betas = append(betas, part)
			}
		}
		if len(betas) > 0 {
			payload["anthropic_beta"] = betas
		}
	}

	if existingVersion, ok := payload["anthropic_version"].(string); !ok || strings.TrimSpace(existingVersion) == "" {
		payload["anthropic_version"] = "bedrock-2023-05-31"
	}
	delete(payload, "model")
	delete(payload, "stream")

	return json.Marshal(payload)
}

func awsRequestParentContext(c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}

func (a *Adaptor) resolveClientAndModel(c *gin.Context, info *relaycommon.RelayInfo) (*bedrockruntime.Client, string, *types.NewAPIError) {
	awsCli, err := newAwsClient(c, info)
	if err != nil {
		return nil, "", types.NewError(err, types.ErrorCodeChannelAwsClientError)
	}
	a.AwsClient = awsCli

	awsModelID := getAwsModelID(info.UpstreamModelName)
	awsRegionPrefix := getAwsRegionPrefix(awsCli.Options().Region)
	if awsModelCanCrossRegion(awsModelID, awsRegionPrefix) {
		awsModelID = awsModelCrossRegion(awsModelID, awsRegionPrefix)
	}
	return awsCli, awsModelID, nil
}

// InvokeRawClaudeNonStream invokes Bedrock InvokeModel with a prepared Claude JSON payload
// and returns the raw Anthropic JSON response bytes for IR decoding.
func (a *Adaptor) InvokeRawClaudeNonStream(c *gin.Context, info *relaycommon.RelayInfo, finalBody []byte) ([]byte, *types.NewAPIError) {
	awsCli, awsModelID, apiErr := a.resolveClientAndModel(c, info)
	if apiErr != nil {
		return nil, apiErr
	}

	ctx, cancel := newAwsInvokeContextFrom(awsRequestParentContext(c))
	defer cancel()

	awsResp, err := awsCli.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(awsModelID),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        finalBody,
	})
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		if errors.Is(err, context.Canceled) {
			return nil, types.NewOpenAIError(errors.Wrap(err, "InvokeModel"), types.ErrorCodeAwsInvokeError, statusCode, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewOpenAIError(errors.Wrap(err, "InvokeModel"), types.ErrorCodeAwsInvokeError, statusCode)
	}
	if awsResp == nil {
		return nil, types.NewOpenAIError(errors.New("InvokeModel returned nil response"), types.ErrorCodeAwsInvokeError, http.StatusBadGateway)
	}
	return awsResp.Body, nil
}

// InvokeRawClaudeStream invokes Bedrock InvokeModelWithResponseStream and streams each decoded
// Anthropic JSON event chunk to onChunk for real-time IR translation.
func (a *Adaptor) InvokeRawClaudeStream(c *gin.Context, info *relaycommon.RelayInfo, finalBody []byte, onChunk func(chunk []byte) *types.NewAPIError) *types.NewAPIError {
	awsCli, awsModelID, apiErr := a.resolveClientAndModel(c, info)
	if apiErr != nil {
		return apiErr
	}

	ctx, cancel := newAwsInvokeContextFrom(awsRequestParentContext(c))
	defer cancel()

	awsResp, err := awsCli.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(awsModelID),
		Accept:      aws.String("application/json"),
		ContentType: aws.String("application/json"),
		Body:        finalBody,
	})
	if err != nil {
		statusCode := getAwsErrorStatusCode(err)
		if errors.Is(err, context.Canceled) {
			return types.NewOpenAIError(errors.Wrap(err, "InvokeModelWithResponseStream"), types.ErrorCodeAwsInvokeError, statusCode, types.ErrOptionWithSkipRetry())
		}
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModelWithResponseStream"), types.ErrorCodeAwsInvokeError, statusCode)
	}
	if awsResp == nil || awsResp.GetStream() == nil {
		return types.NewOpenAIError(errors.New("InvokeModelWithResponseStream returned nil stream"), types.ErrorCodeAwsInvokeError, http.StatusBadGateway)
	}
	stream := awsResp.GetStream()
	defer stream.Close()

	deliveredAny := false
	for event := range stream.Events() {
		switch v := event.(type) {
		case *bedrockruntimeTypes.ResponseStreamMemberChunk:
			info.SetFirstResponseTime()
			deliveredAny = true
			if err := onChunk(v.Value.Bytes); err != nil {
				return err
			}
		case *bedrockruntimeTypes.UnknownUnionMember:
			if deliveredAny {
				return types.NewError(fmt.Errorf("unknown bedrock stream event tag: %s", v.Tag), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
			}
			return types.NewError(fmt.Errorf("unknown bedrock stream event tag: %s", v.Tag), types.ErrorCodeInvalidRequest)
		default:
			if deliveredAny {
				return types.NewError(errors.New("nil or unknown bedrock stream event type"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
			}
			return types.NewError(errors.New("nil or unknown bedrock stream event type"), types.ErrorCodeInvalidRequest)
		}
	}
	if err := stream.Err(); err != nil {
		statusCode := getAwsErrorStatusCode(err)
		if deliveredAny || errors.Is(err, context.Canceled) {
			return types.NewOpenAIError(errors.Wrap(err, "InvokeModelWithResponseStream"), types.ErrorCodeAwsInvokeError, statusCode, types.ErrOptionWithSkipRetry())
		}
		return types.NewOpenAIError(errors.Wrap(err, "InvokeModelWithResponseStream"), types.ErrorCodeAwsInvokeError, statusCode)
	}
	return nil
}

func inlineBedrockURLImages(c *gin.Context, payload map[string]any) error {
	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil
	}
	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}
		blocks, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, blockRaw := range blocks {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}
			if err := inlineSourceBlock(c, block); err != nil {
				return err
			}
			// Also handle images embedded inside tool_result content arrays.
			if nested, ok := block["content"].([]any); ok {
				for _, subRaw := range nested {
					if sub, ok := subRaw.(map[string]any); ok {
						if err := inlineSourceBlock(c, sub); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func inlineSourceBlock(c *gin.Context, block map[string]any) error {
	src, ok := block["source"].(map[string]any)
	if !ok {
		return nil
	}
	if srcType, _ := src["type"].(string); srcType != "url" {
		return nil
	}
	rawURL, _ := src["url"].(string)
	if rawURL == "" {
		return nil
	}
	fileSource := types.NewURLFileSource(rawURL)
	base64Data, mimeType, err := service.GetBase64Data(c, fileSource, "formatting image for Bedrock Claude")
	if err != nil {
		return fmt.Errorf("get file base64 from url failed: %s", err.Error())
	}
	blockType, _ := block["type"].(string)
	if blockType == "document" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "text/") {
		if rawText, decErr := base64.StdEncoding.DecodeString(base64Data); decErr == nil {
			src["type"] = "text"
			src["media_type"] = mimeType
			src["data"] = string(rawText)
			delete(src, "url")
			return nil
		}
	}
	src["type"] = "base64"
	src["media_type"] = mimeType
	src["data"] = base64Data
	delete(src, "url")
	return nil
}
