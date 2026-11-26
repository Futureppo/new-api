package openai

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
)

// Accept the resource endpoint and the documented SDK base URLs without
// duplicating /openai or /anthropic. Preserve a reverse proxy's path prefix.
func azureRequestURL(info *relaycommon.RelayInfo) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/"))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid Azure endpoint")
	}
	prefix := strings.TrimRight(u.Path, "/")
	v1 := strings.HasSuffix(prefix, "/openai/v1")
	for _, suffix := range []string{"/openai/v1", "/openai", "/anthropic/v1", "/anthropic"} {
		if strings.HasSuffix(prefix, suffix) {
			prefix = strings.TrimSuffix(prefix, suffix)
			break
		}
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment = prefix, "", "", ""
	if relaycommon.IsAzureClaude(info) {
		if info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact {
			return "", fmt.Errorf("Azure Claude requires the Messages protocol; enable IR conversion for Responses requests")
		}
		// Foundry's Anthropic endpoint uses the same resource name on this host.
		// Custom proxies and private endpoints retain their configured hostname.
		host := strings.ToLower(u.Hostname())
		port := u.Port()
		for _, suffix := range []string{".openai.azure.com", ".cognitiveservices.azure.com"} {
			if strings.HasSuffix(host, suffix) && !strings.Contains(strings.TrimSuffix(host, suffix), ".") {
				u.Host = strings.TrimSuffix(host, suffix) + ".services.ai.azure.com"
				if port != "" {
					u.Host += ":" + port
				}
				break
			}
		}
		return u.String() + "/anthropic/v1/messages", nil
	}
	version := info.ApiVersion
	if version == "" {
		version = constant.AzureDefaultAPIVersion
	}
	query := url.Values{}
	var path string
	switch info.RelayMode {
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		path = "/openai/v1/responses"
		// Date versions explicitly select the legacy preview API. GA v1 needs
		// no api-version; never copy a Chat Completions version into v1.
		if override := strings.TrimSpace(info.ChannelOtherSettings.AzureResponsesVersion); override != "" && override != "v1" {
			query.Set("api-version", override)
			if override != "preview" {
				path = "/openai/responses"
			}
		}
		if info.RelayMode == relayconstant.RelayModeResponsesCompact {
			path += "/compact"
		}
	case relayconstant.RelayModeRealtime:
		path = "/openai/realtime"
		query.Set("deployment", info.UpstreamModelName)
		query.Set("api-version", version)
	default:
		task := strings.TrimPrefix(strings.Split(info.RequestURLPath, "?")[0], "/v1/")
		if info.RelayMode == relayconstant.RelayModeChatCompletions || info.RelayFormat == types.RelayFormatClaude || info.RelayFormat == types.RelayFormatGemini {
			task = "chat/completions"
		}
		if v1 {
			path = "/openai/v1/" + task
		} else {
			deployment := info.UpstreamModelName
			// Retain the historical unmapped-name behavior only for dated old channels.
			if info.ChannelCreateTime > 0 && info.ChannelCreateTime < constant.AzureNoRemoveDotTime && !info.IsModelMapped && len(info.ChannelOtherSettings.AzureDeploymentModels) == 0 {
				deployment = strings.ReplaceAll(deployment, ".", "")
			}
			path = "/openai/deployments/" + url.PathEscape(deployment) + "/" + task
			query.Set("api-version", version)
		}
	}
	result := relaycommon.GetFullRequestURL(u.String(), path, info.ChannelType)
	if len(query) > 0 {
		result += "?" + query.Encode()
	}
	return result, nil
}
