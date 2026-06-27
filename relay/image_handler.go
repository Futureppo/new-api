package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var requestBody io.Reader

	if shouldPassThroughImageRequest(info) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

			if common.DebugEnabled {
				logger.LogDebug(c, fmt.Sprintf("image request body: %s", string(jsonData)))
			}
			requestBody = bytes.NewBuffer(jsonData)
		}
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	imageN := uint(1)
	if request.N != nil {
		imageN = *request.N
	}

	// n is handled via OtherRatio so it is applied exactly once in quota
	// calculation (both price-based and ratio-based paths).
	// Adaptors may have already set a more accurate count from the
	// upstream response; only set the default when they haven't.
	if info.PriceData.UsePrice { // only price model use N ratio
		if _, hasN := info.PriceData.OtherRatios["n"]; !hasN {
			info.PriceData.AddOtherRatio("n", float64(imageN))
		}
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	quality := "standard"
	if request.Quality == "hd" {
		quality = "hd"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	recordOpenAILocalImageTask(c, info, request, imageN)
	return nil
}

type openAILocalImageTaskResult struct {
	URL           string `json:"url,omitempty"`
	HasB64JSON    bool   `json:"has_b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type openAILocalImageTaskData struct {
	ID             string                       `json:"id,omitempty"`
	Model          string                       `json:"model,omitempty"`
	Prompt         string                       `json:"prompt,omitempty"`
	Size           string                       `json:"size,omitempty"`
	Quality        string                       `json:"quality,omitempty"`
	N              uint                         `json:"n,omitempty"`
	ResponseFormat string                       `json:"response_format,omitempty"`
	Data           []openAILocalImageTaskResult `json:"data,omitempty"`
}

func recordOpenAILocalImageTask(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, imageN uint) {
	if info == nil || request == nil || info.ChannelType != constant.ChannelTypeOpenAILocal {
		return
	}
	raw, ok := c.Get("openai_response_body")
	if !ok {
		return
	}
	responseBody, ok := raw.([]byte)
	if !ok || len(responseBody) == 0 {
		return
	}

	var payload struct {
		ID   string `json:"id"`
		Data []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	_ = common.Unmarshal(responseBody, &payload)

	taskData := openAILocalImageTaskData{
		ID:             payload.ID,
		Model:          request.Model,
		Prompt:         request.Prompt,
		Size:           request.Size,
		Quality:        request.Quality,
		N:              imageN,
		ResponseFormat: request.ResponseFormat,
		Data:           make([]openAILocalImageTaskResult, 0, len(payload.Data)),
	}

	resultURL := ""
	for _, item := range payload.Data {
		if resultURL == "" && item.URL != "" {
			resultURL = item.URL
		}
		taskData.Data = append(taskData.Data, openAILocalImageTaskResult{
			URL:           item.URL,
			HasB64JSON:    item.B64JSON != "",
			RevisedPrompt: item.RevisedPrompt,
		})
	}

	task := model.InitTask(constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAILocal)), info)
	now := time.Now().Unix()
	task.Action = constant.TaskActionImageGeneration
	if info.RelayMode == relayconstant.RelayModeImagesEdits {
		task.Action = constant.TaskActionImageEdit
	}
	task.Status = model.TaskStatusSuccess
	task.Progress = "100%"
	task.SubmitTime = now
	task.StartTime = now
	task.FinishTime = now
	task.Quota = info.PriceData.Quota
	task.PrivateData.ResultURL = resultURL
	task.SetData(taskData)
	if err := task.Insert(); err != nil {
		common.SysError("insert OpenAI-local image task error: " + err.Error())
	}
}

func shouldPassThroughImageRequest(info *relaycommon.RelayInfo) bool {
	if info != nil && info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeAgnesAI {
		return false
	}
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	return info != nil && info.ChannelSetting.PassThroughBodyEnabled
}
