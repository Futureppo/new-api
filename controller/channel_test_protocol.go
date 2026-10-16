package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
)

// Native integrations retain their authentication and provider-specific request
// formats. Every other combination is still testable using the selected wire
// protocol at the original channel's base URL, key, mapping and overrides.
// The returned copy is only used by this test; the saved channel is never changed.
func channelForExplicitEndpointTest(source *model.Channel, endpoint constant.EndpointType, modelName string) (*model.Channel, bool, error) {
	protocolType := constant.ChannelTypeOpenAI
	native := false
	switch endpoint {
	case constant.EndpointTypeTypeSafeSystemOne:
		protocolType = constant.ChannelTypeTypeSafe
		native = source.Type == constant.ChannelTypeTypeSafe || source.Type == constant.ChannelTypeQianwenAI
	case constant.EndpointTypeOpenAIDecisions:
		native = source.Type == constant.ChannelTypeOpenAI || source.Type == constant.ChannelTypeOpenRouter
	case constant.EndpointTypeOpenAIResponseCompact:
		native = source.Type == constant.ChannelTypeOpenAI || source.Type == constant.ChannelTypeCodex
	case constant.EndpointTypeCohereChat, constant.EndpointTypeCohereRerank, constant.EndpointTypeCohereEmbeddings:
		protocolType = constant.ChannelTypeCohere
		native = source.Type == constant.ChannelTypeCohere
	case constant.EndpointTypeAnthropic:
		protocolType = constant.ChannelTypeAnthropic
		native = lo.Contains(common.GetEndpointTypesByChannelType(source.Type, modelName), endpoint)
	case constant.EndpointTypeGemini:
		protocolType = constant.ChannelTypeGemini
		native = lo.Contains(common.GetEndpointTypesByChannelType(source.Type, modelName), endpoint)
	case constant.EndpointTypeVertexNative:
		native = source.Type == constant.ChannelTypeVertexAi
	case constant.EndpointTypeBatchGeneration:
		protocolType = constant.ChannelTypeGMICloud
		native = source.Type == constant.ChannelTypeGMICloud
	case constant.EndpointTypeOpenAIVideo:
		native = source.Type == constant.ChannelTypeOpenAI || source.Type == constant.ChannelTypeOpenRouter ||
			lo.Contains(common.GetEndpointTypesByChannelType(source.Type, modelName), endpoint)
	case constant.EndpointTypeOpenAI:
		// Compatibility conversions (e.g. Chat -> Messages/Gemini) do not test
		// the explicitly selected /chat/completions endpoint.
		native = source.Type != constant.ChannelTypeAnthropic && source.Type != constant.ChannelTypeGemini &&
			source.Type != constant.ChannelTypeAws && source.Type != constant.ChannelTypeTypeSafe &&
			lo.Contains(common.GetEndpointTypesByChannelType(source.Type, modelName), endpoint)
	default:
		native = source.Type == constant.ChannelTypeOpenAI || lo.Contains(common.GetEndpointTypesByChannelType(source.Type, modelName), endpoint)
	}
	if native {
		return source, false, nil
	}
	copy := *source
	baseURL := source.GetBaseURL()
	if baseURL == "" && source.Type >= 0 && source.Type < len(constant.ChannelBaseURLs) {
		baseURL = constant.ChannelBaseURLs[source.Type]
	}
	if strings.TrimSpace(baseURL) == "" {
		return nil, false, fmt.Errorf("configure a channel base URL before testing an endpoint")
	}
	copy.BaseURL = &baseURL
	copy.Type = protocolType
	return &copy, true, nil
}

func explicitTestRequestURL(info *relaycommon.RelayInfo, endpoint constant.EndpointType) string {
	metadata, _ := common.GetDefaultEndpointInfo(endpoint)
	path := metadata.Path
	switch endpoint {
	case constant.EndpointTypeCohereChat:
		path = "/v2/chat"
	case constant.EndpointTypeCohereRerank:
		path = "/v2/rerank"
	case constant.EndpointTypeCohereEmbeddings:
		path = "/v2/embed"
	case constant.EndpointTypeVertexNative:
		path = info.RequestURLPath
	case constant.EndpointTypeGemini:
		if info.IsStream {
			path = strings.Replace(path, ":generateContent", ":streamGenerateContent?alt=sse", 1)
		}
	}
	path = strings.ReplaceAll(path, "{model}", url.PathEscape(info.UpstreamModelName))
	if endpoint == constant.EndpointTypeVertexNative {
		if request, ok := info.Request.(*dto.VertexRequest); ok {
			path = "/v1/models/" + url.PathEscape(info.UpstreamModelName) + ":" + request.Action
		}
	}
	base := strings.TrimRight(info.ChannelBaseUrl, "/")
	for _, version := range []string{"/v1", "/v2", "/v1beta"} {
		if strings.HasSuffix(base, version) && strings.HasPrefix(path, version+"/") {
			base = strings.TrimSuffix(base, version)
			break
		}
	}
	return base + path
}

type explicitEndpointTestAdaptor struct {
	channel.Adaptor
	endpoint constant.EndpointType
}

func (a *explicitEndpointTestAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return explicitTestRequestURL(info, a.endpoint), nil
}

func (a *explicitEndpointTestAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, body)
}

func (a *explicitEndpointTestAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if a.endpoint != constant.EndpointTypeVertexNative {
		return a.Adaptor.DoResponse(c, resp, info)
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil || !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() || len(gjson.ParseBytes(body).Map()) == 0 {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("invalid native endpoint response"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if err := validateTestResponseBody(body, false); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	usage := &dto.Usage{
		PromptTokens:     int(gjson.GetBytes(body, "usageMetadata.promptTokenCount").Int()),
		CompletionTokens: int(gjson.GetBytes(body, "usageMetadata.candidatesTokenCount").Int()),
		TotalTokens:      int(gjson.GetBytes(body, "usageMetadata.totalTokenCount").Int()),
	}
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}

// Task probes submit once and report the upstream task identifier. They do not
// persist or poll the task, just like the existing native channel test flow.
type explicitEndpointTaskTestAdaptor struct {
	channel.TaskAdaptor
	endpoint constant.EndpointType
	body     []byte
}

func (a *explicitEndpointTaskTestAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if a.endpoint == constant.EndpointTypeBatchGeneration {
		info.Action = constant.TaskActionBatchInference
	} else {
		info.Action = constant.TaskActionGenerate
	}
	return nil
}

func (a *explicitEndpointTaskTestAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return explicitTestRequestURL(info, a.endpoint), nil
}

func (a *explicitEndpointTaskTestAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, &req.Header)
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", gin.MIMEJSON)
	return nil
}

func (a *explicitEndpointTaskTestAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	var body map[string]any
	if err := common.Unmarshal(a.body, &body); err != nil {
		return nil, err
	}
	body["model"] = info.UpstreamModelName
	data, err := common.Marshal(body)
	if err == nil && len(info.ParamOverride) > 0 {
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
	}
	if err != nil {
		return nil, err
	}
	if gjson.GetBytes(data, "stream").Bool() {
		return nil, fmt.Errorf("task tests do not support streaming")
	}
	return bytes.NewReader(data), nil
}

func (a *explicitEndpointTaskTestAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, body)
}

func (a *explicitEndpointTaskTestAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusBadGateway)
	}
	if err := validateTestResponseBody(body, false); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "invalid_response", http.StatusBadGateway)
	}
	var id string
	for _, field := range []string{"id", "request_id", "task_id"} {
		if id = gjson.GetBytes(body, field).String(); id != "" {
			break
		}
	}
	if id == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("task id is missing"), "invalid_response", http.StatusBadGateway)
	}
	switch gjson.GetBytes(body, "status").String() {
	case "failed", "failure", "cancelled", "canceled":
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream task was not accepted"), "invalid_response", http.StatusBadGateway)
	}
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, resp, body)
	return id, body, nil
}
