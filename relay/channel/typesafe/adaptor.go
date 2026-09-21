package typesafe

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const SystemOnePath = "/v1/systemone"

var ModelList = []string{"jev-latest", "jev-preview", "jev-1.13.0"}

type Adaptor struct{}

func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = constant.ChannelBaseURLs[constant.ChannelTypeTypeSafe]
	}
	return strings.TrimSuffix(base, "/v1")
}

func (a *Adaptor) Init(*relaycommon.RelayInfo) {}
func (a *Adaptor) GetChannelName() string      { return "TypeSafe" }
func (a *Adaptor) GetModelList() []string      { return ModelList }

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeTypeSafe || info.RelayMode != relayconstant.RelayModeTypeSafeSystemOne {
		return "", unsupportedRequest()
	}
	return NormalizeBaseURL(info.ChannelBaseUrl) + SystemOnePath, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	key := strings.TrimSpace(info.ApiKey)
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return errors.New("invalid TypeSafe channel key")
	}
	channel.SetupApiRequestHeader(info, c, header)
	header.Set("Authorization", "Bearer "+key)
	header.Set("Content-Type", gin.MIMEJSON)
	header.Set("Accept", gin.MIMEJSON)
	return nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if c.Request.Method != http.MethodPost || info.IsStream {
		return nil, unsupportedRequest()
	}
	return channel.DoApiRequest(a, c, info, body)
}

func unsupportedRequest() error {
	return types.NewErrorWithStatusCode(errors.New("TypeSafe only supports synchronous POST /v1/systemone"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) ConvertOpenAIRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeneralOpenAIRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertImageRequest(*gin.Context, *relaycommon.RelayInfo, dto.ImageRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, unsupportedRequest()
}

var _ channel.Adaptor = (*Adaptor)(nil)
