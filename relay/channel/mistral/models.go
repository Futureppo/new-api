package mistral

import "strings"

func IsTranscriptionModel(model string) bool {
	model = strings.ToLower(model)
	return strings.HasPrefix(model, "voxtral-mini-") && !strings.Contains(model, "realtime") && !strings.Contains(model, "tts")
}
