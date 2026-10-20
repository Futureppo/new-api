package bilibili

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	DefaultModel = "Index-Translate-35B-A3B"
	UserAgent    = "Index-Translate-Client/1.0"
	TestPrompt   = "Translate to English. Output only the translation: 你好"
)

// ModelList contains the model returned by the public /v1/models endpoint.
var ModelList = []string{DefaultModel}

type Adaptor struct{ openai.Adaptor }

// NormalizeBaseURL accepts SDK and complete endpoint URLs without losing proxy prefixes.
func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return constant.ChannelBaseURLs[constant.ChannelTypeBilibili]
	}
	for _, suffix := range []string{"/chat/completions", "/responses", "/models"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return strings.TrimSuffix(base, "/v1")
}

func ValidateBaseURL(base string) error {
	u, err := url.Parse(NormalizeBaseURL(base))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("Bilibili requires an HTTP(S) API URL without credentials, query or fragment")
	}
	return nil
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	var path string
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions:
		path = "/v1/chat/completions"
	case relayconstant.RelayModeResponses:
		path = "/v1/responses"
	default:
		return "", unsupportedRequest()
	}
	if err := ValidateBaseURL(info.ChannelBaseUrl); err != nil {
		return "", invalidRequest(err)
	}
	return NormalizeBaseURL(info.ChannelBaseUrl) + path, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	header.Set("User-Agent", UserAgent)
	header.Del("Authorization")
	if key := strings.TrimSpace(info.ApiKey); key != "" {
		header.Set("Authorization", "Bearer "+key)
	}
	return nil
}

// Apply the official client's translation defaults only to omitted values.
// RawMessage preserves false and zero as well as other template options.
func translationDefaults(temperature **float64, kwargs *json.RawMessage) error {
	if *temperature == nil {
		*temperature = common.GetPointer(0.0)
	}
	options := make(map[string]json.RawMessage)
	if raw := strings.TrimSpace(string(*kwargs)); raw != "" && raw != "null" {
		if err := common.Unmarshal(*kwargs, &options); err != nil || options == nil {
			return invalidRequest(errors.New("chat_template_kwargs must be an object"))
		}
	}
	if _, exists := options["enable_thinking"]; !exists {
		options["enable_thinking"] = json.RawMessage("false")
	}
	raw, err := common.Marshal(options)
	if err != nil {
		return err
	}
	*kwargs = raw
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, invalidRequest(errors.New("request is nil"))
	}
	if err := translationDefaults(&request.Temperature, &request.ChatTemplateKwargs); err != nil {
		return nil, err
	}
	return request, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if err := translationDefaults(&request.Temperature, &request.ChatTemplateKwargs); err != nil {
		return nil, err
	}
	return request, nil
}

// Override DoRequest so the shared sender uses Bilibili URL and header methods.
func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if _, err := a.GetRequestURL(info); err != nil {
		return nil, err
	}
	return channel.DoApiRequest(a, c, info, body)
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return "bilibili" }

func invalidRequest(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func unsupportedRequest() *types.NewAPIError {
	return invalidRequest(errors.New("Bilibili only supports Chat Completions and Responses creation"))
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

var _ channel.Adaptor = (*Adaptor)(nil)
