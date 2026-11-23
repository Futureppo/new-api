package ir

import "github.com/QuantumNous/new-api/dto"

// ToDTOUsage converts normalized IR Usage into new-api's billing *dto.Usage,
// preserving exact Anthropic-exclusive vs OpenAI-inclusive cache accounting semantics.
func (u *Usage) ToDTOUsage() *dto.Usage {
	if u == nil {
		return &dto.Usage{}
	}
	out := &dto.Usage{
		PromptTokens:                u.InputTokens,
		CompletionTokens:            u.OutputTokens,
		TotalTokens:                 u.TotalTokens,
		InputTokens:                 u.InputTokens,
		OutputTokens:                u.OutputTokens,
		ClaudeCacheCreation5mTokens: u.CacheCreation5mTokens,
		ClaudeCacheCreation1hTokens: u.CacheCreation1hTokens,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         u.CachedReadTokens,
			CachedCreationTokens: u.CacheCreationTokens,
			TextTokens:           u.TextInputTokens,
			AudioTokens:          u.AudioInputTokens,
			ImageTokens:          u.ImageInputTokens,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{
			TextTokens:      u.TextOutputTokens,
			AudioTokens:     u.AudioOutputTokens,
			ImageTokens:     u.ImageOutputTokens,
			ReasoningTokens: u.ReasoningTokens,
		},
	}
	if u.InputExclusive {
		out.UsageSemantic = "anthropic"
	} else {
		out.UsageSemantic = "openai"
	}
	if u.CacheCreationTokens > 0 {
		v := u.CacheCreationTokens
		out.PromptTokensDetails.CacheWriteTokens = &v
	}
	if computed := u.InclusiveInputTokens() + out.CompletionTokens; out.TotalTokens < computed {
		out.TotalTokens = computed
	}
	return out
}

// NormalizeCacheCreationSplit mirrors service.NormalizeCacheCreationSplit so 5m/1h
// cache creation counts remain consistent without importing service (preventing cycles).
func NormalizeCacheCreationSplit(totalTokens, tokens5m, tokens1h int) (int, int) {
	if totalTokens <= 0 {
		return 0, 0
	}
	if tokens5m < 0 {
		tokens5m = 0
	}
	if tokens1h < 0 {
		tokens1h = 0
	}
	if remainder := totalTokens - tokens5m - tokens1h; remainder > 0 {
		return tokens5m + remainder, tokens1h
	}
	return tokens5m, tokens1h
}
