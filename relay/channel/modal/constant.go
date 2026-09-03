package modal

import "strings"

const ChannelName = "modal"

// Modal serves user-defined deployments, so there is no platform-wide model
// catalog. Models can be entered manually or fetched from the deployment.
var ModelList = []string{}

// NormalizeBaseURL accepts either a Modal deployment origin or the complete
// OpenAI-compatible chat completions URL shown by Modal's curl examples.
func NormalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, suffix := range []string{"/v1/chat/completions", "/chat/completions", "/v1"} {
		if strings.HasSuffix(baseURL, suffix) {
			return strings.TrimSuffix(baseURL, suffix)
		}
	}
	return baseURL
}
