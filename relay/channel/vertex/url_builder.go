package vertex

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultAPIVersion    = "v1"
	OpenSourceAPIVersion = "v1beta1"
	PublisherGoogle      = "google"
	PublisherAnthropic   = "anthropic"
)

func normalizeVertexBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func appendVertexAPIVersion(baseURL, version string) string {
	version = strings.Trim(strings.TrimSpace(version), "/")
	if version == "" || strings.HasSuffix(baseURL, "/"+version) {
		return baseURL
	}
	return baseURL + "/" + version
}

func buildPublisherAPIBaseURL(baseURL, version, projectID, region string) string {
	if normalized := normalizeVertexBaseURL(baseURL); normalized != "" {
		normalized = appendVertexAPIVersion(normalized, version)
		if strings.TrimSpace(projectID) != "" {
			return fmt.Sprintf("%s/projects/%s/locations/%s", normalized, projectID, region)
		}
		return normalized
	}
	if region == "global" {
		if strings.TrimSpace(projectID) == "" {
			return fmt.Sprintf("https://aiplatform.googleapis.com/%s", version)
		}
		return fmt.Sprintf("https://aiplatform.googleapis.com/%s/projects/%s/locations/global", version, projectID)
	}
	if strings.TrimSpace(projectID) == "" {
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com/%s", region, version)
	}
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/%s/projects/%s/locations/%s", region, version, projectID, region)
}

func BuildPublisherModelURL(baseURL, version, projectID, region, publisher, modelName, action string) string {
	return fmt.Sprintf(
		"%s/publishers/%s/models/%s:%s",
		buildPublisherAPIBaseURL(baseURL, version, projectID, region),
		publisher,
		modelName,
		action,
	)
}

func BuildGoogleModelURL(baseURL, version, projectID, region, modelName, action string) string {
	return BuildPublisherModelURL(baseURL, version, projectID, region, PublisherGoogle, modelName, action)
}

func BuildAnthropicModelURL(baseURL, version, projectID, region, modelName, action string) string {
	return BuildPublisherModelURL(baseURL, version, projectID, region, PublisherAnthropic, modelName, action)
}

func BuildOpenSourceChatCompletionsURL(baseURL, projectID, region string) string {
	if normalized := normalizeVertexBaseURL(baseURL); normalized != "" {
		return fmt.Sprintf(
			"%s/projects/%s/locations/%s/endpoints/openapi/chat/completions",
			appendVertexAPIVersion(normalized, OpenSourceAPIVersion),
			projectID,
			region,
		)
	}
	// Vertex MaaS intentionally uses the global API hostname even for regional locations.
	return fmt.Sprintf(
		"https://aiplatform.googleapis.com/%s/projects/%s/locations/%s/endpoints/openapi/chat/completions",
		OpenSourceAPIVersion,
		projectID,
		region,
	)
}

func AppendAPIKey(rawURL, apiKey string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("key", apiKey)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func BuildGoogleModelsListURL(baseURL, region, pageToken string) (string, error) {
	rawURL := buildPublisherAPIBaseURL(baseURL, OpenSourceAPIVersion, "", region) + "/publishers/google/models"
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := u.Query()
	query.Set("pageSize", "1000")
	query.Set("listAllVersions", "false")
	query.Set("view", "PUBLISHER_MODEL_VIEW_BASIC")
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
