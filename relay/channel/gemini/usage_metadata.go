package gemini

import "github.com/QuantumNous/new-api/dto"

// UsageFromMetadata maps only reported usage, without local token estimates.
func UsageFromMetadata(metadata dto.GeminiUsageMetadata) dto.Usage {
	usage := buildUsageFromGeminiMetadata(metadata, 0)
	textReported := false
	for _, details := range [][]dto.GeminiPromptTokensDetails{metadata.PromptTokensDetails, metadata.ToolUsePromptTokensDetails} {
		for _, detail := range details {
			if detail.Modality == "IMAGE" {
				usage.PromptTokensDetails.ImageTokens += detail.TokenCount
			}
			if detail.Modality == "TEXT" {
				textReported = true
			}
		}
	}
	if !textReported {
		usage.PromptTokensDetails.TextTokens = max(0, usage.PromptTokens-usage.PromptTokensDetails.AudioTokens-usage.PromptTokensDetails.ImageTokens)
	}
	usage.CompletionTokens = max(0, usage.CompletionTokens)
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return usage
}
