package openrouter

import "strings"

// NormalizeBaseURL accepts both the dashboard's /api base and SDK /api/v1 bases.
// Custom proxy prefixes are retained.
func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://openrouter.ai/api"
	}
	return strings.TrimSuffix(base, "/v1")
}
