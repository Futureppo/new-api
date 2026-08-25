package gmicloud

import "strings"

const (
	DefaultBaseURL   = "https://api.gmi-serving.com"
	TaskBaseURL      = "https://console.gmicloud.ai"
	TaskRequestsPath = "/api/v1/ie/requestqueue/apikey/requests"
	TaskModelsPath   = "/api/v1/ie/requestqueue/apikey/models"
)

var LLMModelList = []string{
	"MiniMaxAI/MiniMax-M3",
	"MiniMaxAI/MiniMax-M2.7",
}

var AudioModelList = []string{
	"minimax-tts-speech-2.8-turbo",
	"minimax-tts-speech-2.8-hd",
	"minimax-audio-voice-clone-speech-2.8-turbo",
	"minimax-audio-voice-clone-speech-2.8-hd",
	"minimax-music-3.0",
}

var ModelList = append(append([]string{}, LLMModelList...), AudioModelList...)

var ChannelName = "gmicloud"

func ResolveTaskBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || baseURL == DefaultBaseURL {
		return TaskBaseURL
	}
	return baseURL
}

func IsMusicModel(model string) bool {
	return strings.HasPrefix(model, "minimax-music-")
}

func IsVoiceCloneModel(model string) bool {
	return strings.Contains(model, "voice-clone")
}

func IsAudioModel(model string) bool {
	for _, supportedModel := range AudioModelList {
		if model == supportedModel {
			return true
		}
	}
	return false
}
