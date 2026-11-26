package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/openaimodel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/openaicompat"
)

func useOpenAICompletionTokens(model string, channelType int) bool {
	_, _, capabilities, known := openaimodel.Resolve(model)
	if channelType == constant.ChannelTypeAzure {
		_, _, capabilities, known = openaimodel.ResolveAzure(model)
	}
	return known && capabilities.CompletionTokens && (channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeAzure || capabilities.LegacyO)
}

// Tests use the same final-body compatibility rules as production relay.
func prepareAzureChannelTestRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if relaycommon.IsAzureClaude(info) {
		return body, nil
	}
	return prepareOpenAIChannelTestRequest(info, body)
}

func prepareOpenAIChannelTestRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if info.RelayMode != relayconstant.RelayModeChatCompletions && info.RelayMode != relayconstant.RelayModeResponses {
		return body, nil
	}
	responses := info.RelayMode == relayconstant.RelayModeResponses
	normalize := relaycommon.NormalizeOpenAIRequest
	if info.ChannelType == constant.ChannelTypeAzure {
		normalize = func(body []byte, responses bool) ([]byte, string, bool, error) {
			return relaycommon.NormalizeAzureRequest(info, body, responses)
		}
	}
	body, model, requiresResponses, err := normalize(body, responses)
	if err != nil {
		return nil, err
	}
	info.UpstreamModelName = model
	if responses || (!requiresResponses && !service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, model)) {
		return body, nil
	}
	var chat dto.GeneralOpenAIRequest
	if err := common.Unmarshal(body, &chat); err != nil {
		return nil, err
	}
	if err := openaicompat.ValidateChatRequestForResponses(&chat); err != nil {
		return nil, err
	}
	request, err := service.ChatCompletionsRequestToResponsesRequest(&chat)
	if err != nil {
		return nil, err
	}
	body, err = common.Marshal(request)
	if err != nil {
		return nil, err
	}
	body, _, _, err = normalize(body, true)
	if err != nil {
		return nil, err
	}
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"
	info.InitResponsesUsageInfo(request)
	return body, nil
}
