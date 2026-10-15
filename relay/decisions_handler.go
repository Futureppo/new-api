package relay

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func DecisionsHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	info.DisablePing = true
	if info.ChannelType != constant.ChannelTypeOpenAI && info.ChannelType != constant.ChannelTypeOpenRouter {
		return types.NewErrorWithStatusCode(errors.New("/v1/decisions requires an OpenAI or OpenRouter channel"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	var request dto.Request
	switch original := info.Request.(type) {
	case *dto.OpenAIDecisionsRequest:
		copy := *original
		request = &copy
	case *dto.OpenRouterDecisionsRequest:
		copy := *original
		request = &copy
	default:
		return types.NewErrorWithStatusCode(errors.New("invalid Decisions request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	service.StartConversationCapture(c, info)
	if err := helper.ModelMappedHelper(c, info, request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	var body []byte
	var err error
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		info.MarkModelMappingBypassed()
		storage, storageErr := common.GetBodyStorage(c)
		if storageErr != nil {
			return types.NewError(storageErr, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		body, err = storage.Bytes()
		// The payload was validated on entry; pass-through preserves it verbatim.
	} else {
		body, err = common.Marshal(request)
		if err == nil {
			body, err = relaycommon.PrepareDecisionsRequest(info, body)
		}
	}
	if err != nil {
		if fixed, ok := relaycommon.AsParamOverrideReturnError(err); ok {
			return relaycommon.NewAPIErrorFromParamOverride(fixed)
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	relaycommon.SetConversationUpstreamRequest(info, body)
	a := &openai.Adaptor{}
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
	resp, ok := response.(*http.Response)
	if !ok || resp == nil {
		return types.NewErrorWithStatusCode(errors.New("empty Decisions response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(c.Request.Context(), resp, false)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}
	relaycommon.WrapConversationUpstreamResponse(info, resp)
	usage, apiErr := a.DoResponse(c, resp, info)
	if apiErr != nil {
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}
	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	return nil
}
