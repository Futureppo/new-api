package constant

import "strings"

type OpenCodeEndpoint string

const (
	OpenCodeEndpointChat      OpenCodeEndpoint = "chat_completions"
	OpenCodeEndpointResponses OpenCodeEndpoint = "responses"
	OpenCodeEndpointMessages  OpenCodeEndpoint = "messages"
	OpenCodeEndpointGemini    OpenCodeEndpoint = "gemini"
)

// GetOpenCodeEndpoint returns the protocol required by OpenCode Zen for a model.
// Zen exposes one model catalog across several provider-compatible endpoints.
func GetOpenCodeEndpoint(model string) OpenCodeEndpoint {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, "gpt-"), strings.HasPrefix(model, "grok-"):
		return OpenCodeEndpointResponses
	case strings.HasPrefix(model, "claude-"), strings.HasPrefix(model, "qwen"):
		return OpenCodeEndpointMessages
	case strings.HasPrefix(model, "gemini-"):
		return OpenCodeEndpointGemini
	default:
		return OpenCodeEndpointChat
	}
}

// GetOpenCodeGoEndpoint returns the protocol required by OpenCode Go for a
// model. Go's routing differs from Zen: Grok uses Chat Completions, while
// MiniMax uses Messages.
func GetOpenCodeGoEndpoint(model string) OpenCodeEndpoint {
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(model, "gpt-"):
		return OpenCodeEndpointResponses
	case strings.HasPrefix(model, "minimax-"), strings.HasPrefix(model, "qwen"):
		return OpenCodeEndpointMessages
	default:
		return OpenCodeEndpointChat
	}
}
