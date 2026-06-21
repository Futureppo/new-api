package common

import "github.com/QuantumNous/new-api/constant"

// GetEndpointTypesByChannelType returns the preferred endpoint types for a channel/model pair.
func GetEndpointTypesByChannelType(channelType int, modelName string) []constant.EndpointType {
	var endpointTypes []constant.EndpointType
	switch channelType {
	case constant.ChannelTypeJina:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeJinaRerank}
	case constant.ChannelTypeCohere:
		if IsCohereRerankModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeCohereRerank}
		} else if IsCohereEmbeddingModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeCohereEmbeddings}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeCohereChat}
		}
	case constant.ChannelTypeAws:
		fallthrough
	case constant.ChannelTypeAnthropic:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
	case constant.ChannelTypeVertexAi:
		fallthrough
	case constant.ChannelTypeGemini:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
	case constant.ChannelTypeOpenRouter:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
	case constant.ChannelTypeXai:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypePoe:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeSora:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
	case constant.ChannelTypeVolcEngine:
		if IsVolcEngineContentGenerationTaskModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		} else if IsVolcEngineEmbeddingModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeEmbeddings}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	case constant.ChannelTypeGCP:
		if IsGCPSpeechModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeAudioSpeech}
		} else if IsGCPTranscriptionModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeAudioTranscription}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeAudioSpeech, constant.EndpointTypeAudioTranscription}
		}
	default:
		if IsOpenAIResponseOnlyModel(modelName) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIResponse}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	}
	if channelType != constant.ChannelTypePoe && (IsImageGenerationModel(modelName) || (channelType == constant.ChannelTypeVolcEngine && IsVolcEngineImageGenerationModel(modelName))) {
		endpointTypes = append([]constant.EndpointType{constant.EndpointTypeImageGeneration}, endpointTypes...)
	}
	if channelType != constant.ChannelTypePoe && (IsVideoGenerationModel(modelName) || (channelType == constant.ChannelTypeVolcEngine && IsVolcEngineContentGenerationTaskModel(modelName))) {
		endpointTypes = prependEndpointType(endpointTypes, constant.EndpointTypeOpenAIVideo)
	}
	return endpointTypes
}

func prependEndpointType(endpointTypes []constant.EndpointType, endpointType constant.EndpointType) []constant.EndpointType {
	for _, et := range endpointTypes {
		if et == endpointType {
			return endpointTypes
		}
	}
	return append([]constant.EndpointType{endpointType}, endpointTypes...)
}
