package vyceai

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct{}

type imageRequest struct {
	Model       string `json:"model"`
	Prompt      string `json:"prompt"`
	AspectRatio string `json:"aspectRatio"`
	EnableNSFW  bool   `json:"enableNsfw"`
	Size        string `json:"size"`
}

func (a *Adaptor) Init(*relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info == nil {
		return "", errors.New("relay info is nil")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	if baseURL == "" {
		baseURL = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeVyceAI]
	}
	return baseURL + ImageStreamPath, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	if info == nil {
		return errors.New("relay info is nil")
	}
	apiKey := strings.TrimSpace(info.ApiKey)
	if apiKey == "" {
		return invalidRequestError("channel key is empty")
	}
	if strings.ContainsAny(apiKey, "\r\n") {
		return invalidRequestError("channel key must not contain CR or LF characters")
	}
	channel.SetupApiRequestHeader(info, c, header)
	header.Set("Authorization", "Bearer "+apiKey)
	header.Set("Content-Type", gin.MIMEJSON)
	header.Set("Accept", "text/event-stream")
	return nil
}

func (a *Adaptor) ConvertImageRequest(_ *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if info == nil {
		return nil, invalidRequestError("relay info is nil")
	}
	if info.RelayMode != relayconstant.RelayModeImagesGenerations {
		return nil, invalidRequestError("VyceAI only supports image generations")
	}

	model := strings.TrimSpace(request.Model)
	if strings.TrimSpace(info.UpstreamModelName) != "" {
		model = strings.TrimSpace(info.UpstreamModelName)
	}
	aspectRatio, ok := aspectRatioForModel(model)
	if !ok {
		return nil, invalidRequestError(fmt.Sprintf("unsupported VyceAI model %q", model))
	}

	// VyceAI always returns exactly one image, regardless of the OpenAI n field.
	info.PriceData.AddOtherRatio("n", 1)
	return imageRequest{
		Model:       UpstreamModel,
		Prompt:      request.Prompt,
		AspectRatio: aspectRatio,
		EnableNSFW:  true,
		Size:        UpstreamSize,
	}, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info == nil {
		return nil, badResponseError(errors.New("relay info is nil"))
	}
	// The upstream response is SSE, but the OpenAI image response is synchronous.
	info.IsStream = false
	return handleImageResponse(c, resp, info)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func (a *Adaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, invalidRequestError("VyceAI does not support chat completions")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, invalidRequestError("VyceAI does not support Claude messages")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, invalidRequestError("VyceAI does not support Gemini requests")
}

func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, invalidRequestError("VyceAI does not support reranking")
}

func (a *Adaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, invalidRequestError("VyceAI does not support embeddings")
}

func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, invalidRequestError("VyceAI does not support audio")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, invalidRequestError("VyceAI does not support the Responses API")
}

func invalidRequestError(message string) error {
	return types.NewErrorWithStatusCode(
		errors.New(message),
		types.ErrorCodeInvalidRequest,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}
