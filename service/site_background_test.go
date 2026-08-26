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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

func allowPrivateSiteBackgroundTestRequests(t *testing.T) {
	t.Helper()
	fetchSetting := system_setting.GetFetchSetting()
	originalProtection := fetchSetting.EnableSSRFProtection
	fetchSetting.EnableSSRFProtection = false
	InitHttpClient()
	t.Cleanup(func() {
		fetchSetting.EnableSSRFProtection = originalProtection
		InitHttpClient()
	})
}

func TestFetchSiteBackgroundImageRequestsImageAPIOnce(t *testing.T) {
	allowPrivateSiteBackgroundTestRequests(t)

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write([]byte("current-random-image"))
	}))
	t.Cleanup(server.Close)

	image, err := FetchSiteBackgroundImage(system_setting.SiteBackgroundSource{
		Type:    system_setting.SiteBackgroundSourceImageAPI,
		URL:     server.URL,
		Enabled: true,
		Weight:  1,
	}, "")

	require.NoError(t, err)
	require.Equal(t, int32(1), requestCount.Load())
	require.Equal(t, "image/png", image.ContentType)
	require.Equal(t, []byte("current-random-image"), image.Data)
}

func TestFetchSiteBackgroundImageResolvesJSONAPI(t *testing.T) {
	allowPrivateSiteBackgroundTestRequests(t)

	var metadataRequests atomic.Int32
	var imageRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/metadata":
			metadataRequests.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"image":{"url":"/asset"}}`))
		case "/asset":
			imageRequests.Add(1)
			writer.Header().Set("Content-Type", "image/webp")
			_, _ = writer.Write([]byte("resolved-image"))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	image, err := FetchSiteBackgroundImage(system_setting.SiteBackgroundSource{
		Type:     system_setting.SiteBackgroundSourceJSONAPI,
		URL:      server.URL + "/metadata",
		JSONPath: "image.url",
		Enabled:  true,
		Weight:   1,
	}, "")

	require.NoError(t, err)
	require.Equal(t, int32(1), metadataRequests.Load())
	require.Equal(t, int32(1), imageRequests.Load())
	require.Equal(t, "image/webp", image.ContentType)
	require.Equal(t, []byte("resolved-image"), image.Data)
}

func TestFetchSiteBackgroundImageRejectsNonImage(t *testing.T) {
	allowPrivateSiteBackgroundTestRequests(t)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("not an image"))
	}))
	t.Cleanup(server.Close)

	_, err := FetchSiteBackgroundImage(system_setting.SiteBackgroundSource{
		Type:    system_setting.SiteBackgroundSourceImageURL,
		URL:     server.URL,
		Enabled: true,
		Weight:  1,
	}, "")

	require.ErrorContains(t, err, "not an image")
}
