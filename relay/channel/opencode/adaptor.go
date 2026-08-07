package opencode

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	openAI openai.Adaptor
	claude claude.Adaptor
	gemini gemini.Adaptor
}

type GoAdaptor struct {
	Adaptor
}

func endpoint(info *relaycommon.RelayInfo) constant.OpenCodeEndpoint {
	if info == nil {
		return constant.OpenCodeEndpointChat
	}
	if info.ChannelType == constant.ChannelTypeOpenCodeGo {
		return constant.GetOpenCodeGoEndpoint(info.UpstreamModelName)
	}
	return constant.GetOpenCodeEndpoint(info.UpstreamModelName)
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	switch endpoint(info) {
	case constant.OpenCodeEndpointMessages:
		a.claude.Init(info)
	case constant.OpenCodeEndpointGemini:
		a.gemini.Init(info)
	default:
		a.openAI.Init(info)
	}
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info == nil {
		return "", errors.New("opencode zen: relay info is nil")
	}
	baseURL := strings.TrimRight(info.ChannelBaseUrl, "/")
	switch endpoint(info) {
	case constant.OpenCodeEndpointResponses:
		if info.RelayMode == relayconstant.RelayModeResponsesCompact {
			return "", errors.New("opencode zen does not support responses compaction")
		}
		return baseURL + "/v1/responses", nil
	case constant.OpenCodeEndpointMessages:
		return baseURL + "/v1/messages", nil
	case constant.OpenCodeEndpointGemini:
		action := "generateContent"
		if info.IsStream {
			action = "streamGenerateContent?alt=sse"
			if info.RelayMode == relayconstant.RelayModeGemini {
				info.DisablePing = true
			}
		}
		return fmt.Sprintf("%s/v1/models/%s:%s", baseURL, url.PathEscape(info.UpstreamModelName), action), nil
	default:
		return baseURL + "/v1/chat/completions", nil
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	switch endpoint(info) {
	case constant.OpenCodeEndpointMessages:
		return a.claude.SetupRequestHeader(c, header, info)
	case constant.OpenCodeEndpointGemini:
		return a.gemini.SetupRequestHeader(c, header, info)
	default:
		return a.openAI.SetupRequestHeader(c, header, info)
	}
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	switch endpoint(info) {
	case constant.OpenCodeEndpointResponses:
		return nil, errors.New("opencode zen responses models require chat-completions to responses conversion")
	case constant.OpenCodeEndpointMessages:
		return a.claude.ConvertOpenAIRequest(c, info, request)
	case constant.OpenCodeEndpointGemini:
		return a.gemini.ConvertOpenAIRequest(c, info, request)
	default:
		return a.openAI.ConvertOpenAIRequest(c, info, request)
	}
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	switch endpoint(info) {
	case constant.OpenCodeEndpointResponses:
		return nil, errors.New("opencode zen responses models require messages to responses conversion")
	case constant.OpenCodeEndpointMessages:
		return a.claude.ConvertClaudeRequest(c, info, request)
	case constant.OpenCodeEndpointGemini:
		return a.gemini.ConvertClaudeRequest(c, info, request)
	default:
		return a.openAI.ConvertClaudeRequest(c, info, request)
	}
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	switch endpoint(info) {
	case constant.OpenCodeEndpointResponses:
		return nil, errors.New("opencode zen responses models do not support Gemini request conversion")
	case constant.OpenCodeEndpointMessages:
		converted, err := a.openAI.ConvertGeminiRequest(c, info, request)
		if err != nil {
			return nil, err
		}
		openAIRequest, ok := converted.(*dto.GeneralOpenAIRequest)
		if !ok {
			return nil, fmt.Errorf("opencode zen: expected OpenAI request, got %T", converted)
		}
		return a.claude.ConvertOpenAIRequest(c, info, openAIRequest)
	case constant.OpenCodeEndpointGemini:
		return a.gemini.ConvertGeminiRequest(c, info, request)
	default:
		return a.openAI.ConvertGeminiRequest(c, info, request)
	}
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if endpoint(info) != constant.OpenCodeEndpointResponses {
		return nil, fmt.Errorf("opencode zen model %q does not use the responses endpoint", info.UpstreamModelName)
	}
	return a.openAI.ConvertOpenAIResponsesRequest(c, info, request)
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("opencode zen does not support rerank requests")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("opencode zen does not support embedding requests")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("opencode zen does not support audio requests")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("opencode zen does not support image requests")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch endpoint(info) {
	case constant.OpenCodeEndpointMessages:
		return a.claude.DoResponse(c, resp, info)
	case constant.OpenCodeEndpointGemini:
		return a.gemini.DoResponse(c, resp, info)
	default:
		return a.openAI.DoResponse(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *GoAdaptor) GetModelList() []string {
	return GoModelList
}

func (a *GoAdaptor) GetChannelName() string {
	return GoChannelName
}
