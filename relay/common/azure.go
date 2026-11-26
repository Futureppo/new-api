package common

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/openaimodel"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// AzureModelName resolves metadata before names: a deployment can deliberately
// have the name of a different model. Never infer capabilities from ID substrings.
func AzureModelName(info *RelayInfo, deployment string) string {
	if name := info.ChannelOtherSettings.AzureDeploymentModels[deployment]; name != "" {
		return name
	}
	if _, _, _, known := openaimodel.ResolveAzure(deployment); known || strings.HasPrefix(deployment, "claude-") {
		return deployment
	}
	// Older channels may only have an original-model -> deployment mapping.
	// Don't reuse that identity if a parameter override selected another deployment.
	if deployment == info.ModelMappingTargetName && info.OriginModelName != "" {
		return info.OriginModelName
	}
	return deployment
}

func IsAzureClaude(info *RelayInfo) bool {
	return info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeAzure &&
		strings.HasPrefix(AzureModelName(info, info.UpstreamModelName), "claude-")
}

func NormalizeAzureRequest(info *RelayInfo, body []byte, responses bool) ([]byte, string, bool, error) {
	var identity struct {
		Model string `json:"model"`
	}
	if err := common.Unmarshal(body, &identity); err != nil {
		return nil, "", false, err
	}
	if identity.Model == "" {
		identity.Model = info.UpstreamModelName
	}
	_, _, caps, known := openaimodel.ResolveAzure(AzureModelName(info, identity.Model))
	if !known {
		return body, identity.Model, false, nil
	}
	// A suffix on a deployment is literal. Only model mapping chooses a different ID.
	result, deployment, requiresResponses, err := normalizeOpenAIModelRequest(body, identity.Model, "", caps, responses)
	if err != nil {
		return nil, "", false, err
	}
	effortPath := "reasoning_effort"
	if responses {
		effortPath = "reasoning.effort"
	}
	effort := gjson.GetBytes(result, effortPath).String()
	if !responses && strings.HasPrefix(AzureModelName(info, identity.Model), "gpt-5.6-") && effort != "" && effort != "none" {
		var chat dto.GeneralOpenAIRequest
		if err := common.Unmarshal(result, &chat); err != nil {
			return nil, "", false, err
		}
		if hasOpenAIChatTools(&chat) {
			requiresResponses = true
		}
	}
	if caps.RemoveSampling(effort) {
		for _, name := range []string{"presence_penalty", "frequency_penalty", "logit_bias"} {
			result, err = sjson.DeleteBytes(result, name)
			if err != nil {
				return nil, "", false, err
			}
		}
	}
	return result, deployment, requiresResponses || (!responses && effort == "max"), nil
}
