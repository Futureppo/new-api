package relay

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func MistralNativeHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	info.DisablePing = true
	if info.ChannelType != constant.ChannelTypeMistral || !mistral.NativeMethodAllowed(c.Request.Method, c.Request.URL.Path) {
		return types.NewOpenAIError(errors.New("this native endpoint requires a Mistral AI channel"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	original, ok := info.Request.(*dto.MistralNativeRequest)
	if !ok {
		return types.NewError(errors.New("invalid Mistral native request"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	request := *original
	if err := helper.ModelMappedHelper(c, info, &request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	if mistral.RequiresCallPrice(c.Request.URL.Path) && !info.PriceData.UsePrice && !info.PriceData.FreeModel {
		return types.NewOpenAIError(errors.New("Mistral OCR and speech passthrough may omit token usage; configure per-request pricing for this model"), types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	a := &mistral.Adaptor{}
	a.Init(info)
	body, err := a.ConvertNativeRequest(c, info, &request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	var result *mistral.NativeResult
	var apiErr *types.NewAPIError
	if c.Request.URL.Path == mistral.RealtimePath {
		_, err = doChannelRPMGuardedRequest(c, info, func() (any, error) {
			result, apiErr = a.NativeRealtime(c, info)
			if apiErr != nil {
				return &http.Response{StatusCode: apiErr.StatusCode}, nil
			}
			return nil, nil
		})
	} else {
		var response any
		response, err = doChannelRPMGuardedRequest(c, info, func() (any, error) { return a.DoRequest(c, info, body) })
		if err == nil {
			result, apiErr = a.NativeResponse(c, response.(*http.Response), info)
		}
	}
	if err != nil {
		return types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if apiErr != nil && (result == nil || result.Usage == nil || result.Usage.TotalTokens == 0 || !info.IsStream) {
		return apiErr
	}
	usage := result.Usage
	extra := []string{"Mistral native passthrough"}
	if result.Pages != nil {
		extra = append(extra, fmt.Sprintf("pages_processed=%d", *result.Pages))
	}
	if usage == nil {
		usage = &dto.Usage{}
		extra = append(extra, "上游未返回 token usage")
	}
	service.PostTextConsumeQuota(c, info, usage, extra)
	return apiErr
}
