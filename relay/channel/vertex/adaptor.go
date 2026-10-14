package vertex

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const (
	RequestModeClaude     = 1
	RequestModeGemini     = 2
	RequestModeOpenSource = 3
)

var claudeModelMap = map[string]string{
	"claude-3-sonnet-20240229":   "claude-3-sonnet@20240229",
	"claude-3-opus-20240229":     "claude-3-opus@20240229",
	"claude-3-haiku-20240307":    "claude-3-haiku@20240307",
	"claude-3-5-sonnet-20240620": "claude-3-5-sonnet@20240620",
	"claude-3-5-sonnet-20241022": "claude-3-5-sonnet-v2@20241022",
	"claude-3-7-sonnet-20250219": "claude-3-7-sonnet@20250219",
	"claude-sonnet-4-20250514":   "claude-sonnet-4@20250514",
	"claude-opus-4-20250514":     "claude-opus-4@20250514",
	"claude-opus-4-1-20250805":   "claude-opus-4-1@20250805",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4-5@20250929",
	"claude-haiku-4-5-20251001":  "claude-haiku-4-5@20251001",
	"claude-opus-4-5-20251101":   "claude-opus-4-5@20251101",
	"claude-opus-4-6":            "claude-opus-4-6",
	"claude-opus-4-7":            "claude-opus-4-7",
}

const anthropicVersion = "vertex-2023-10-16"

type Adaptor struct {
	RequestMode           int
	AccountCredentials    Credentials
	capability            vertexmodel.Capability
	initErr               error
	action                string
	native                bool
	responseKind          string
	embeddingFormat       string
	embeddingNativeAction string
	audioFormat           string
	imageFormat           string
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	// Vertex AI does not support functionResponse.id; keep it stripped here for consistency.
	if model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled {
		removeFunctionResponseID(request)
	}
	geminiAdaptor := gemini.Adaptor{}
	return geminiAdaptor.ConvertGeminiRequest(c, info, request)
}

func removeFunctionResponseID(request *dto.GeminiChatRequest) {
	if request == nil {
		return
	}

	if len(request.Contents) > 0 {
		for i := range request.Contents {
			if len(request.Contents[i].Parts) == 0 {
				continue
			}
			for j := range request.Contents[i].Parts {
				part := &request.Contents[i].Parts[j]
				if part.FunctionResponse == nil {
					continue
				}
				if len(part.FunctionResponse.ID) > 0 {
					part.FunctionResponse.ID = nil
				}
			}
		}
	}

	if len(request.Requests) > 0 {
		for i := range request.Requests {
			removeFunctionResponseID(&request.Requests[i])
		}
	}
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	if a.capability.Family != vertexmodel.Claude {
		return nil, errors.New("Vertex messages requires a Claude model")
	}
	request.Model = a.capability.Model
	if v, ok := claudeModelMap[info.UpstreamModelName]; ok {
		c.Set("request_model", v)
	} else {
		c.Set("request_model", request.Model)
	}
	vertexClaudeReq := copyRequest(request, anthropicVersion)
	return vertexClaudeReq, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return a.convertAudio(c, info, request)
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return a.convertImage(c, info, request)
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.capability, a.initErr = vertexmodel.Resolve(info.UpstreamModelName)
	a.RequestMode = RequestModeGemini
	switch a.capability.Family {
	case vertexmodel.Claude:
		a.RequestMode = RequestModeClaude
	case vertexmodel.OpenAPI, vertexmodel.Grok, vertexmodel.PartnerChat, vertexmodel.FIM, vertexmodel.OCR:
		a.RequestMode = RequestModeOpenSource
	}
}

func (a *Adaptor) getRequestUrl(info *relaycommon.RelayInfo, modelName, action string) (string, error) {
	if a.initErr != nil {
		return "", a.initErr
	}
	cap := a.capability
	region, err := ResolveModelRegion(info.ApiVersion, info.OriginModelName)
	if err != nil {
		return "", err
	}
	if cap.Family == vertexmodel.Grok {
		region = "global"
	}
	project := ""
	if info.ChannelOtherSettings.VertexKeyType != dto.VertexKeyTypeAPIKey {
		creds, err := ParseCredentials(info.ApiKey)
		if err != nil {
			return "", err
		}
		a.AccountCredentials = creds
		project = creds.ProjectID
	} else {
		if cap.Publisher != "google" {
			return "", errors.New("Vertex API key mode only supports Google publisher models")
		}
		if action != "generateContent" && action != "streamGenerateContent" && action != "countTokens" {
			return "", errors.New("this Vertex method requires service-account JSON credentials")
		}
		region = "global"
	}
	if action == "chat/completions" || action == "responses" {
		if action == "chat/completions" && cap.Family == vertexmodel.OpenAPI {
			return BuildOpenSourceChatCompletionsURL(info.ChannelBaseUrl, project, region), nil
		}
		return buildPublisherAPIBaseURL(info.ChannelBaseUrl, "v1", project, region) + "/endpoints/openapi/" + action, nil
	}
	if cap.Family == vertexmodel.Claude {
		if mapped, ok := claudeModelMap[modelName]; ok {
			modelName = mapped
		}
	}
	suffix := action
	if action == "streamGenerateContent" {
		suffix += "?alt=sse"
	}
	result := BuildPublisherModelURL(info.ChannelBaseUrl, cap.Version, project, region, cap.Publisher, modelName, suffix)
	if project == "" {
		return AppendAPIKey(result, info.ApiKey)
	}
	return result, nil
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if a.initErr != nil {
		return "", a.initErr
	}
	if (info.RelayMode == constant.RelayModeResponses && a.capability.Family != vertexmodel.Grok) || info.RelayMode == constant.RelayModeResponsesCompact {
		return "", errors.New("Vertex only supports stateless Grok Responses; compact is unsupported")
	}
	modelName := a.capability.Model
	if a.RequestMode == RequestModeGemini && a.capability.Action == "generateContent" && model_setting.GetGeminiSettings().ThinkingAdapterEnabled && !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
		if strings.Contains(modelName, "-thinking-") {
			modelName = strings.SplitN(modelName, "-thinking-", 2)[0]
		} else if strings.HasSuffix(modelName, "-thinking") {
			modelName = strings.TrimSuffix(modelName, "-thinking")
		} else if strings.HasSuffix(modelName, "-nothinking") {
			modelName = strings.TrimSuffix(modelName, "-nothinking")
		} else if base, level, ok := reasoning.TrimEffortSuffix(modelName); ok && level != "" {
			modelName = base
		}
	}
	action := a.action
	if action == "" {
		action = a.capability.Action
		switch info.RelayMode {
		case constant.RelayModeResponses:
			action = "responses"
		case constant.RelayModeResponsesCompact:
			return "", errors.New("Vertex does not support Responses compact")
		}
		if info.IsStream {
			if action == "generateContent" {
				action = "streamGenerateContent"
			}
			if action == "rawPredict" {
				action = "streamRawPredict"
			}
		}
	}
	return a.getRequestUrl(info, modelName, action)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Content-Type", "application/json")
	if info.ChannelOtherSettings.VertexKeyType != dto.VertexKeyTypeAPIKey {
		if strings.TrimSpace(a.AccountCredentials.ClientEmail) == "" {
			creds, err := ParseCredentials(info.ApiKey)
			if err != nil {
				return err
			}
			a.AccountCredentials = creds
		}
		accessToken, err := getAccessToken(c.Request.Context(), a, info)
		if err != nil {
			return err
		}
		req.Set("Authorization", "Bearer "+accessToken)
	}
	if strings.Contains(info.UpstreamModelName, "claude") {
		claude.CommonClaudeHeadersOperation(c, req, info)
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if a.initErr != nil {
		return nil, a.initErr
	}
	if a.RequestMode == RequestModeGemini && strings.HasPrefix(info.UpstreamModelName, "imagen") {
		prompt := ""
		for _, m := range request.Messages {
			if m.Role == "user" {
				prompt = m.StringContent()
				if prompt != "" {
					break
				}
			}
		}
		if prompt == "" {
			if p, ok := request.Prompt.(string); ok {
				prompt = p
			}
		}
		if prompt == "" {
			return nil, errors.New("prompt is required for image generation")
		}

		imgReq := dto.ImageRequest{
			Model:  request.Model,
			Prompt: prompt,
			N:      lo.ToPtr(uint(1)),
			Size:   "1024x1024",
		}
		if request.N != nil && *request.N > 0 {
			imgReq.N = lo.ToPtr(uint(*request.N))
		}
		if request.Size != "" {
			imgReq.Size = request.Size
		}
		if len(request.ExtraBody) > 0 {
			var extra map[string]any
			if err := common.Unmarshal(request.ExtraBody, &extra); err == nil {
				if n, ok := extra["n"].(float64); ok && n > 0 {
					imgReq.N = lo.ToPtr(uint(n))
				}
				if size, ok := extra["size"].(string); ok {
					imgReq.Size = size
				}
				// accept aspectRatio in extra body (top-level or under parameters)
				if ar, ok := extra["aspectRatio"].(string); ok && ar != "" {
					imgReq.Size = ar
				}
				if params, ok := extra["parameters"].(map[string]any); ok {
					if ar, ok := params["aspectRatio"].(string); ok && ar != "" {
						imgReq.Size = ar
					}
				}
			}
		}
		c.Set("request_model", request.Model)
		return a.ConvertImageRequest(c, info, imgReq)
	}
	if a.RequestMode == RequestModeClaude {
		request.Model = a.capability.Model
		claudeReq, err := claude.RequestOpenAI2ClaudeMessage(c, *request)
		if err != nil {
			return nil, err
		}
		vertexClaudeReq := copyRequest(claudeReq, anthropicVersion)
		c.Set("request_model", claudeReq.Model)
		info.UpstreamModelName = claudeReq.Model
		return vertexClaudeReq, nil
	} else if a.RequestMode == RequestModeGemini {
		if a.capability.Action != "generateContent" {
			return nil, errors.New("this Vertex model requires its native or dedicated compatibility endpoint")
		}
		request.Model = a.capability.Model
		geminiRequest, err := gemini.CovertOpenAI2Gemini(c, *request, info)
		if err != nil {
			return nil, err
		}
		c.Set("request_model", request.Model)
		return geminiRequest, nil
	} else if a.RequestMode == RequestModeOpenSource {
		if a.capability.Family == vertexmodel.OCR {
			return nil, errors.New("use /v1/ocr for Vertex Mistral OCR")
		}
		if a.capability.Family == vertexmodel.FIM {
			return nil, errors.New("use /v1/fim/completions for Vertex Codestral")
		}
		if a.capability.Family == vertexmodel.OpenAPI || a.capability.Family == vertexmodel.Grok {
			request.Model = a.capability.OpenAIModel()
		} else {
			request.Model = a.capability.Model
			request.StreamOptions = nil
		}
		return request, nil
	}
	return nil, errors.New("unsupported request mode")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("Vertex model channels do not support the Vertex AI Search rerank API")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return a.convertEmbedding(c, info, request)
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return a.convertResponses(c, info, request)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	if info.RelayMode == constant.RelayModeResponses {
		// Revalidate after channel parameter overrides and passthrough handling.
		var request dto.OpenAIResponsesRequest
		if err := common.DecodeJson(requestBody, &request); err != nil {
			return nil, err
		}
		converted, err := a.convertResponses(c, info, request)
		if err != nil {
			return nil, err
		}
		data, err := common.Marshal(converted)
		if err != nil {
			return nil, err
		}
		requestBody = bytes.NewReader(data)
	}
	if a.responseKind == "embedding" {
		return a.doEmbeddings(c, info, requestBody)
	}
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if a.native {
		return a.nativeResponse(c, resp, info)
	}
	switch a.responseKind {
	case "embedding":
		return a.embeddingResponse(c, resp, info)
	case "image":
		return a.imageResponse(c, resp, info)
	case "speech", "transcribe":
		return a.audioResponse(c, resp, info)
	}
	if info.RelayMode == constant.RelayModeResponses {
		if info.IsStream {
			return openai.OaiResponsesStreamHandler(c, info, resp)
		}
		return openai.OaiResponsesHandler(c, info, resp)
	}
	claudeAdaptor := claude.Adaptor{}
	if info.IsStream {
		switch a.RequestMode {
		case RequestModeClaude:
			return claudeAdaptor.DoResponse(c, resp, info)
		case RequestModeGemini:
			if info.RelayMode == constant.RelayModeGemini {
				return gemini.GeminiTextGenerationStreamHandler(c, info, resp)
			} else {
				return gemini.GeminiChatStreamHandler(c, info, resp)
			}
		case RequestModeOpenSource:
			return openai.OaiStreamHandler(c, info, resp)
		}
	} else {
		switch a.RequestMode {
		case RequestModeClaude:
			return claudeAdaptor.DoResponse(c, resp, info)
		case RequestModeGemini:
			if info.RelayMode == constant.RelayModeGemini {
				return gemini.GeminiTextGenerationHandler(c, info, resp)
			} else {
				if strings.HasPrefix(info.UpstreamModelName, "imagen") {
					return gemini.GeminiImageHandler(c, info, resp)
				}
				return gemini.GeminiChatHandler(c, info, resp)
			}
		case RequestModeOpenSource:
			return openai.OpenaiHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	modelList := make([]string, 0, len(vertexmodel.Catalog)+len(claude.ModelList))
	seen := map[string]bool{}
	for _, list := range [][]string{ModelList, vertexmodel.Catalog, claude.ModelList} {
		for _, name := range list {
			if _, err := vertexmodel.Resolve(name); err == nil && !seen[name] {
				seen[name] = true
				modelList = append(modelList, name)
			}
		}
	}
	return modelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
