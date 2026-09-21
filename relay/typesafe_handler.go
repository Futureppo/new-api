package relay

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/typesafe"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TypeSafeHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	info.DisablePing = true
	if info.ChannelType != constant.ChannelTypeTypeSafe {
		return types.NewErrorWithStatusCode(errors.New("/v1/systemone requires a TypeSafe channel"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	original, ok := info.Request.(*dto.TypeSafeRequest)
	if !ok {
		return types.NewError(errors.New("invalid TypeSafe request"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	// Mapping and overrides must start from the original payload on every attempt.
	request := *original
	if err := helper.ModelMappedHelper(c, info, &request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	body, err := common.Marshal(request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		body, err = relaycommon.ApplyParamOverrideWithRelayInfo(body, info)
		if err != nil {
			if fixed, ok := relaycommon.AsParamOverrideReturnError(err); ok {
				return relaycommon.NewAPIErrorFromParamOverride(fixed)
			}
			return types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
	}
	// Validate again so an override cannot enable an unsupported stream.
	var effective dto.TypeSafeRequest
	if err := common.Unmarshal(body, &effective); err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	a := &typesafe.Adaptor{}
	a.Init(info)
	response, err := doChannelRPMGuardedRequest(c, info, func() (any, error) {
		return a.DoRequest(c, info, bytes.NewReader(body))
	})
	if err != nil {
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	httpResponse, ok := response.(*http.Response)
	if !ok {
		return types.NewErrorWithStatusCode(errors.New("invalid TypeSafe response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	usage, apiErr := a.DoResponse(c, httpResponse, info)
	if apiErr != nil {
		return apiErr
	}
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), []string{"TypeSafe System One"})
	return nil
}
