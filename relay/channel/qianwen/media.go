package qianwen

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const (
	multimodalPath = "/api/v1/services/aigc/multimodal-generation/generation"
	speechPath     = "/api/v1/services/audio/tts/SpeechSynthesizer"
	musicPath      = "/api/v1/services/audio/music/generation"
	videoPath      = "/api/v1/services/aigc/video-generation/video-synthesis"
	rerankPath     = "/api/v1/services/rerank/text-rerank/text-rerank"
)

func compatibleImage(model string) bool {
	return model == "qwen-image-3.0" || model == "qwen-image-3.0-pro" || model == "qwen-image-2.1-pro"
}

// All optional provider parameters remain raw JSON or pointers so explicit
// false/zero values survive the gateway's copy and conversion stages.
func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if !info.PriceData.UsePrice && !strings.HasPrefix(request.Model, "wan2.7-image") {
		return nil, invalid("configure QianwenAI image unit price per image; this model does not report token usage")
	}
	if request.Stream != nil && *request.Stream {
		return nil, invalid("QianwenAI synchronous images do not support streaming")
	}
	if len(request.Mask) != 0 && string(request.Mask) != "null" {
		return nil, invalid("QianwenAI images do not document a mask parameter; use image and prompt for editing")
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "url" && request.ResponseFormat != "b64_json" {
		return nil, invalid("image response_format must be url or b64_json")
	}
	c.Set("qianwen_image_format", request.ResponseFormat)
	if info.RelayMode == relayconstant.RelayModeImagesEdits && strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/") {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		defer form.RemoveAll()
		if len(form.File["mask"]) != 0 {
			return nil, invalid("QianwenAI images do not support mask uploads")
		}
		files := form.File["image"]
		if len(files) == 0 {
			files = form.File["image[]"]
		}
		var images []string
		for _, header := range files {
			file, err := header.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(io.LimitReader(file, 10*1024*1024+1))
			file.Close()
			if err != nil {
				return nil, err
			}
			if len(data) > 10*1024*1024 {
				return nil, invalid("QianwenAI input image exceeds 10 MB")
			}
			images = append(images, "data:"+http.DetectContentType(data)+";base64,"+base64.StdEncoding.EncodeToString(data))
		}
		if len(images) == 0 {
			return nil, invalid("image is required")
		}
		request.Image, err = common.Marshal(images)
		if err != nil {
			return nil, err
		}
	}
	if info.RelayMode == relayconstant.RelayModeImagesEdits && len(request.Image) == 0 {
		return nil, invalid("image is required")
	}
	params := map[string]any{}
	if raw := request.Extra["parameters"]; len(raw) > 0 {
		if err := common.Unmarshal(raw, &params); err != nil {
			return nil, invalid("invalid image parameters")
		}
		if params == nil {
			return nil, invalid("image parameters must be an object")
		}
	}
	if request.N != nil {
		params["n"] = request.N
	}
	if request.Size != "" {
		params["size"] = request.Size
	}
	if request.Seed != nil {
		params["seed"] = request.Seed
	}
	if request.Watermark != nil {
		params["watermark"] = request.Watermark
	}
	if request.PromptExtend != nil {
		params["prompt_extend"] = request.PromptExtend
	}
	if request.EnableThinking != nil {
		params["enable_thinking"] = request.EnableThinking
	}
	if request.PromptExtendMode != "" {
		params["prompt_extend_mode"] = request.PromptExtendMode
	}
	if request.NegativePrompt != "" {
		params["negative_prompt"] = request.NegativePrompt
	}
	if compatibleImage(request.Model) {
		params["model"], params["prompt"] = request.Model, request.Prompt
		if len(request.Image) > 0 {
			params["image"] = request.Image
		}
		return params, nil
	}
	if size, ok := params["size"].(string); ok {
		params["size"] = strings.ReplaceAll(size, "x", "*")
	}
	content := []map[string]any{}
	if len(request.Image) > 0 {
		var images []string
		if common.GetJsonType(request.Image) == "string" {
			var value string
			if err := common.Unmarshal(request.Image, &value); err != nil {
				return nil, err
			}
			images = []string{value}
		} else if err := common.Unmarshal(request.Image, &images); err != nil {
			return nil, invalid("image must be a URL/data URI or an array of them")
		}
		if len(images) == 0 {
			return nil, invalid("image must not be empty")
		}
		for _, value := range images {
			content = append(content, map[string]any{"image": value})
		}
	}
	content = append(content, map[string]any{"text": request.Prompt})
	input := any(map[string]any{"messages": []any{map[string]any{"role": "user", "content": content}}})
	if raw := request.Extra["input"]; len(raw) > 0 {
		input = raw
	}
	return map[string]any{"model": request.Model, "input": input, "parameters": params}, nil
}

type nativeError struct {
	Code    string             `json:"code"`
	Message string             `json:"message"`
	Error   *types.OpenAIError `json:"error"`
}

func (e nativeError) apiError() *types.NewAPIError {
	if e.Error != nil && e.Error.Message != "" {
		return types.WithOpenAIError(*e.Error, http.StatusBadGateway)
	}
	if e.Code != "" {
		return types.WithOpenAIError(types.OpenAIError{Code: e.Code, Type: "upstream_error", Message: e.Message}, http.StatusBadGateway)
	}
	return nil
}

func readJSONResponse(resp *http.Response, target any) ([]byte, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	data, err := io.ReadAll(resp.Body)
	if err == nil {
		err = common.Unmarshal(data, target)
	}
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	var upstreamError nativeError
	if err = common.Unmarshal(data, &upstreamError); err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	return data, upstreamError.apiError()
}

func imageResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var result struct {
		Data   []map[string]any `json:"data"`
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
		Usage json.RawMessage `json:"usage"`
	}
	data, apiErr := readJSONResponse(resp, &result)
	if apiErr != nil {
		return nil, apiErr
	}
	for _, choice := range result.Output.Choices {
		for _, content := range choice.Message.Content {
			if content.Image != "" {
				result.Data = append(result.Data, map[string]any{"url": content.Image})
			}
		}
	}
	if len(result.Data) == 0 {
		return nil, types.NewError(fmt.Errorf("QianwenAI returned no generated images"), types.ErrorCodeBadResponseBody)
	}
	if c.GetString("qianwen_image_format") == "b64_json" {
		for _, item := range result.Data {
			if address, _ := item["url"].(string); address != "" {
				encoded, _, err := service.GetBase64Data(c, types.NewURLFileSource(address), "QianwenAI generated image")
				if err != nil {
					return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
				}
				item["b64_json"] = encoded
				delete(item, "url")
			}
		}
	}
	if !compatibleImage(info.UpstreamModelName) || c.GetString("qianwen_image_format") == "b64_json" {
		var err error
		data, err = common.Marshal(map[string]any{"created": common.GetTimestamp(), "data": result.Data, "usage": result.Usage})
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
	}
	if info.PriceData.UsePrice {
		info.PriceData.AddOtherRatio("n", float64(len(result.Data)))
		info.NonTokenUsage = true
	}
	usage := &dto.Usage{}
	if len(result.Usage) != 0 {
		if err := common.Unmarshal(result.Usage, usage); err != nil {
			return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
		}
		if usage.PromptTokens == 0 {
			usage.PromptTokens = usage.InputTokens
		}
		if usage.CompletionTokens == 0 {
			usage.CompletionTokens = usage.OutputTokens
		}
	}
	if !info.PriceData.UsePrice && usage.PromptTokens+usage.CompletionTokens <= 0 {
		return nil, types.NewError(fmt.Errorf("QianwenAI image response is missing token usage"), types.ErrorCodeBadResponseBody)
	}
	service.IOCopyBytesGracefully(c, resp, data)
	return usage, nil
}

func (a *Adaptor) ConvertRerankRequest(_ *gin.Context, _ int, request dto.RerankRequest) (any, error) {
	if len(request.QueryObject) > 0 && request.Model != "qwen3-vl-rerank" {
		return nil, invalid("query objects require qwen3-vl-rerank")
	}
	if request.Model == "qwen3-rerank" {
		return request, nil
	}
	params := map[string]any{}
	if request.TopN != nil {
		params["top_n"] = request.TopN
	}
	if request.ReturnDocuments != nil {
		params["return_documents"] = request.ReturnDocuments
	}
	if request.Instruct != "" {
		params["instruct"] = request.Instruct
	}
	if request.FPS != nil {
		params["fps"] = request.FPS
	}
	query := any(request.Query)
	if len(request.QueryObject) > 0 {
		query = request.QueryObject
	}
	return map[string]any{"model": request.Model, "input": map[string]any{"query": query, "documents": request.Documents}, "parameters": params}, nil
}

func rerankResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	var result struct {
		dto.RerankResponse
		Output struct {
			Results []dto.RerankResponseResult `json:"results"`
		} `json:"output"`
	}
	_, apiErr := readJSONResponse(resp, &result)
	if apiErr != nil {
		return nil, apiErr
	}
	if info.UpstreamModelName != "qwen3-rerank" {
		result.Results = result.Output.Results
	}
	if result.Results == nil {
		return nil, types.NewError(fmt.Errorf("QianwenAI returned no rerank results"), types.ErrorCodeBadResponseBody)
	}
	result.Usage.PromptTokens = result.Usage.TotalTokens
	c.JSON(http.StatusOK, result.RerankResponse)
	return &result.Usage, nil
}
