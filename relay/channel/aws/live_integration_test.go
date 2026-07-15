//go:build integration

package aws

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockRuntimeTypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/stretchr/testify/require"
)

func TestLiveAwsClaudeModels(t *testing.T) {
	if os.Getenv("AWS_BEDROCK_LIVE_TEST") != "1" {
		t.Skip("set AWS_BEDROCK_LIVE_TEST=1 to run billable Bedrock checks")
	}

	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region := os.Getenv("AWS_DEFAULT_REGION")
	require.NotEmpty(t, accessKeyID)
	require.NotEmpty(t, secretAccessKey)
	require.NotEmpty(t, region)

	rawKey := fmt.Sprintf("%s|%s|%s", accessKeyID, secretAccessKey, region)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	models, err := FetchClaudeModels(ctx, rawKey, dto.AwsKeyTypeAKSK, "")
	require.NoError(t, err)
	require.NotEmpty(t, models)
	t.Logf("discovered %d active Claude model identifiers", len(models))

	credential, err := parseAwsCredential(rawKey, dto.AwsKeyTypeAKSK)
	require.NoError(t, err)
	httpClient, err := newAwsHTTPClient("")
	require.NoError(t, err)
	client := newAwsRuntimeClient(credential, httpClient)

	payload, err := common.Marshal(map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        8,
		"messages": []map[string]any{{
			"role":    "user",
			"content": "Reply OK",
		}},
	})
	require.NoError(t, err)

	successes := 0
	for _, modelID := range models {
		nonStreamOutput, invokeErr := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
			ModelId:     aws.String(modelID),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
			Body:        payload,
		})
		if invokeErr != nil {
			t.Logf("LIVE_RESULT model=%s non_stream=failed error=%v", modelID, invokeErr)
			continue
		}
		if len(nonStreamOutput.Body) == 0 {
			t.Logf("LIVE_RESULT model=%s non_stream=failed error=empty_response", modelID)
			continue
		}

		streamOutput, streamErr := client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
			ModelId:     aws.String(modelID),
			Accept:      aws.String("application/json"),
			ContentType: aws.String("application/json"),
			Body:        payload,
		})
		if streamErr != nil {
			t.Logf("LIVE_RESULT model=%s non_stream=ok stream=failed error=%v", modelID, streamErr)
			continue
		}

		stream := streamOutput.GetStream()
		chunks := 0
		for event := range stream.Events() {
			if chunk, ok := event.(*bedrockRuntimeTypes.ResponseStreamMemberChunk); ok && len(chunk.Value.Bytes) > 0 {
				chunks++
			}
		}
		streamErr = stream.Err()
		_ = stream.Close()
		if streamErr != nil || chunks == 0 {
			t.Logf("LIVE_RESULT model=%s non_stream=ok stream=failed chunks=%d error=%v", modelID, chunks, streamErr)
			continue
		}

		successes++
		t.Logf("LIVE_RESULT model=%s non_stream=ok stream=ok chunks=%d", modelID, chunks)
	}

	require.Positive(t, successes, "no discovered Claude model passed both live checks")
}
