package mistral

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	nativePath        string
	nativeContentType string
	embeddingFormat   string
	audioFormat       string
	audioGranularity  string
	audioDuration     *float64
}

func unsupported() error {
	return invalidRequest("this endpoint is not supported by the Mistral AI channel")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, unsupported()
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	return nil, unsupported()
}

func (a *Adaptor) ConvertImageRequest(*gin.Context, *relaycommon.RelayInfo, dto.ImageRequest) (any, error) {
	return nil, unsupported()
}

func (a *Adaptor) Init(*relaycommon.RelayInfo) { *a = Adaptor{} }

// NormalizeBaseURL accepts both the provider root and an OpenAI-style /v1 base.
func NormalizeBaseURL(base string) string {
	return strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(base), "/"), "/v1")
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if a.nativePath != "" {
		return nativeURL(info, a.nativePath)
	}
	base, err := url.Parse(NormalizeBaseURL(info.ChannelBaseUrl))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return "", errors.New("invalid Mistral base URL")
	}
	var endpoint string
	switch info.RelayMode {
	case relayconstant.RelayModeChatCompletions:
		endpoint = "/v1/chat/completions"
	case relayconstant.RelayModeEmbeddings:
		endpoint = "/v1/embeddings"
	case relayconstant.RelayModeAudioTranscription:
		endpoint = "/v1/audio/transcriptions"
	default:
		return "", unsupported()
	}
	base.Path = strings.TrimSuffix(strings.TrimRight(base.Path, "/"), "/v1") + endpoint
	base.RawPath, base.Fragment = "", ""
	return base.String(), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	if a.nativeContentType != "" {
		req.Set("Content-Type", a.nativeContentType)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if info.RelayMode != relayconstant.RelayModeChatCompletions {
		return nil, unsupported()
	}
	converted, err := requestOpenAI2Mistral(request)
	if err != nil {
		return nil, err
	}
	info.ReasoningEffort = converted.ReasoningEffort
	return converted, nil
}

func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, unsupported()
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, unsupported()
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	var resp *http.Response
	var err error
	if info.RelayMode == relayconstant.RelayModeAudioTranscription {
		resp, err = channel.DoFormRequest(a, c, info, body)
	} else {
		resp, err = channel.DoApiRequest(a, c, info, body)
	}
	if err != nil || resp == nil || resp.StatusCode == http.StatusOK || a.nativePath != "" {
		return resp, err
	}
	// Normalize before the shared error handler consumes the body. Preserve status
	// and retry headers, including model-specific subscription/rate-limit errors.
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if normalized := normalizeErrorBody(data, resp.StatusCode); normalized != nil {
		data = normalized
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	resp.Header.Del("Content-Length")
	return resp, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, badResponse(errors.New("empty Mistral response"))
	}
	switch info.RelayMode {
	case relayconstant.RelayModeEmbeddings:
		return a.embeddingResponse(c, resp, info)
	case relayconstant.RelayModeAudioTranscription:
		return a.audioResponse(c, resp, info)
	case relayconstant.RelayModeChatCompletions:
		if info.IsStream {
			return streamResponse(c, resp, info)
		}
		return openai.OpenaiHandlerWithBodyTransformer(c, info, resp, normalizeMistralResponseData)
	default:
		resp.Body.Close()
		return nil, invalidRequest("unsupported Mistral response endpoint")
	}
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return ChannelName }
