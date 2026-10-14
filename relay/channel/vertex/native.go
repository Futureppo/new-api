package vertex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func badResponse(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) ConvertNativeRequest(c *gin.Context, info *relaycommon.RelayInfo, r *dto.VertexRequest) (io.Reader, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	a.native = true
	a.action = r.Action
	if !a.capability.Allows(r.Action) {
		return nil, fmt.Errorf("Vertex model %s does not support %s", r.Model, r.Action)
	}
	if r.Action == "predictLongRunning" || r.Action == "fetchPredictOperation" {
		return nil, errors.New("video operations must use the task relay")
	}
	if c.Request.URL.Path == "/v1/ocr" && a.capability.Family != vertexmodel.OCR {
		return nil, errors.New("/v1/ocr requires a Vertex OCR model")
	}
	if c.Request.URL.Path == "/v1/fim/completions" && a.capability.Family != vertexmodel.FIM {
		return nil, errors.New("/v1/fim/completions requires a Vertex Codestral model")
	}
	fields := make(map[string]json.RawMessage, len(r.Fields))
	for k, v := range r.Fields {
		fields[k] = v
	}
	if model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled && (r.Action == "generateContent" || r.Action == "streamGenerateContent") {
		stripNativeFunctionResponseIDs(fields)
	}
	if a.capability.Publisher == "anthropic" {
		delete(fields, "model")
		fields["anthropic_version"] = raw(anthropicVersion)
	} else if a.capability.Action == "rawPredict" {
		fields["model"] = raw(a.capability.Model)
	} else {
		delete(fields, "model")
	}
	if r.Action == "rawPredict" && r.IsStream(c) {
		a.action = "streamRawPredict"
		if !a.capability.Allows(a.action) {
			return nil, errors.New("streaming is not supported by this Vertex model")
		}
	}
	if a.action == "streamRawPredict" {
		fields["stream"] = raw(true)
	}
	if r.Action == "embedContent" || r.Action == "batchEmbedContents" {
		a.responseKind = "embedding"
		a.embeddingNativeAction = r.Action
		a.action = a.capability.Action
		if r.Action == "embedContent" {
			fields = map[string]json.RawMessage{"requests": raw([]any{fields})}
		}
	}
	if _, err := a.GetRequestURL(info); err != nil {
		return nil, err
	}
	data, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
		if err != nil {
			return nil, err
		}
	}
	if a.capability.Action == "rawPredict" {
		var effective struct {
			Stream *bool `json:"stream"`
		}
		if err := common.Unmarshal(data, &effective); err != nil {
			return nil, err
		}
		if effective.Stream != nil && *effective.Stream {
			a.action = "streamRawPredict"
			if !a.capability.Allows(a.action) {
				return nil, errors.New("streaming is not supported by this Vertex model")
			}
		}
		if a.action == "streamRawPredict" && effective.Stream != nil && !*effective.Stream {
			return nil, errors.New("stream=false conflicts with streamRawPredict")
		}
	}
	info.IsStream = strings.HasPrefix(a.action, "stream")
	info.DisablePing = true
	return bytes.NewReader(data), nil
}

// Strip only the unsupported field, without re-encoding parts through the
// Gemini DTO (which would discard provider extension fields).
func stripNativeFunctionResponseIDs(fields map[string]json.RawMessage) {
	var contents []map[string]json.RawMessage
	if common.Unmarshal(fields["contents"], &contents) != nil {
		return
	}
	for _, content := range contents {
		var parts []map[string]json.RawMessage
		if common.Unmarshal(content["parts"], &parts) != nil {
			continue
		}
		for _, part := range parts {
			var response map[string]json.RawMessage
			if common.Unmarshal(part["functionResponse"], &response) == nil && response != nil {
				delete(response, "id")
				part["functionResponse"] = raw(response)
			}
		}
		content["parts"] = raw(parts)
	}
	fields["contents"] = raw(contents)
}

func (a *Adaptor) RequiresCallPrice() bool { return a.capability.RequiresCallPrice() }

func (a *Adaptor) convertResponses(c *gin.Context, info *relaycommon.RelayInfo, r dto.OpenAIResponsesRequest) (any, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	if a.capability.Family != vertexmodel.Grok {
		return nil, errors.New("Vertex Responses is only supported for documented Grok models")
	}
	if len(r.ContextManagement) > 0 && string(r.ContextManagement) != "null" && string(r.ContextManagement) != "[]" {
		return nil, errors.New("Vertex Grok does not support context_management or compact")
	}
	if r.PreviousResponseID != "" || (r.Background != nil && *r.Background) {
		return nil, errors.New("Vertex Grok does not support previous_response_id or background mode")
	}
	if len(r.Store) > 0 && string(r.Store) != "null" {
		var store bool
		if err := common.Unmarshal(r.Store, &store); err != nil {
			return nil, err
		}
		if store {
			return nil, errors.New("Vertex Grok requires store=false")
		}
	}
	r.Store = raw(false)
	r.Model = a.capability.OpenAIModel()
	a.action = "responses"
	return r, nil
}

// responseUsage only uses upstream usage, never converts media counts to tokens.
func responseUsage(data []byte) (dto.Usage, bool, error) {
	var envelope struct {
		UsageMetadata *dto.GeminiUsageMetadata `json:"usageMetadata"`
		Usage         json.RawMessage          `json:"usage"`
		Message       struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &envelope); err != nil {
		return dto.Usage{}, false, err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return dto.Usage{}, false, fmt.Errorf("Vertex upstream error: %s", envelope.Error)
	}
	if envelope.UsageMetadata != nil {
		return gemini.UsageFromMetadata(*envelope.UsageMetadata), true, nil
	}
	if len(envelope.Usage) == 0 {
		envelope.Usage = envelope.Message.Usage
	}
	if len(envelope.Usage) > 0 && string(envelope.Usage) != "null" {
		var usage dto.Usage
		if err := common.Unmarshal(envelope.Usage, &usage); err != nil {
			return usage, false, err
		}
		var anthropic struct {
			Input       int `json:"input_tokens"`
			Output      int `json:"output_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
		}
		_ = common.Unmarshal(envelope.Usage, &anthropic)
		if usage.PromptTokens == 0 {
			usage.PromptTokens = anthropic.Input
		}
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = anthropic.Output
		}
		usage.PromptTokensDetails.CachedTokens = max(usage.PromptTokensDetails.CachedTokens, anthropic.CacheRead)
		usage.PromptTokensDetails.CachedCreationTokens = max(usage.PromptTokensDetails.CachedCreationTokens, anthropic.CacheCreate)
		if usage.TotalTokens == 0 {
			usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		}
		return usage, true, nil
	}
	return dto.Usage{}, false, nil
}

func (a *Adaptor) nativeResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if a.responseKind == "embedding" {
		return a.embeddingResponse(c, resp, info)
	}
	if info.IsStream {
		return a.nativeStream(c, resp, info)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse(err)
	}
	usage, _, err := responseUsage(data)
	if err != nil {
		return nil, badResponse(err)
	}
	if vertexmodel.IsTokenTool(a.action) {
		var fields map[string]json.RawMessage
		_ = common.Unmarshal(data, &fields)
		key := "totalTokens"
		if a.action == "computeTokens" {
			key = "tokensInfo"
		}
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return nil, badResponse(fmt.Errorf("Vertex %s response is missing %s", a.action, key))
		}
	}
	if a.action == "predict" {
		var p struct {
			Predictions []map[string]json.RawMessage `json:"predictions"`
		}
		if err := common.Unmarshal(data, &p); err != nil {
			return nil, badResponse(err)
		}
		count := 0
		for _, prediction := range p.Predictions {
			if len(prediction["raiFilteredReason"]) > 0 && string(prediction["raiFilteredReason"]) != `""` {
				continue
			}
			if len(prediction) == 0 {
				continue
			}
			if a.capability.Family == vertexmodel.Imagen && len(prediction["bytesBase64Encoded"]) <= 2 && len(prediction["gcsUri"]) <= 2 {
				continue
			}
			if a.capability.Family == vertexmodel.Music && len(prediction["audioContent"]) <= 2 && len(prediction["bytesBase64Encoded"]) <= 2 && len(prediction["gcsUri"]) <= 2 {
				continue
			}
			count++
			var emb struct {
				Statistics struct {
					TokenCount int `json:"token_count"`
				} `json:"statistics"`
			}
			_ = common.Unmarshal(prediction["embeddings"], &emb)
			usage.PromptTokens += emb.Statistics.TokenCount
			usage.TotalTokens += emb.Statistics.TokenCount
		}
		if count == 0 {
			return nil, badResponse(errors.New("Vertex returned no usable predictions"))
		}
		if a.capability.Family == vertexmodel.Imagen || a.capability.Family == vertexmodel.Music {
			info.PriceData.AddOtherRatio("n", float64(count))
		}
	}
	info.NonTokenUsage = a.RequiresCallPrice()
	if a.capability.Family == vertexmodel.Claude {
		usage.UsageSemantic = "anthropic"
	}
	c.Data(http.StatusOK, "application/json", data)
	return &usage, nil
}

func (a *Adaptor) nativeStream(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64<<10), 64<<20)
	usage := dto.Usage{}
	events := 0
	var streamErr *types.NewAPIError
	helper.SetEventStreamHeaders(c)
	for scanner.Scan() {
		if err := c.Request.Context().Err(); err != nil {
			return &usage, badResponse(err)
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "" && data != "[DONE]" {
				events++
				u, present, err := responseUsage([]byte(data))
				if err != nil {
					streamErr = badResponse(err)
				} else if present {
					// Anthropic emits input and output usage in separate events.
					if a.capability.Family == vertexmodel.Claude {
						usage.UsageSemantic = "anthropic"
						usage.PromptTokens = max(usage.PromptTokens, u.PromptTokens)
						usage.CompletionTokens = max(usage.CompletionTokens, u.CompletionTokens)
						usage.PromptTokensDetails.CachedTokens = max(usage.PromptTokensDetails.CachedTokens, u.PromptTokensDetails.CachedTokens)
						usage.PromptTokensDetails.CachedCreationTokens = max(usage.PromptTokensDetails.CachedCreationTokens, u.PromptTokensDetails.CachedCreationTokens)
						usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
					} else {
						usage = u
					}
				}
			}
		}
		if _, err := io.WriteString(c.Writer, line+"\n"); err != nil {
			return &usage, badResponse(err)
		}
		if line == "" {
			c.Writer.Flush()
		}
		info.SendResponseCount++
		if streamErr != nil {
			_, _ = io.WriteString(c.Writer, "\n")
			c.Writer.Flush()
			break
		}
	}
	if scanner.Err() != nil {
		return &usage, badResponse(scanner.Err())
	}
	if events == 0 {
		return &usage, badResponse(errors.New("Vertex returned an empty event stream"))
	}
	return &usage, streamErr
}
