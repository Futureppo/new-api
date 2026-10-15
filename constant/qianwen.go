package constant

import "strings"

const QianwenIntentModel = "tongyi-intent-detect-v3"

const QianwenDecisionModel = "decision-model-preview"

func IsQianwenDecisionModel(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	return name == QianwenDecisionModel || strings.HasPrefix(name, QianwenDecisionModel+"-")
}

// The documented intent model uses Chat Completions and system-prompt modes.
func IsQianwenIntentModel(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), QianwenIntentModel)
}
