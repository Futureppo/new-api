package constant

import "strings"

const QianwenIntentModel = "tongyi-intent-detect-v3"

// The documented intent model uses Chat Completions and system-prompt modes.
func IsQianwenIntentModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), QianwenIntentModel)
}
