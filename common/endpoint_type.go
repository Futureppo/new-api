package common

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/openaimodel"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
)

// GetEndpointTypesByChannelType returns the preferred endpoint types for a channel/model pair.
func GetEndpointTypesByChannelType(channelType int, modelName string) []constant.EndpointType {
	if channelType == constant.ChannelTypeMistral && strings.HasPrefix(strings.ToLower(modelName), "mistral-moderation-") {
		return []constant.EndpointType{constant.EndpointTypeModerations}
	}
	var endpointTypes []constant.EndpointType
	switch channelType {
	case constant.ChannelTypeQianwenAI:
		if constant.IsQianwenDecisionModel(modelName) {
			return []constant.EndpointType{constant.EndpointTypeTypeSafeSystemOne}
		}
		if constant.IsQianwenIntentModel(modelName) {
			return []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
		name := strings.ToLower(modelName)
		switch {
		case strings.Contains(name, "embedding"):
			return []constant.EndpointType{constant.EndpointTypeEmbeddings}
		case strings.Contains(name, "rerank"):
			return []constant.EndpointType{constant.EndpointTypeJinaRerank}
		case strings.Contains(name, "tts") || strings.HasPrefix(name, "cosyvoice") || strings.HasPrefix(name, "fun-music-"):
			return []constant.EndpointType{constant.EndpointTypeAudioSpeech}
		case strings.HasPrefix(name, "wan3.") || strings.Contains(name, "-t2v") || strings.Contains(name, "-i2v") || strings.Contains(name, "-r2v"):
			return []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		case strings.HasPrefix(name, "qwen-image") || strings.Contains(name, "wan2.7-image") || strings.Contains(name, "wan2.6-image") || name == "z-image-turbo":
			return []constant.EndpointType{constant.EndpointTypeImageGeneration}
		}
		return []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeVLLM, constant.ChannelTypeLiteLLM:
		return []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeXunfeiMaas:
		name := strings.ToLower(modelName)
		if strings.Contains(name, "embedding") {
			return []constant.EndpointType{constant.EndpointTypeEmbeddings}
		}
		if strings.Contains(name, "rerank") {
			return []constant.EndpointType{constant.EndpointTypeJinaRerank}
		}
		if name == "xopzimageturbo" || name == "xopqwentti20b" || IsImageGenerationModel(modelName) || strings.Contains(name, "kolors") || strings.Contains(name, "flux") {
			return []constant.EndpointType{constant.EndpointTypeImageGeneration}
		}
		switch name {
		case "xop3qwen32bvl", "xopkimik27code", "xopkimik26", "xopkimik25",
			"xopqwen36v35b", "xopqwen35397b", "xoppaddleocrv16", "xophunyuanocr", "xopdeepseekocr":
			return []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
		return []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic}
	case constant.ChannelTypeMiMo:
		return []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic}
	case constant.ChannelTypeTypeSafe:
		return []constant.EndpointType{constant.EndpointTypeTypeSafeSystemOne}
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
		cap, err := vertexmodel.Resolve(modelName)
		if err != nil {
			return nil
		}
		switch cap.Family {
		case vertexmodel.Embedding, vertexmodel.MultimodalEmbedding:
			return []constant.EndpointType{constant.EndpointTypeEmbeddings, constant.EndpointTypeVertexNative}
		case vertexmodel.Imagen, vertexmodel.GeminiImage:
			return []constant.EndpointType{constant.EndpointTypeImageGeneration, constant.EndpointTypeVertexNative}
		case vertexmodel.Speech:
			return []constant.EndpointType{constant.EndpointTypeAudioSpeech, constant.EndpointTypeVertexNative}
		case vertexmodel.Transcribe:
			return []constant.EndpointType{constant.EndpointTypeAudioTranscription, constant.EndpointTypeVertexNative}
		case vertexmodel.Video:
			return []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		case vertexmodel.Claude:
			return []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
		case vertexmodel.Grok:
			return []constant.EndpointType{constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAI}
		case vertexmodel.FIM:
			return []constant.EndpointType{constant.EndpointTypeVertexNative}
		case vertexmodel.OpenAPI, vertexmodel.PartnerChat:
			return []constant.EndpointType{constant.EndpointTypeOpenAI}
		case vertexmodel.OCR, vertexmodel.Music:
			return []constant.EndpointType{constant.EndpointTypeVertexNative}
		default:
			return []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
		}
	case constant.ChannelTypeGemini:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
	case constant.ChannelTypeOpenRouter:
		return openRouterEndpointTypes(modelName)
	case constant.ChannelTypeModal, constant.ChannelTypeKilo, constant.ChannelTypeCline:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
	case constant.ChannelTypeGMICloud:
		if strings.TrimSpace(modelName) == "hy-image-v3.5-preview" {
			return []constant.EndpointType{constant.EndpointTypeImageGeneration}
		} else if strings.EqualFold(strings.TrimSpace(modelName), "Gemini-batch-inference") {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeBatchGeneration}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	case constant.ChannelTypeVercel:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeVyceAI:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeImageGeneration}
	case constant.ChannelTypeXai:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypePoe:
		endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeOpenCode:
		switch constant.GetOpenCodeEndpoint(modelName) {
		case constant.OpenCodeEndpointResponses:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAI}
		case constant.OpenCodeEndpointMessages:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
		case constant.OpenCodeEndpointGemini:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
		default:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	case constant.ChannelTypeOpenCodeGo:
		switch constant.GetOpenCodeGoEndpoint(modelName) {
		case constant.OpenCodeEndpointResponses:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIResponse, constant.EndpointTypeOpenAI}
		case constant.OpenCodeEndpointMessages:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
		default:
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
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
		_, _, capabilities, known := openaimodel.Resolve(modelName)
		if known && (capabilities.LegacyResponsesOnly || (channelType == constant.ChannelTypeOpenAI && capabilities.ResponsesOnly)) {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAIResponse}
		} else {
			endpointTypes = []constant.EndpointType{constant.EndpointTypeOpenAI}
		}
	}
	if channelType != constant.ChannelTypePoe && (IsImageGenerationModel(modelName) || (channelType == constant.ChannelTypeVolcEngine && IsVolcEngineImageGenerationModel(modelName))) {
		endpointTypes = append([]constant.EndpointType{constant.EndpointTypeImageGeneration}, endpointTypes...)
	}
	if channelType != constant.ChannelTypePoe && channelType != constant.ChannelTypeOpenRouter && (IsVideoGenerationModel(modelName) || (channelType == constant.ChannelTypeVolcEngine && IsVolcEngineContentGenerationTaskModel(modelName))) {
		endpointTypes = prependEndpointType(endpointTypes, constant.EndpointTypeOpenAIVideo)
	}
	if channelType == constant.ChannelTypeOpenAI && modelName == "gpt-6-luna" {
		endpointTypes = append(endpointTypes, constant.EndpointTypeOpenAIDecisions)
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
