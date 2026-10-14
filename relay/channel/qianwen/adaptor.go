package qianwen

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
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

const DefaultTestModel = "qwen3.7-plus"

// Suggestions from the public documentation, not an account's authorized models.
var ModelList = []string{
	"qwen3.8-max", "qwen3.8-flash", DefaultTestModel, "deepseek-v4.1-flash", "kimi-k3", "glm-5.3", "MiniMax-M2.5",
	"qwen-image-3.0-pro", "qwen-image-3.0", "qwen-image-2.1-pro", "wan2.7-image-pro",
	"wan3.0-video", "wan3.0-video-prime", "wan2.7-t2v", "wan2.7-i2v",
	"qwen-audio-3.1-tts-flash", "qwen-audio-3.0-tts-flash", "qwen-audio-3.1-tts-next", "qwen3-tts-flash", "fun-music-v1",
	"text-embedding-v4", "text-embedding-v3", "qwen3-rerank", "qwen3.7-text-rerank", "qwen3-vl-rerank",
	constant.QianwenIntentModel,
}

type Adaptor struct{}

// NormalizeBaseURL accepts the two SDK bases and complete endpoint URLs,
// retaining any reverse-proxy prefix before the known suffix.
func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return constant.ChannelBaseURLs[constant.ChannelTypeQianwenAI]
	}
	for _, suffix := range []string{
		"/compatible-mode/v1/chat/completions", "/compatible-mode/v1/responses", "/compatible-mode/v1/models",
		"/apps/anthropic/v1/messages", "/apps/anthropic/v1", "/apps/anthropic", "/compatible-mode/v1",
		"/compatible-mode/v1/images/generations", "/compatible-mode/v1/embeddings",
		"/compatible-api/v1/reranks", "/compatible-api/v1",
		multimodalPath, speechPath, musicPath, videoPath, rerankPath, "/api/v1",
		"/api/v1/services/aigc/text-generation/generation",
	} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix)
		}
	}
	return base
}

func ValidateBaseURL(base string) error {
	u, err := url.Parse(NormalizeBaseURL(base))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return invalid("invalid QianwenAI base URL")
	}
	return nil
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	adaptor := openai.Adaptor{}
	adaptor.Init(info)
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.ChannelMeta != nil && constant.IsQianwenIntentModel(info.UpstreamModelName) &&
		(info.RelayFormat != types.RelayFormatOpenAI || info.RelayMode != relayconstant.RelayModeChatCompletions) {
		return "", invalid("tongyi-intent-detect-v3 uses /v1/chat/completions; select the intent or tool mode in the system message")
	}
	var path string
	switch {
	case info.RelayFormat == types.RelayFormatClaude:
		path = "/apps/anthropic/v1/messages"
	case info.RelayFormat == types.RelayFormatOpenAI && info.RelayMode == relayconstant.RelayModeChatCompletions:
		path = "/compatible-mode/v1/chat/completions"
	case info.RelayFormat == types.RelayFormatOpenAIResponses && info.RelayMode == relayconstant.RelayModeResponses:
		path = "/compatible-mode/v1/responses"
	case info.RelayMode == relayconstant.RelayModeEmbeddings:
		path = "/compatible-mode/v1/embeddings"
	case info.RelayMode == relayconstant.RelayModeRerank:
		path = rerankPath
		if info.UpstreamModelName == "qwen3-rerank" {
			path = "/compatible-api/v1/reranks"
		}
	case info.RelayMode == relayconstant.RelayModeImagesGenerations || info.RelayMode == relayconstant.RelayModeImagesEdits:
		path = multimodalPath
		if compatibleImage(info.UpstreamModelName) {
			path = "/compatible-mode/v1/images/generations"
		}
	case info.RelayMode == relayconstant.RelayModeAudioSpeech:
		path = audioPath(info.UpstreamModelName)
	default:
		return "", unsupportedRequest()
	}
	if err := ValidateBaseURL(info.ChannelBaseUrl); err != nil {
		return "", err
	}
	return NormalizeBaseURL(info.ChannelBaseUrl) + path, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, header)
	header.Set("Content-Type", "application/json")
	if info.RelayMode == relayconstant.RelayModeAudioSpeech && info.IsStream {
		header.Set("X-DashScope-SSE", "enable")
	}
	if info.RelayFormat == types.RelayFormatClaude {
		header.Del("Authorization")
		header.Set("x-api-key", info.ApiKey)
		version := c.GetHeader("anthropic-version")
		if version == "" {
			version = "2023-06-01"
		}
		header.Set("anthropic-version", version)
	} else {
		header.Del("x-api-key")
		header.Set("Authorization", "Bearer "+info.ApiKey)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, invalid("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, invalid("request is nil")
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
	// Validate the final body, including passthrough and administrator overrides.
	if info.RelayMode == relayconstant.RelayModeResponses {
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, err
		}
		var request struct {
			Background *bool `json:"background"`
			Store      *bool `json:"store"`
		}
		if err := common.Unmarshal(data, &request); err != nil {
			return nil, invalid("invalid QianwenAI Responses request")
		}
		if request.Background != nil && *request.Background {
			return nil, invalid("QianwenAI does not support background Responses")
		}
		c.Set("qianwen_store_response", request.Store == nil || *request.Store)
		body = bytes.NewReader(data)
	}
	return channel.DoApiRequest(a, c, info, body)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		return imageResponse(c, resp, info)
	case relayconstant.RelayModeRerank:
		return rerankResponse(c, resp, info)
	case relayconstant.RelayModeAudioSpeech:
		return audioResponse(c, resp, info)
	}
	if info.RelayFormat == types.RelayFormatClaude {
		adaptor := claude.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	}
	adaptor := openai.Adaptor{}
	return adaptor.DoResponse(c, resp, info)
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return "QianwenAI" }

func invalid(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func unsupportedRequest() error {
	return invalid("QianwenAI does not support this endpoint; Responses compact and background execution are not documented by the provider")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, unsupportedRequest()
}
func (a *Adaptor) ConvertEmbeddingRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	if request.OutputType != "" && request.OutputType != "dense" {
		return nil, invalid("QianwenAI OpenAI embeddings do not support sparse output; use a DashScope sparse embedding client")
	}
	if request.EncodingFormat != "" && request.EncodingFormat != "float" {
		return nil, invalid("QianwenAI embeddings only support encoding_format=float")
	}
	return request, nil
}

var _ channel.Adaptor = (*Adaptor)(nil)

func (a *Adaptor) FinalizeRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	// Resource lifecycle requests must use the same channel credential as create.
	// Preserve unrelated administrator header overrides.
	if info.RelayFormat == types.RelayFormatClaude {
		header.Del("Authorization")
		header.Set("x-api-key", info.ApiKey)
		if header.Get("anthropic-version") == "" {
			header.Set("anthropic-version", "2023-06-01")
		}
	} else {
		header.Del("x-api-key")
		header.Set("Authorization", "Bearer "+info.ApiKey)
	}
	return nil
}
