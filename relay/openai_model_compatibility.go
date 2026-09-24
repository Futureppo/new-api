package relay

import (
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

func invalidOpenAIModelRequest(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func normalizeOfficialChatRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, bool, error) {
	body, model, responses, err := relaycommon.NormalizeOpenAIRequest(body, false)
	if err == nil {
		info.UpstreamModelName = model
	}
	return body, responses, err
}

func normalizeOfficialResponsesRequest(info *relaycommon.RelayInfo, body []byte) ([]byte, error) {
	body, model, _, err := relaycommon.NormalizeOpenAIRequest(body, true)
	if err == nil {
		info.UpstreamModelName = model
	}
	return body, err
}
