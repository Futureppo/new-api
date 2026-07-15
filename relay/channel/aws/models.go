package aws

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrockTypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
)

type bedrockModelLister interface {
	ListFoundationModels(context.Context, *bedrock.ListFoundationModelsInput, ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error)
	ListInferenceProfiles(context.Context, *bedrock.ListInferenceProfilesInput, ...func(*bedrock.Options)) (*bedrock.ListInferenceProfilesOutput, error)
}

// FetchClaudeModels returns active Anthropic text models that can be invoked in
// the credential's region. System-defined inference profiles are preferred over
// their underlying foundation model IDs because newer Claude models often
// require cross-region inference.
func FetchClaudeModels(ctx context.Context, rawKey string, keyType dto.AwsKeyType, proxy string) ([]string, error) {
	credential, err := parseAwsCredential(rawKey, keyType)
	if err != nil {
		return nil, err
	}
	httpClient, err := newAwsHTTPClient(proxy)
	if err != nil {
		return nil, err
	}
	return fetchClaudeModels(ctx, newAwsBedrockClient(credential, httpClient))
}

func fetchClaudeModels(ctx context.Context, client bedrockModelLister) ([]string, error) {
	profiles, coveredModels, err := fetchClaudeInferenceProfiles(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("fetch AWS Claude inference profiles failed: %w", err)
	}

	foundationOutput, err := client.ListFoundationModels(ctx, &bedrock.ListFoundationModelsInput{
		ByProvider:       aws.String("Anthropic"),
		ByOutputModality: bedrockTypes.ModelModalityText,
		ByInferenceType:  bedrockTypes.InferenceTypeOnDemand,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch AWS Claude foundation models failed: %w", err)
	}

	modelSet := make(map[string]struct{}, len(profiles)+len(foundationOutput.ModelSummaries))
	for _, profileID := range profiles {
		modelSet[profileID] = struct{}{}
	}
	for _, summary := range foundationOutput.ModelSummaries {
		modelID := strings.TrimSpace(aws.ToString(summary.ModelId))
		if !isActiveClaudeFoundationModel(summary, modelID) {
			continue
		}
		if _, covered := coveredModels[modelID]; covered {
			continue
		}
		modelSet[modelID] = struct{}{}
	}

	models := make([]string, 0, len(modelSet))
	for modelID := range modelSet {
		models = append(models, modelID)
	}
	sort.Strings(models)
	return models, nil
}

func fetchClaudeInferenceProfiles(ctx context.Context, client bedrockModelLister) ([]string, map[string]struct{}, error) {
	profileSet := make(map[string]struct{})
	coveredModels := make(map[string]struct{})
	var nextToken *string

	for {
		output, err := client.ListInferenceProfiles(ctx, &bedrock.ListInferenceProfilesInput{
			MaxResults: aws.Int32(1000),
			NextToken:  nextToken,
			TypeEquals: bedrockTypes.InferenceProfileTypeSystemDefined,
		})
		if err != nil {
			return nil, nil, err
		}
		for _, summary := range output.InferenceProfileSummaries {
			profileID := strings.TrimSpace(aws.ToString(summary.InferenceProfileId))
			if summary.Status != bedrockTypes.InferenceProfileStatusActive || !isClaudeModelID(profileID) {
				continue
			}
			profileSet[profileID] = struct{}{}
			for _, model := range summary.Models {
				if modelID := foundationModelIDFromARN(aws.ToString(model.ModelArn)); modelID != "" {
					coveredModels[modelID] = struct{}{}
				}
			}
		}
		if output.NextToken == nil || strings.TrimSpace(aws.ToString(output.NextToken)) == "" {
			break
		}
		nextToken = output.NextToken
	}

	profiles := make([]string, 0, len(profileSet))
	for profileID := range profileSet {
		profiles = append(profiles, profileID)
	}
	sort.Strings(profiles)
	return profiles, coveredModels, nil
}

func isActiveClaudeFoundationModel(summary bedrockTypes.FoundationModelSummary, modelID string) bool {
	if !isClaudeModelID(modelID) || !strings.EqualFold(aws.ToString(summary.ProviderName), "Anthropic") {
		return false
	}
	if summary.ModelLifecycle == nil || summary.ModelLifecycle.Status != bedrockTypes.FoundationModelLifecycleStatusActive {
		return false
	}
	return containsModelModality(summary.OutputModalities, bedrockTypes.ModelModalityText) &&
		containsInferenceType(summary.InferenceTypesSupported, bedrockTypes.InferenceTypeOnDemand)
}

func containsModelModality(values []bedrockTypes.ModelModality, expected bedrockTypes.ModelModality) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsInferenceType(values []bedrockTypes.InferenceType, expected bedrockTypes.InferenceType) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func isClaudeModelID(modelID string) bool {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return strings.HasPrefix(modelID, "anthropic.claude") || strings.Contains(modelID, ".anthropic.claude")
}

func foundationModelIDFromARN(modelARN string) string {
	const marker = "foundation-model/"
	index := strings.LastIndex(modelARN, marker)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(modelARN[index+len(marker):])
}
