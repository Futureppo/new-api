package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrockTypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/require"
)

type mockBedrockModelLister struct {
	profilePages      []*bedrock.ListInferenceProfilesOutput
	profileErr        error
	foundationOutput  *bedrock.ListFoundationModelsOutput
	foundationErr     error
	profileCalls      int
	foundationRequest *bedrock.ListFoundationModelsInput
}

func (m *mockBedrockModelLister) ListInferenceProfiles(_ context.Context, input *bedrock.ListInferenceProfilesInput, _ ...func(*bedrock.Options)) (*bedrock.ListInferenceProfilesOutput, error) {
	if m.profileErr != nil {
		return nil, m.profileErr
	}
	if input.TypeEquals != bedrockTypes.InferenceProfileTypeSystemDefined {
		return nil, errors.New("unexpected profile type")
	}
	if m.profileCalls >= len(m.profilePages) {
		return &bedrock.ListInferenceProfilesOutput{}, nil
	}
	page := m.profilePages[m.profileCalls]
	m.profileCalls++
	return page, nil
}

func (m *mockBedrockModelLister) ListFoundationModels(_ context.Context, input *bedrock.ListFoundationModelsInput, _ ...func(*bedrock.Options)) (*bedrock.ListFoundationModelsOutput, error) {
	m.foundationRequest = input
	if m.foundationErr != nil {
		return nil, m.foundationErr
	}
	return m.foundationOutput, nil
}

func TestFetchClaudeModelsPrefersActiveInferenceProfiles(t *testing.T) {
	t.Parallel()

	client := &mockBedrockModelLister{
		profilePages: []*bedrock.ListInferenceProfilesOutput{
			{
				InferenceProfileSummaries: []bedrockTypes.InferenceProfileSummary{
					{
						InferenceProfileId: aws.String("us.anthropic.claude-sonnet-4-6"),
						Status:             bedrockTypes.InferenceProfileStatusActive,
						Models: []bedrockTypes.InferenceProfileModel{{
							ModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-sonnet-4-6"),
						}},
					},
					{
						InferenceProfileId: aws.String("us.amazon.nova-pro-v1:0"),
						Status:             bedrockTypes.InferenceProfileStatusActive,
					},
				},
				NextToken: aws.String("page-2"),
			},
			{
				InferenceProfileSummaries: []bedrockTypes.InferenceProfileSummary{
					{
						InferenceProfileId: aws.String("global.anthropic.claude-haiku-4-5-20251001-v1:0"),
						Status:             bedrockTypes.InferenceProfileStatusActive,
						Models: []bedrockTypes.InferenceProfileModel{{
							ModelArn: aws.String("arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-haiku-4-5-20251001-v1:0"),
						}},
					},
				},
			},
		},
		foundationOutput: &bedrock.ListFoundationModelsOutput{
			ModelSummaries: []bedrockTypes.FoundationModelSummary{
				activeClaudeFoundationModel("anthropic.claude-sonnet-4-6"),
				activeClaudeFoundationModel("anthropic.claude-haiku-4-5-20251001-v1:0"),
				activeClaudeFoundationModel("anthropic.claude-3-haiku-20240307-v1:0"),
				legacyClaudeFoundationModel("anthropic.claude-2-v1:0"),
			},
		},
	}

	models, err := fetchClaudeModels(context.Background(), client)
	require.NoError(t, err)
	require.Equal(t, []string{
		"anthropic.claude-3-haiku-20240307-v1:0",
		"global.anthropic.claude-haiku-4-5-20251001-v1:0",
		"us.anthropic.claude-sonnet-4-6",
	}, models)
	require.Equal(t, 2, client.profileCalls)
	require.Equal(t, "Anthropic", aws.ToString(client.foundationRequest.ByProvider))
	require.Equal(t, bedrockTypes.ModelModalityText, client.foundationRequest.ByOutputModality)
	require.Empty(t, client.foundationRequest.ByInferenceType)
}

func TestFetchClaudeModelsFallsBackWhenListingProfilesIsDenied(t *testing.T) {
	t.Parallel()

	denied := &bedrockTypes.AccessDeniedException{Message: aws.String("missing bedrock:ListInferenceProfiles")}
	profileOnlyModel := activeClaudeFoundationModel("anthropic.claude-sonnet-4-6")
	profileOnlyModel.InferenceTypesSupported = []bedrockTypes.InferenceType{"INFERENCE_PROFILE"}
	provisionedOnlyModel := activeClaudeFoundationModel("anthropic.claude-provisioned-only")
	provisionedOnlyModel.InferenceTypesSupported = []bedrockTypes.InferenceType{bedrockTypes.InferenceTypeProvisioned}
	for name, profileErr := range map[string]error{
		"direct": denied,
		"SDK wrapped": &smithy.OperationError{
			ServiceID: "Bedrock", OperationName: "ListInferenceProfiles", Err: denied,
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := &mockBedrockModelLister{
				profileErr: profileErr,
				foundationOutput: &bedrock.ListFoundationModelsOutput{
					ModelSummaries: []bedrockTypes.FoundationModelSummary{
						profileOnlyModel,
						provisionedOnlyModel,
						activeClaudeFoundationModel("anthropic.claude-3-haiku-20240307-v1:0"),
						legacyClaudeFoundationModel("anthropic.claude-2-v1:0"),
					},
				},
			}
			models, err := fetchClaudeModels(context.Background(), client)
			require.NoError(t, err)
			require.Equal(t, []string{
				"anthropic.claude-3-haiku-20240307-v1:0",
				"anthropic.claude-sonnet-4-6",
			}, models)
			require.NotNil(t, client.foundationRequest)
			require.Equal(t, "Anthropic", aws.ToString(client.foundationRequest.ByProvider))
			require.Empty(t, client.foundationRequest.ByInferenceType)
		})
	}
}

func TestFetchClaudeModelsReturnsControlPlaneErrors(t *testing.T) {
	t.Parallel()

	_, err := fetchClaudeModels(context.Background(), &mockBedrockModelLister{profileErr: errors.New("access denied")})
	require.EqualError(t, err, "fetch AWS Claude inference profiles failed: access denied")

	_, err = fetchClaudeModels(context.Background(), &mockBedrockModelLister{
		profilePages:  []*bedrock.ListInferenceProfilesOutput{{}},
		foundationErr: errors.New("throttled"),
	})
	require.EqualError(t, err, "fetch AWS Claude foundation models failed: throttled")
}

func TestFetchClaudeModelsDoesNotSuppressOtherProfileErrors(t *testing.T) {
	t.Parallel()

	for name, profileErr := range map[string]error{
		"throttled":         &bedrockTypes.ThrottlingException{Message: aws.String("rate limited")},
		"expired token":     &smithy.GenericAPIError{Code: "ExpiredTokenException", Message: "expired"},
		"invalid signature": &smithy.GenericAPIError{Code: "InvalidSignatureException", Message: "invalid signature"},
		"timeout":           context.DeadlineExceeded,
	} {
		t.Run(name, func(t *testing.T) {
			client := &mockBedrockModelLister{profileErr: profileErr}
			_, err := fetchClaudeModels(context.Background(), client)
			require.ErrorIs(t, err, profileErr)
			require.Nil(t, client.foundationRequest)
		})
	}
}

func TestFetchClaudeModelsReturnsFoundationPermissionErrorAfterFallback(t *testing.T) {
	t.Parallel()

	foundationErr := &bedrockTypes.AccessDeniedException{Message: aws.String("missing bedrock:ListFoundationModels")}
	client := &mockBedrockModelLister{
		profileErr:    &bedrockTypes.AccessDeniedException{Message: aws.String("missing bedrock:ListInferenceProfiles")},
		foundationErr: foundationErr,
	}
	_, err := fetchClaudeModels(context.Background(), client)
	require.ErrorIs(t, err, foundationErr)
	require.Contains(t, err.Error(), "fetch AWS Claude foundation models failed")
}

func TestFoundationModelIDFromARN(t *testing.T) {
	t.Parallel()
	require.Equal(t, "anthropic.claude-sonnet-4-6", foundationModelIDFromARN("arn:aws:bedrock:us-east-1::foundation-model/anthropic.claude-sonnet-4-6"))
	require.Empty(t, foundationModelIDFromARN("arn:aws:bedrock:us-east-1:123:inference-profile/example"))
}

func activeClaudeFoundationModel(modelID string) bedrockTypes.FoundationModelSummary {
	return bedrockTypes.FoundationModelSummary{
		ModelId:                 aws.String(modelID),
		ProviderName:            aws.String("Anthropic"),
		ModelLifecycle:          &bedrockTypes.FoundationModelLifecycle{Status: bedrockTypes.FoundationModelLifecycleStatusActive},
		OutputModalities:        []bedrockTypes.ModelModality{bedrockTypes.ModelModalityText},
		InferenceTypesSupported: []bedrockTypes.InferenceType{bedrockTypes.InferenceTypeOnDemand},
	}
}

func legacyClaudeFoundationModel(modelID string) bedrockTypes.FoundationModelSummary {
	model := activeClaudeFoundationModel(modelID)
	model.ModelLifecycle.Status = bedrockTypes.FoundationModelLifecycleStatusLegacy
	return model
}
