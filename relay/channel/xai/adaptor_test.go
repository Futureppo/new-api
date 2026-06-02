package xai

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIRequestRejectsVideoModelOnChatEndpoint(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-imagine-video"},
	}
	request := &dto.GeneralOpenAIRequest{Model: "grok-imagine-video"}

	converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)

	require.Nil(t, converted)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.True(t, errors.As(err, &apiErr))
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	require.True(t, types.IsSkipRetryError(apiErr))
	require.Contains(t, apiErr.Error(), "/v1/videos")
}
