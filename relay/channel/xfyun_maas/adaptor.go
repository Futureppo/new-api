package xfyun_maas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const DefaultBaseURL = "https://maas-api.cn-huabei-1.xf-yun.com"

// Public examples, not a discovery response or a claim about APIKEY permissions.
var ModelList = []string{
	"spark-x2.5-1.7b", "xop35qwen2b", "xophunyuanocr", "xopzimageturbo", "xopqwentti20b",
	"xopglm53", "xopglm52", "xopdeepseekv4pro0813", "xopdeepseekv4flash0731",
	"xopkimik27code", "xopkimik26", "xopqwen36v35b", "xopqwen35397b",
	"xop3qwen32bvl", "xop3qwen8bembedding", "xop3qwen8breranker",
}

type Adaptor struct {
	openai.Adaptor
	embeddingFormat string
	embeddingCount  int
	rerank          dto.RerankRequest
}

func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return DefaultBaseURL
	}
	for _, suffix := range []string{"/anthropic/v1", "/v2.1", "/v2", "/v1"} {
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix)
		}
	}
	return base
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	base := NormalizeBaseURL(info.ChannelBaseUrl)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", invalid("invalid Xunfei MaaS base URL")
	}
	var path string
	switch {
	case info.RelayFormat == types.RelayFormatClaude:
		path = "/anthropic/v1/messages"
	case info.RelayMode == relayconstant.RelayModeChatCompletions:
		path = "/v2/chat/completions"
	case info.RelayMode == relayconstant.RelayModeResponses:
		path = "/v1/responses"
	case info.RelayMode == relayconstant.RelayModeImagesGenerations:
		path = "/v2.1/tti"
	case info.RelayMode == relayconstant.RelayModeEmbeddings:
		path = "/v2/embeddings"
	case info.RelayMode == relayconstant.RelayModeRerank:
		path = "/v2/rerank"
	default:
		return "", invalid("this endpoint is not supported by Xunfei MaaS")
	}
	return base + path, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, h *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, h)
	h.Set("Content-Type", "application/json")
	if info.RelayFormat == types.RelayFormatClaude {
		h.Del("Authorization")
		h.Set("x-api-key", info.ApiKey)
		version := c.GetHeader("anthropic-version")
		if version == "" {
			version = "2023-06-01"
		}
		h.Set("anthropic-version", version)
	} else {
		h.Set("Authorization", "Bearer "+info.ApiKey)
	}
	return nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	result, err := channel.DoApiRequest(a, c, info, body)
	if err != nil {
		return nil, err
	}
	// The relay checks HTTP status before DoResponse. Normalize MaaS errors here
	// too, so non-200 code/message and header.code responses keep their details.
	if resp := result; resp != nil && resp.StatusCode != http.StatusOK && resp.Body != nil {
		data, readErr := readBody(resp)
		if readErr != nil {
			return nil, readErr
		}
		if apiErr := businessError(data, resp.StatusCode); apiErr != nil {
			data, err = common.Marshal(map[string]any{"error": apiErr.ToOpenAIError()})
			if err != nil {
				return nil, err
			}
		}
		replaceBody(resp, data)
	}
	return result, nil
}

func invalid(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(fmt.Errorf("%s", message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func requestMap(v any) (map[string]json.RawMessage, error) {
	b, err := common.Marshal(v)
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	err = common.Unmarshal(b, &result)
	return result, err
}

// These known vision IDs also accept text-only conversations. Custom vision
// deployments are detected by their image content, for the token limit field.
func isVision(request *dto.GeneralOpenAIRequest) bool {
	switch request.Model {
	case "xop3qwen32bvl", "xopkimik27code", "xopkimik26", "xopkimik25",
		"xopqwen36v35b", "xopqwen35397b", "xoppaddleocrv16", "xophunyuanocr", "xopdeepseekocr":
		return true
	}
	for _, message := range request.Messages {
		for _, part := range message.ParseContent() {
			if part.Type == "image_url" {
				return true
			}
		}
	}
	return false
}

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, invalid("request is nil")
	}
	if request.N != nil && *request.N != 1 {
		return nil, invalid("Xunfei MaaS chat only supports n=1")
	}
	m, err := requestMap(request)
	if err != nil {
		return nil, err
	}
	delete(m, "n")
	if !isVision(request) {
		// Live API verification: all three text protocols require model, and
		// both Spark and Qwen accept stream_options.include_usage.
		if request.MaxCompletionTokens == nil && request.MaxTokens != nil {
			m["max_completion_tokens"] = m["max_tokens"]
		}
		delete(m, "max_tokens")
	}
	return m, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	if (request.Background != nil && *request.Background) || request.PreviousResponseID != "" || len(request.Conversation) > 0 || len(request.ContextManagement) > 0 {
		return nil, invalid("Xunfei MaaS Responses does not support background, previous_response_id, conversation or context_management; send the conversation in input")
	}
	var tools []struct {
		Type string `json:"type"`
	}
	if len(request.Tools) > 0 {
		if err := common.Unmarshal(request.Tools, &tools); err != nil {
			return nil, invalid("invalid Responses tools")
		}
		for _, tool := range tools {
			if tool.Type != "function" {
				return nil, invalid("Xunfei MaaS Responses only supports function tools")
			}
		}
	}
	m, err := requestMap(request)
	if err != nil {
		return nil, err
	}
	delete(m, "background")
	delete(m, "stream_options")
	return m, nil
}

func (a *Adaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, invalid("request is nil")
	}
	if len(request.McpServers) > 0 || len(request.Container) > 0 || len(request.ContextManagement) > 0 {
		return nil, invalid("Xunfei MaaS Messages does not support MCP, container or context_management")
	}
	m, err := requestMap(request)
	if err != nil {
		return nil, err
	}
	if request.System != nil && !request.IsStringSystem() {
		var texts []string
		for _, part := range request.ParseSystem() {
			if part.Type != "text" {
				return nil, invalid("Xunfei MaaS system only supports text")
			}
			texts = append(texts, part.GetText())
		}
		m["system"], err = common.Marshal(strings.Join(texts, "\n"))
		if err != nil {
			return nil, err
		}
	}
	if request.Thinking != nil {
		if request.Thinking.BudgetTokens != nil {
			return nil, invalid("Xunfei MaaS cannot honor thinking.budget_tokens; use thinking.type with output_config.effort")
		}
		switch request.Thinking.Type {
		case "enabled", "adaptive":
			m["enable_thinking"] = json.RawMessage("true")
		case "disabled":
			m["enable_thinking"] = json.RawMessage("false")
		default:
			return nil, invalid("unsupported thinking.type")
		}
		if request.Thinking.Display != "" {
			switch request.Thinking.Display {
			case "omitted":
				m["clear_thinking"] = json.RawMessage("true")
			case "summarized":
				m["clear_thinking"] = json.RawMessage("false")
			default:
				return nil, invalid("unsupported thinking.display")
			}
		}
		delete(m, "thinking")
	}
	if len(request.OutputConfig) > 0 {
		var config struct {
			Effort string `json:"effort"`
		}
		if err := common.Unmarshal(request.OutputConfig, &config); err != nil {
			return nil, invalid("invalid output_config")
		}
		switch config.Effort {
		case "low", "medium", "high":
			m["reasoning_effort"], _ = common.Marshal(config.Effort)
		default:
			return nil, invalid("Xunfei MaaS Messages effort must be low, medium or high")
		}
		delete(m, "output_config")
	}
	return m, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(_ *gin.Context, _ *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	inputs, err := textInputs(request.Input)
	if err != nil || len(inputs) == 0 {
		return nil, invalid("embedding input must be a non-empty string or string array; token IDs are not supported")
	}
	for _, input := range inputs {
		if input == "" {
			return nil, invalid("embedding input cannot contain empty text")
		}
	}
	a.embeddingCount = len(inputs)
	if request.InputType != "" || len(request.EmbeddingTypes) != 0 || request.Truncate != "" {
		return nil, invalid("Xunfei MaaS embeddings does not support input_type, embedding_types or truncate")
	}
	switch request.EncodingFormat {
	case "", "float", "base64":
		a.embeddingFormat = request.EncodingFormat
	default:
		return nil, invalid("encoding_format must be float or base64")
	}
	if request.Dimensions != nil && *request.Dimensions <= 0 {
		return nil, invalid("dimensions must be positive")
	}
	return struct {
		Model          string `json:"model"`
		Input          any    `json:"input"`
		EncodingFormat string `json:"encoding_format"`
		Dimensions     *int   `json:"dimensions,omitempty"`
	}{request.Model, request.Input, "float", request.Dimensions}, nil
}

func textInputs(input any) ([]string, error) {
	switch v := input.(type) {
	case string:
		return []string{v}, nil
	case []string:
		return v, nil
	case []any:
		texts := make([]string, len(v))
		for i, item := range v {
			var ok bool
			texts[i], ok = item.(string)
			if !ok {
				return nil, fmt.Errorf("non-text input")
			}
		}
		return texts, nil
	default:
		return nil, fmt.Errorf("non-text input")
	}
}

func (a *Adaptor) ConvertRerankRequest(_ *gin.Context, _ int, request dto.RerankRequest) (any, error) {
	texts, err := textInputs(request.Documents)
	if err != nil || len(texts) == 0 || request.Query == "" {
		return nil, invalid("rerank requires query and a non-empty array of document strings")
	}
	if request.TopN != nil && *request.TopN <= 0 {
		return nil, invalid("top_n must be positive")
	}
	if request.MaxChunkPerDoc != nil || request.OverLapTokens != nil {
		return nil, invalid("Xunfei MaaS rerank does not support max_chunk_per_doc or overlap_tokens")
	}
	a.rerank = request
	return struct {
		Model     string   `json:"model"`
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
	}{request.Model, request.Query, texts}, nil
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, invalid("Xunfei MaaS does not support the Gemini endpoint")
}

func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, invalid("Xunfei MaaS does not support audio endpoints")
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return "xfyun_maas" }

var _ channel.Adaptor = (*Adaptor)(nil)
