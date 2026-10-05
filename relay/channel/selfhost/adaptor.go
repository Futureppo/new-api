package selfhost

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type Adaptor struct{ openai.Adaptor }
type VLLMAdaptor struct{ Adaptor }
type LiteLLMAdaptor struct{ Adaptor }

func (a *VLLMAdaptor) GetChannelName() string    { return "vllm" }
func (a *LiteLLMAdaptor) GetChannelName() string { return "litellm" }
func (a *Adaptor) GetModelList() []string        { return []string{} }

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	var path string
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions:
		path = "/v1/chat/completions"
	case relayconstant.RelayModeResponses:
		path = "/v1/responses"
	default:
		return "", invalidRequest(fmt.Errorf("vLLM/LiteLLM supports Chat Completions and Responses"))
	}
	if err := ValidateBaseURL(info.ChannelBaseUrl); err != nil {
		return "", invalidRequest(err)
	}
	return NormalizeBaseURL(info.ChannelBaseUrl) + path, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	header.Del("Authorization")
	if key := strings.TrimSpace(info.ApiKey); key != "" {
		header.Set("Authorization", "Bearer "+key)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, fmt.Errorf("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if _, err := a.GetRequestURL(info); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	normalized, model, effort, err := NormalizeRequest(raw, info.ChannelType, info.RelayMode == relayconstant.RelayModeResponses)
	if err != nil {
		return nil, invalidRequest(err)
	}
	info.UpstreamModelName = model
	info.ReasoningEffort = effort
	relaycommon.SetConversationUpstreamRequest(info, normalized)
	return channel.DoApiRequest(a, c, info, bytes.NewReader(normalized))
}

func invalidRequest(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

var _ channel.Adaptor = (*VLLMAdaptor)(nil)
var _ channel.Adaptor = (*LiteLLMAdaptor)(nil)
