package common

import "github.com/QuantumNous/new-api/constant"

// EndpointInfo describes the default path and HTTP method for a built-in endpoint.
type EndpointInfo struct {
	Path   string `json:"path"`
	Method string `json:"method"`
}

var defaultEndpointInfoMap = map[constant.EndpointType]EndpointInfo{
	constant.EndpointTypeOpenAI:                {Path: "/v1/chat/completions", Method: "POST"},
	constant.EndpointTypeOpenAIResponse:        {Path: "/v1/responses", Method: "POST"},
	constant.EndpointTypeOpenAIResponseCompact: {Path: "/v1/responses/compact", Method: "POST"},
	constant.EndpointTypeAnthropic:             {Path: "/v1/messages", Method: "POST"},
	constant.EndpointTypeGemini:                {Path: "/v1beta/models/{model}:generateContent", Method: "POST"},
	constant.EndpointTypeJinaRerank:            {Path: "/v1/rerank", Method: "POST"},
	constant.EndpointTypeCohereChat:            {Path: "/v1/chat/completions", Method: "POST"},
	constant.EndpointTypeCohereRerank:          {Path: "/v1/rerank", Method: "POST"},
	constant.EndpointTypeCohereEmbeddings:      {Path: "/v1/embeddings", Method: "POST"},
	constant.EndpointTypeImageGeneration:       {Path: "/v1/images/generations", Method: "POST"},
	constant.EndpointTypeEmbeddings:            {Path: "/v1/embeddings", Method: "POST"},
	constant.EndpointTypeOpenAIVideo:           {Path: "/v1/videos", Method: "POST"},
	constant.EndpointTypeAudioSpeech:           {Path: "/v1/audio/speech", Method: "POST"},
	constant.EndpointTypeAudioTranscription:    {Path: "/v1/audio/transcriptions", Method: "POST"},
}

// GetDefaultEndpointInfo returns the built-in default endpoint metadata.
func GetDefaultEndpointInfo(et constant.EndpointType) (EndpointInfo, bool) {
	info, ok := defaultEndpointInfoMap[et]
	return info, ok
}
