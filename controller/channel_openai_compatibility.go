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
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func useOpenAICompletionTokens(model string, channelType int) bool {
	_, _, capabilities, known := openaimodel.Resolve(model)
	return known && capabilities.CompletionTokens && (channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeAzure || capabilities.LegacyO)
}

// Azure uses deployment names in the URL. Apply token compatibility using the
// mapped deployment after overrides; preserve the endpoint and unrelated fields.
func prepareAzureChannelTestRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if info.RelayMode != relayconstant.RelayModeChatCompletions || !useOpenAICompletionTokens(info.UpstreamModelName, constant.ChannelTypeAzure) {
		return body, nil
	}
	if tokens := gjson.GetBytes(body, "max_tokens"); tokens.Exists() && !gjson.GetBytes(body, "max_completion_tokens").Exists() {
		var err error
		body, err = sjson.SetRawBytes(body, "max_completion_tokens", []byte(tokens.Raw))
		if err != nil {
			return nil, err
		}
	}
	return sjson.DeleteBytes(body, "max_tokens")
}

func prepareOpenAIChannelTestRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if info.RelayMode != relayconstant.RelayModeChatCompletions && info.RelayMode != relayconstant.RelayModeResponses {
		return body, nil
	}
	responses := info.RelayMode == relayconstant.RelayModeResponses
	body, model, requiresResponses, err := relaycommon.NormalizeOpenAIRequest(body, responses)
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
	body, _, _, err = relaycommon.NormalizeOpenAIRequest(body, true)
	if err != nil {
		return nil, err
	}
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"
	info.InitResponsesUsageInfo(request)
	return body, nil
}
