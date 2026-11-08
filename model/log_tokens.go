package model

import "github.com/QuantumNous/new-api/common"

// logTokenCounts returns throughput counts, independently of billing discounts.
// OpenAI prompt counts already include cache; Anthropic prompt counts do not.
func logTokenCounts(promptTokens, completionTokens int, other string) (input, output int) {
	input, output = max(0, promptTokens), max(0, completionTokens)
	var metadata struct {
		InputTokensTotal      *int   `json:"input_tokens_total"`
		UsageSemantic         string `json:"usage_semantic"`
		UsageSource           string `json:"usage_source"`
		Claude                bool   `json:"claude"`
		CacheTokens           int    `json:"cache_tokens"`
		CacheWriteTokens      int    `json:"cache_write_tokens"`
		CacheCreationTokens   int    `json:"cache_creation_tokens"`
		CacheCreationTokens5m int    `json:"cache_creation_tokens_5m"`
		CacheCreationTokens1h int    `json:"cache_creation_tokens_1h"`
	}
	if other == "" || common.UnmarshalJsonStr(other, &metadata) != nil {
		return
	}
	if metadata.InputTokensTotal != nil && *metadata.InputTokensTotal >= 0 {
		return *metadata.InputTokensTotal, output
	}
	separateCache := metadata.UsageSemantic == "anthropic"
	if metadata.UsageSemantic == "" {
		// The "anthropic" source marks our OpenAI-normalized Claude conversion;
		// its prompt count includes cache even if the upstream format was Claude.
		// Untagged legacy conversions may only have the TTL split fields.
		separateCache = metadata.UsageSource != "anthropic" && (metadata.Claude ||
			(metadata.UsageSource == "" && (metadata.CacheCreationTokens5m > 0 || metadata.CacheCreationTokens1h > 0)))
	}
	if separateCache {
		cacheWrite := max(0, metadata.CacheWriteTokens, metadata.CacheCreationTokens,
			max(0, metadata.CacheCreationTokens5m)+max(0, metadata.CacheCreationTokens1h))
		input += max(0, metadata.CacheTokens) + cacheWrite
	}
	return
}
