/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package service

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const maxSiteBackgroundJSONBytes = 1 << 20

type SiteBackgroundImage struct {
	ContentType string
	Data        []byte
}

func FetchSiteBackgroundImage(source system_setting.SiteBackgroundSource, baseURL string) (*SiteBackgroundImage, error) {
	resolvedSourceURL, err := resolveSiteBackgroundURL(source.URL, baseURL)
	if err != nil {
		return nil, err
	}

	imageURL := resolvedSourceURL
	if source.Type == system_setting.SiteBackgroundSourceJSONAPI {
		imageURL, err = fetchSiteBackgroundJSONImageURL(source, resolvedSourceURL)
		if err != nil {
			return nil, err
		}
	}

	return fetchSiteBackgroundImageBytes(imageURL)
}

func resolveSiteBackgroundURL(rawURL string, baseURL string) (string, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	parsedURL, err := url.Parse(trimmedURL)
	if err != nil {
		return "", fmt.Errorf("invalid site background URL: %w", err)
	}
	if !parsedURL.IsAbs() {
		if strings.TrimSpace(baseURL) == "" {
			return "", fmt.Errorf("relative site background URL requires a server address")
		}
		parsedBaseURL, parseErr := url.Parse(baseURL)
		if parseErr != nil || !parsedBaseURL.IsAbs() {
			return "", fmt.Errorf("invalid site background base URL")
		}
		parsedURL = parsedBaseURL.ResolveReference(parsedURL)
	}
	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return "", fmt.Errorf("site background URL must use HTTP(S)")
	}
	if parsedURL.User != nil {
		return "", fmt.Errorf("site background URL cannot contain credentials")
	}
	return parsedURL.String(), nil
}

func fetchSiteBackgroundJSONImageURL(source system_setting.SiteBackgroundSource, sourceURL string) (string, error) {
	response, err := DoDownloadRequest(sourceURL, "site background JSON API")
	if err != nil {
		return "", fmt.Errorf("failed to request site background JSON API: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("site background JSON API returned HTTP %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSiteBackgroundJSONBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read site background JSON API response: %w", err)
	}
	if len(data) > maxSiteBackgroundJSONBytes {
		return "", fmt.Errorf("site background JSON API response is too large")
	}

	var payload any
	if err = common.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("site background JSON API returned invalid JSON: %w", err)
	}
	value, ok := getSiteBackgroundJSONPathValue(payload, source.JSONPath)
	if !ok {
		return "", fmt.Errorf("site background JSON path does not exist")
	}
	imageURL, ok := value.(string)
	if !ok || strings.TrimSpace(imageURL) == "" {
		return "", fmt.Errorf("site background JSON path does not contain an image URL")
	}
	return resolveSiteBackgroundURL(imageURL, sourceURL)
}

func getSiteBackgroundJSONPathValue(payload any, path string) (any, bool) {
	current := payload
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return current, true
	}

	for _, segment := range strings.Split(trimmedPath, ".") {
		switch value := current.(type) {
		case map[string]any:
			var exists bool
			current, exists = value[segment]
			if !exists {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			current = value[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func fetchSiteBackgroundImageBytes(imageURL string) (*SiteBackgroundImage, error) {
	response, err := DoDownloadRequest(imageURL, "site background image")
	if err != nil {
		return nil, fmt.Errorf("failed to request site background image: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("site background image returned HTTP %d", response.StatusCode)
	}

	maxImageBytes := int64(constant.MaxFileDownloadMB) * 1024 * 1024
	if maxImageBytes <= 0 {
		maxImageBytes = 64 * 1024 * 1024
	}
	if response.ContentLength > maxImageBytes {
		return nil, fmt.Errorf("site background image exceeds the maximum download size")
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read site background image: %w", err)
	}
	if int64(len(data)) > maxImageBytes {
		return nil, fmt.Errorf("site background image exceeds the maximum download size")
	}

	contentType := smartDetectMimeType(response, imageURL, data)
	if parsedType, _, parseErr := mime.ParseMediaType(contentType); parseErr == nil {
		contentType = strings.ToLower(parsedType)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("site background response is not an image")
	}

	return &SiteBackgroundImage{ContentType: contentType, Data: data}, nil
}
