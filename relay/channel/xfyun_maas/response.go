package xfyun_maas

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func badResponse(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}

func readBody(resp *http.Response) ([]byte, error) {
	defer service.CloseResponseBodyGracefully(resp)
	const limit = 64 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && len(b) > limit {
		err = fmt.Errorf("Xunfei MaaS response exceeds 64 MiB")
	}
	return b, err
}

func replaceBody(resp *http.Response, data []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	resp.Header.Del("Content-Length")
}

func businessError(data []byte, status int) *types.NewAPIError {
	var envelope struct {
		Code    json.RawMessage    `json:"code"`
		Message string             `json:"message"`
		Error   *types.OpenAIError `json:"error"`
		Header  *struct {
			Code    json.RawMessage `json:"code"`
			Message string          `json:"message"`
			SID     string          `json:"sid"`
		} `json:"header"`
	}
	if common.Unmarshal(data, &envelope) != nil {
		return nil
	}
	if envelope.Error != nil {
		if status < 400 {
			status = http.StatusBadGateway
		}
		if envelope.Error.Type == "" {
			envelope.Error.Type = "xfyun_maas_error"
		}
		return types.WithOpenAIError(*envelope.Error, status)
	}
	raw, message := envelope.Code, envelope.Message
	if envelope.Header != nil {
		raw, message = envelope.Header.Code, envelope.Header.Message
		if envelope.Header.SID != "" {
			message += " (sid: " + envelope.Header.SID + ")"
		}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	code := string(raw)
	if raw[0] == '"' {
		if common.Unmarshal(raw, &code) != nil {
			return badResponse(fmt.Errorf("invalid upstream error code"))
		}
	}
	if code == "" || code == "0" {
		return nil
	}
	if status < 400 {
		switch code {
		case "10004", "10005", "10013", "10014", "10163", "10224", "10404", "10305", "10313", "10314":
			status = http.StatusBadRequest
		case "11200", "11221":
			status = http.StatusForbidden
		case "11201", "11202", "11203", "11210":
			status = http.StatusTooManyRequests
		case "10110", "10010", "10310":
			status = http.StatusServiceUnavailable
		default:
			status = http.StatusBadGateway
		}
	}
	if message == "" {
		message = "Xunfei MaaS business error " + code
	}
	return types.WithOpenAIError(types.OpenAIError{Type: "xfyun_maas_error", Code: code, Message: message}, status)
}

// Only protocol envelopes are normalized. Tool arguments and user content must
// never be recursively rewritten just because they contain a model_id key.
func normalizeEnvelope(data []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := common.Unmarshal(data, &m); err != nil || m == nil {
		return nil, fmt.Errorf("invalid Xunfei MaaS response object")
	}
	if model, ok := m["model_id"]; ok {
		if len(m["model"]) == 0 || string(m["model"]) == "null" || string(m["model"]) == `""` {
			m["model"] = model
		}
		delete(m, "model_id")
	}
	for _, key := range []string{"response", "message"} {
		if value := m[key]; len(value) > 0 && value[0] == '{' {
			normalized, err := normalizeEnvelope(value)
			if err != nil {
				return nil, err
			}
			m[key] = normalized
		}
	}
	if raw := m["usage"]; len(raw) > 0 && string(raw) != "null" {
		var usage map[string]json.RawMessage
		if err := common.Unmarshal(raw, &usage); err != nil {
			return nil, err
		}
		if cached, ok := usage["cache_read_input_tokens"]; ok {
			var n int
			if err := common.Unmarshal(cached, &n); err != nil {
				return nil, err
			}
			if n < 0 {
				usage["cache_read_input_tokens"] = json.RawMessage("0")
			}
		}
		var err error
		m["usage"], err = common.Marshal(usage)
		if err != nil {
			return nil, err
		}
	}
	return common.Marshal(m)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, badResponse(fmt.Errorf("empty upstream response"))
	}
	if info.IsStream && strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return streamResponse(c, resp, info)
	}
	data, err := readBody(resp)
	if err != nil {
		return nil, badResponse(err)
	}
	if apiErr := businessError(data, resp.StatusCode); apiErr != nil {
		return nil, apiErr
	}
	if info.IsStream {
		return nil, badResponse(fmt.Errorf("expected an SSE response from Xunfei MaaS"))
	}
	data, err = normalizeEnvelope(data)
	if err != nil {
		return nil, badResponse(err)
	}
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations:
		return imageResponse(c, info, data)
	case relayconstant.RelayModeEmbeddings:
		return a.embeddingResponse(c, data)
	case relayconstant.RelayModeRerank:
		return a.rerankResponse(c, data)
	}
	replaceBody(resp, data)
	if info.RelayFormat == types.RelayFormatClaude {
		var result dto.ClaudeResponse
		if err := common.Unmarshal(data, &result); err != nil || len(result.Content) == 0 {
			return nil, badResponse(fmt.Errorf("empty Messages result"))
		}
		return claude.ClaudeHandler(c, resp, info)
	}
	if info.RelayMode == relayconstant.RelayModeResponses {
		var result struct {
			Status string          `json:"status"`
			Output json.RawMessage `json:"output"`
		}
		if err := common.Unmarshal(data, &result); err != nil {
			return nil, badResponse(err)
		}
		if result.Status == "failed" || len(result.Output) == 0 {
			return nil, badResponse(fmt.Errorf("failed or empty Responses result"))
		}
		return openai.OaiResponsesHandler(c, info, resp)
	}
	var result dto.OpenAITextResponse
	if err := common.Unmarshal(data, &result); err != nil || len(result.Choices) == 0 {
		return nil, badResponse(fmt.Errorf("empty Chat Completions result"))
	}
	return openai.OpenaiHandler(c, info, resp)
}

func writeJSON(c *gin.Context, v any) *types.NewAPIError {
	data, err := common.Marshal(v)
	if err != nil {
		return badResponse(err)
	}
	c.Header("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, data)
	return nil
}

func (a *Adaptor) embeddingResponse(c *gin.Context, data []byte) (*dto.Usage, *types.NewAPIError) {
	var response struct {
		Object string `json:"object"`
		Model  string `json:"model"`
		Data   []struct {
			Object    string    `json:"object"`
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Usage *dto.Usage `json:"usage"`
	}
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, badResponse(err)
	}
	if len(response.Data) == 0 || (a.embeddingCount > 0 && len(response.Data) != a.embeddingCount) || response.Usage == nil {
		return nil, badResponse(fmt.Errorf("embedding response is missing vectors or usage"))
	}
	items := make([]map[string]any, len(response.Data))
	seen := make(map[int]bool)
	for i, item := range response.Data {
		if item.Index < 0 || item.Index >= len(response.Data) || seen[item.Index] || len(item.Embedding) == 0 {
			return nil, badResponse(fmt.Errorf("invalid embedding result index or vector"))
		}
		seen[item.Index] = true
		var embedding any = item.Embedding
		if a.embeddingFormat == "base64" {
			b := make([]byte, 4*len(item.Embedding))
			for j, f := range item.Embedding {
				if math.Abs(f) > math.MaxFloat32 {
					return nil, badResponse(fmt.Errorf("embedding value exceeds float32 range"))
				}
				binary.LittleEndian.PutUint32(b[j*4:], math.Float32bits(float32(f)))
			}
			embedding = base64.StdEncoding.EncodeToString(b)
		}
		items[i] = map[string]any{"object": "embedding", "index": item.Index, "embedding": embedding}
	}
	if response.Usage.TotalTokens == 0 {
		response.Usage.TotalTokens = response.Usage.PromptTokens
	}
	return response.Usage, writeJSON(c, map[string]any{"object": "list", "model": response.Model, "data": items, "usage": response.Usage})
}

func (a *Adaptor) rerankResponse(c *gin.Context, data []byte) (*dto.Usage, *types.NewAPIError) {
	var required struct {
		Results []struct {
			Index *int     `json:"index"`
			Score *float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := common.Unmarshal(data, &required); err != nil {
		return nil, badResponse(err)
	}
	for _, item := range required.Results {
		if item.Index == nil || item.Score == nil {
			return nil, badResponse(fmt.Errorf("rerank result is missing index or relevance_score"))
		}
	}
	var response struct {
		Results []dto.RerankResponseResult `json:"results"`
		Usage   *dto.Usage                 `json:"usage"`
	}
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, badResponse(err)
	}
	if len(response.Results) != len(a.rerank.Documents) || response.Usage == nil {
		return nil, badResponse(fmt.Errorf("rerank response is missing results or usage"))
	}
	seen := make(map[int]bool)
	for i := range response.Results {
		item := &response.Results[i]
		if item.Index < 0 || item.Index >= len(a.rerank.Documents) || seen[item.Index] {
			return nil, badResponse(fmt.Errorf("invalid rerank result index %s", strconv.Itoa(item.Index)))
		}
		seen[item.Index] = true
		item.Document = nil
		if a.rerank.GetReturnDocuments() {
			item.Document = dto.RerankDocument{Text: a.rerank.Documents[item.Index]}
		}
	}
	sort.SliceStable(response.Results, func(i, j int) bool {
		return response.Results[i].RelevanceScore > response.Results[j].RelevanceScore
	})
	if a.rerank.TopN != nil && *a.rerank.TopN < len(response.Results) {
		response.Results = response.Results[:*a.rerank.TopN]
	}
	if response.Usage.PromptTokens == 0 {
		response.Usage.PromptTokens = response.Usage.TotalTokens
	}
	if response.Usage.TotalTokens == 0 {
		response.Usage.TotalTokens = response.Usage.PromptTokens
	}
	return response.Usage, writeJSON(c, dto.RerankResponse{Results: response.Results, Usage: *response.Usage})
}
