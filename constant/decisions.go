package constant

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
