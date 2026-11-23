package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

type azureTestTransport func(*http.Request) (*http.Response, error)

func (f azureTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func azureTestReply(status int, body, region string) (*http.Response, error) {
	headers := make(http.Header)
	headers.Set("x-ms-region", region)
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func azureTestOptions(t *testing.T) AzureDiscoveryOptions {
	t.Helper()
	settings := system_setting.GetFetchSetting()
	old := *settings
	settings.ApplyIPFilterForDomain = false // All requests use the in-process transport below.
	t.Cleanup(func() { *settings = old })
	return AzureDiscoveryOptions{BaseURL: "https://resource.openai.azure.com/", Key: "data-key", APIVersion: "2025-04-01-preview", ResourceID: "/subscriptions/sub/resourceGroups/group/providers/Microsoft.CognitiveServices/accounts/resource"}
}

func TestAzureDiscoveryCatalogAndRegion(t *testing.T) {
	options := azureTestOptions(t)
	options.Headers = http.Header{"X-Custom": {"override"}}
	previous := constant.AzureDefaultAPIVersion
	constant.AzureDefaultAPIVersion = "2024-10-21"
	t.Cleanup(func() { constant.AzureDefaultAPIVersion = previous })
	for _, version := range []string{"2025-04-01-preview", ""} {
		options.APIVersion = version
		client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
			require.Equal(t, "/openai/models", r.URL.Path)
			wantVersion := version
			if wantVersion == "" {
				wantVersion = constant.AzureDefaultAPIVersion
			}
			require.Equal(t, wantVersion, r.URL.Query().Get("api-version"))
			require.Equal(t, "data-key", r.Header.Get("api-key"))
			require.Empty(t, r.Header.Get("Authorization"))
			require.Equal(t, "override", r.Header.Get("X-Custom"))
			return azureTestReply(200, `{"data":[{"id":"gpt-4.1"},{"id":" gpt-4.1 "},{"id":"embedding"},{"id":""}]}`, "East US")
		})}
		result, err := discoverAzure(context.Background(), options, client)
		require.NoError(t, err)
		require.Equal(t, []string{"embedding", "gpt-4.1"}, result.Models)
		require.Equal(t, "catalog", result.ModelSource)
		require.Equal(t, "East US", result.Region)
		require.Equal(t, "response_header", result.RegionSource)
		require.Empty(t, result.ModelError)
	}
}

func TestAzureDiscoveryDeploymentsAndPagination(t *testing.T) {
	options := azureTestOptions(t)
	options.ManagementToken = "management-secret"
	requests := 0
	client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "management.azure.com", r.URL.Host)
		require.Equal(t, "Bearer management-secret", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("api-key"))
		require.Equal(t, azureManagementVersion, r.URL.Query().Get("api-version"))
		if !strings.HasSuffix(r.URL.Path, "/deployments") {
			return azureTestReply(200, `{"location":"eastus"}`, "")
		}
		if r.URL.Query().Get("page") == "2" {
			return azureTestReply(200, `{"value":[{"name":"production","properties":{"provisioningState":"Succeeded"}},{"name":"second","properties":{"provisioningState":"Succeeded"}}]}`, "")
		}
		body, err := common.Marshal(map[string]any{
			"value": []any{
				map[string]any{"name": "production", "properties": map[string]any{"model": map[string]string{"name": "gpt-4.1"}, "provisioningState": "Succeeded"}},
				map[string]any{"name": "pending", "properties": map[string]string{"provisioningState": "Creating"}},
			},
			"nextLink": azureManagementOrigin + options.ResourceID + "/deployments?api-version=" + azureManagementVersion + "&page=2",
		})
		require.NoError(t, err)
		return azureTestReply(200, string(body), "")
	})}
	result, err := discoverAzure(context.Background(), options, client)
	require.NoError(t, err)
	require.Equal(t, 3, requests)
	require.Equal(t, []string{"production", "second"}, result.Models)
	require.Equal(t, "deployments", result.ModelSource)
	require.Equal(t, "resource", result.RegionSource)
	require.Equal(t, "eastus", result.Region)
}

func TestAzureDiscoveryManagementFailuresAndEmptyList(t *testing.T) {
	for _, status := range []int{200, 401, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			options := azureTestOptions(t)
			options.ManagementToken = "management-secret"
			options.Target = "models"
			catalogCalls := 0
			client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "management.azure.com" {
					return azureTestReply(status, `{"value":[]}`, "")
				}
				catalogCalls++
				require.Empty(t, r.Header.Get("Authorization"))
				return azureTestReply(200, `{"data":[{"id":"base-model"}]}`, "")
			})}
			result, err := discoverAzure(context.Background(), options, client)
			require.NoError(t, err)
			if status == 401 || status == 403 {
				require.Equal(t, 1, catalogCalls)
				require.True(t, result.ManagementFallback)
				require.Equal(t, []string{"base-model"}, result.Models)
				require.Equal(t, "catalog", result.ModelSource)
			} else {
				require.Zero(t, catalogCalls)
				require.Empty(t, result.Models)
				require.False(t, result.ManagementFallback)
				require.Equal(t, status != 200, result.ModelError != "")
			}
		})
	}
}

func TestAzureDiscoveryPartialResults(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		modelsStatus, accountStatus int
		body, region                string
		modelOK, regionOK           bool
	}{
		{"model_failure", 503, 200, `{"value":[]}`, "", false, true},
		{"region_failure", 200, 503, `{"value":[]}`, "", true, false},
		{"region_permission_fallback", 200, 403, `{"value":[]}`, "West Europe", true, true},
		{"deployment_permission_fallback", 403, 200, `{"value":[]}`, "West Europe", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := azureTestOptions(t)
			options.ManagementToken = "secret"
			client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "management.azure.com" {
					return azureTestReply(200, `{"data":[{"id":"base"}]}`, tc.region)
				}
				if strings.HasSuffix(r.URL.Path, "/deployments") {
					return azureTestReply(tc.modelsStatus, tc.body, "")
				}
				return azureTestReply(tc.accountStatus, `{"location":"eastus"}`, "")
			})}
			result, err := discoverAzure(context.Background(), options, client)
			require.NoError(t, err)
			require.Equal(t, tc.modelOK, result.ModelError == "")
			require.Equal(t, tc.regionOK, result.Region != "")
			if tc.name == "region_permission_fallback" {
				require.Equal(t, "deployments", result.ModelSource)
				require.Empty(t, result.Models)
			}
		})
	}
	for _, body := range []string{`{"data":[]}`, `{"unexpected":true}`} {
		options := azureTestOptions(t)
		client := &http.Client{Transport: azureTestTransport(func(*http.Request) (*http.Response, error) { return azureTestReply(200, body, "") })}
		result, err := discoverAzure(context.Background(), options, client)
		require.NoError(t, err)
		require.Empty(t, result.Region)
		require.Empty(t, result.RegionError)
		require.Equal(t, strings.Contains(body, "unexpected"), result.ModelError != "")
	}
}

func TestAzureDiscoveryCancellationAndRegionOnly(t *testing.T) {
	options := azureTestOptions(t)
	options.Target = "region"
	client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
		return azureTestReply(200, `not a model list`, "West Europe")
	})}
	result, err := discoverAzure(context.Background(), options, client)
	require.NoError(t, err)
	require.Empty(t, result.ModelError)
	require.Empty(t, result.ModelSource)
	require.Equal(t, "West Europe", result.Region)
	client.Transport = azureTestTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err = discoverAzure(ctx, options, client)
	require.NoError(t, err)
	require.Contains(t, result.RegionError, "超时")
}

func TestAzureDiscoveryRejectsCredentialRedirectsAndInvalidPagination(t *testing.T) {
	for _, next := range []string{"https://untrusted.invalid/deployments", azureManagementOrigin + "/other", azureManagementOrigin + "/subscriptions/sub/resourceGroups/group/providers/Microsoft.CognitiveServices/accounts/resource/deployments?api-version=" + azureManagementVersion} {
		options := azureTestOptions(t)
		options.ManagementToken = "secret"
		options.Target = "models"
		calls := 0
		client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			body, _ := common.Marshal(map[string]any{"value": []any{}, "nextLink": next})
			return azureTestReply(200, string(body), "")
		})}
		result, err := discoverAzure(context.Background(), options, client)
		require.NoError(t, err)
		require.Contains(t, result.ModelError, "分页")
		require.Equal(t, 1, calls)
	}
	options := azureTestOptions(t)
	calls := 0
	client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		response, _ := azureTestReply(302, "", "")
		response.Header.Set("Location", "https://untrusted.invalid/")
		return response, nil
	})}
	result, err := discoverAzure(context.Background(), options, client)
	require.NoError(t, err)
	require.Contains(t, result.ModelError, "302")
	require.Equal(t, 1, calls)
}
