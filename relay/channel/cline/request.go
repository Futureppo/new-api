package cline

import (
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

// Cline SDK withMaxCompletionTokensForReasoningModels, pinned to the same
// revision as client_headers.go. Other provider-qualified IDs remain intact.
var reasoningModelID = regexp.MustCompile(`(^|[^a-z0-9])(o[134]|gpt-?5)($|[^a-z0-9])`)

func NormalizeRequest(request *dto.GeneralOpenAIRequest) {
	if request.MaxTokens == nil || !reasoningModelID.MatchString(strings.ToLower(strings.TrimSpace(request.Model))) {
		return
	}
	if request.MaxCompletionTokens == nil {
		request.MaxCompletionTokens = request.MaxTokens
	}
	request.MaxTokens = nil
}
