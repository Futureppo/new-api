package relay

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func VertexNativeHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	if info.ChannelType != constant.ChannelTypeVertexAi {
		return types.NewErrorWithStatusCode(errors.New("this native request requires a Vertex channel"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	original, ok := info.Request.(*dto.VertexRequest)
	if !ok {
		return types.NewError(errors.New("invalid Vertex request"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	request := *original
	if err := helper.ModelMappedHelper(c, info, &request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	a := &vertex.Adaptor{}
	a.Init(info)
	body, err := a.ConvertNativeRequest(c, info, &request)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if a.RequiresCallPrice() && !info.PriceData.UsePrice {
		return types.NewErrorWithStatusCode(errors.New("this Vertex prediction does not report token usage; configure per-request pricing"), types.ErrorCodeModelPriceError, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.SetConversationUpstreamRequest(info, data)
	resp, err := doChannelRPMGuardedRequest(c, info, func() (any, error) { return a.DoRequest(c, info, bytes.NewReader(data)) })
	if err != nil {
		return types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}
	httpResp := resp.(*http.Response)
	if httpResp.StatusCode != http.StatusOK {
		apiErr := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}
	usage, apiErr := a.DoResponse(c, httpResp, info)
	if apiErr != nil {
		return apiErr
	}
	if !vertexmodel.IsTokenTool(request.Action) {
		service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
	}
	return nil
}
