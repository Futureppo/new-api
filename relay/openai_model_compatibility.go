package relay

import (
	"github.com/QuantumNous/new-api/constant"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

func invalidOpenAIModelRequest(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func normalizeOfficialChatRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, bool, error) {
	if info.ChannelType == constant.ChannelTypeAzure {
		body, model, responses, err := relaycommon.NormalizeAzureRequest(info, body, false)
		if err == nil {
			info.UpstreamModelName = model
		}
		return body, responses, err
	}
	body, model, responses, err := relaycommon.NormalizeOpenAIRequest(body, false)
	if err == nil {
		info.UpstreamModelName = model
	}
	return body, responses, err
}

func normalizeOfficialResponsesRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	if info.ChannelType == constant.ChannelTypeAzure {
		body, model, _, err := relaycommon.NormalizeAzureRequest(info, body, true)
		if err == nil {
			info.UpstreamModelName = model
		}
		return body, err
	}
	body, model, _, err := relaycommon.NormalizeOpenAIRequest(body, true)
	if err == nil {
		info.UpstreamModelName = model
	}
	return body, err
}
