package constant

import "strings"

func IsGeminiDecisionModel(model string) bool {
	return strings.HasPrefix(strings.TrimPrefix(model, "models/"), "gemini-decision-")
}

// SupportsNamedDecisions identifies channels using state and named questions.
// Model availability and OpenRouter Enterprise support are checked separately.
func SupportsNamedDecisions(channelType int) bool {
	switch channelType {
	case ChannelTypeOpenRouter, ChannelTypeTypeSafe, ChannelTypeQianwenAI:
		return true
	default:
		return false
	}
}
