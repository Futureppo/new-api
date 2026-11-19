package cohere

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	projectconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	//TODO implement me
	panic("implement me")
	return nil, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	switch info.RelayMode {
	case constant.RelayModeRerank:
		return fmt.Sprintf("%s/v2/rerank", info.ChannelBaseUrl), nil
	case constant.RelayModeEmbeddings:
		return fmt.Sprintf("%s/v2/embed", info.ChannelBaseUrl), nil
	default:
		return fmt.Sprintf("%s/v2/chat", info.ChannelBaseUrl), nil
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", fmt.Sprintf("Bearer %s", info.ApiKey))
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if raw, ok := common.GetContextKeyType[[]byte](c, projectconstant.ContextKeyPlaygroundNativeBody); ok {
		return mappedNativeBody(raw, request.Model)
	}
	return requestOpenAI2Cohere(*request)
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	// TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	if raw, ok := common.GetContextKeyType[[]byte](c, projectconstant.ContextKeyPlaygroundNativeBody); ok {
		return mappedNativeBody(raw, request.Model)
	}
	return requestConvertRerank2Cohere(request), nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	if raw, ok := common.GetContextKeyType[[]byte](c, projectconstant.ContextKeyPlaygroundNativeBody); ok {
		return mappedNativeBody(raw, request.Model)
	}
	if attachment := relaycommon.PlaygroundEmbeddingImages(c); len(attachment.Images) > 0 {
		var inputs []CohereEmbeddingInput
		for _, input := range attachment.Inputs(request.ParseInput(), attachment.Mode == "separate") {
			inputs = append(inputs, CohereEmbeddingInput{Content: input.MediaParts()})
		}
		if len(inputs) > 96 {
			return nil, fmt.Errorf("Cohere accepts at most 96 embedding inputs")
		}
		return &CohereEmbeddingRequest{
			Model:           request.Model,
			Inputs:          inputs,
			InputType:       request.InputType,
			EmbeddingTypes:  []string{"float"},
			OutputDimension: request.Dimensions,
			Truncate:        request.Truncate,
		}, nil
	}
	return requestConvertEmbedding2Cohere(request)
}

func mappedNativeBody(raw []byte, model string) (any, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	modelJSON, err := common.Marshal(model)
	if err != nil {
		return nil, err
	}
	fields["model"] = modelJSON
	return fields, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayMode == constant.RelayModeEmbeddings {
		usage, err = cohereEmbeddingHandler(c, resp, info)
	} else if info.RelayMode == constant.RelayModeRerank {
		usage, err = cohereRerankHandler(c, resp, info)
	} else {
		if info.IsStream {
			usage, err = cohereStreamHandler(c, info, resp) // TODO: fix this
		} else {
			usage, err = cohereHandler(c, info, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
