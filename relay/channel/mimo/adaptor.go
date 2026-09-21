package mimo

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const DefaultTestModel = "mimo-v2.6-pro-ultraspeed"

var ModelList = []string{"mimo-v2.5", "mimo-v2.5-pro", DefaultTestModel}

type Adaptor struct{}

// NormalizeBaseURL accepts either SDK base URL while preserving proxy prefixes.
func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return constant.ChannelBaseURLs[constant.ChannelTypeMiMo]
	}
	for _, suffix := range []string{"/anthropic/v1", "/anthropic", "/v1"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix)
		}
	}
	return base
}

// SupplementModels retains the unlisted model verified on 2026-09-21 in default
// discovery and synchronization results. Availability still depends on upstream.
func SupplementModels(models []string) []string {
	result := make([]string, 0, len(models)+1)
	seen := make(map[string]bool)
	for _, model := range append(append([]string(nil), models...), DefaultTestModel) {
		model = strings.TrimSpace(model)
		lower := strings.ToLower(model)
		if model == "" || seen[model] || strings.Contains(lower, "-asr") || strings.Contains(lower, "-tts") {
			continue
		}
		seen[model] = true
		result = append(result, model)
	}
	return result
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	adaptor := openai.Adaptor{}
	adaptor.Init(info)
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	var path string
	switch {
	case info.RelayFormat == types.RelayFormatClaude:
		path = "/anthropic/v1/messages"
	case info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions:
		path = "/v1/chat/completions"
	default:
		return "", unsupportedRequest()
	}
	base := NormalizeBaseURL(info.ChannelBaseUrl)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid MiMo base URL")
	}
	return base + path, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	header.Set("api-key", info.ApiKey)
	if info.RelayFormat == types.RelayFormatClaude {
		version := c.GetHeader("anthropic-version")
		if version == "" {
			version = "2023-06-01"
		}
		header.Set("anthropic-version", version)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, body)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if info.RelayFormat == types.RelayFormatClaude {
		adaptor := claude.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	}
	adaptor := openai.Adaptor{}
	return adaptor.DoResponse(c, resp, info)
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return "mimo" }

func unsupportedRequest() error {
	return types.NewErrorWithStatusCode(errors.New("MiMo only supports chat completions and Anthropic messages"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
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

var _ channel.Adaptor = (*Adaptor)(nil)
