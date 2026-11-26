package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestAzureDiscoveryKeyDeployments(t *testing.T) {
	for _, suffix := range []string{"", "/", "/openai", "/openai/v1/"} {
		t.Run(suffix, func(t *testing.T) {
			options := azureTestOptions(t)
			options.BaseURL = "https://resource.cognitiveservices.azure.com" + suffix
			options.Headers = http.Header{"X-Custom": {"kept"}}
			calls := 0
			client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/openai/deployments", r.URL.Path)
				require.Equal(t, "2023-03-15-preview", r.URL.Query().Get("api-version"))
				require.Equal(t, "data-key", r.Header.Get("api-key"))
				require.Equal(t, "kept", r.Header.Get("X-Custom"))
				require.Empty(t, r.Header.Get("Authorization"))
				return azureTestReply(200, `{"data":[{"id":"dep-sol","model":"gpt-6-sol","status":"succeeded"},{"id":"dep-astra","model":"gpt-6-astra","status":"Succeeded"},{"id":"dep-sol","model":"gpt-6-sol","status":"succeeded"},{"id":"pending","model":"gpt-6.1-sol","status":"creating"},{"id":"failed","status":"failed"}]}`, "East US 2")
			})}
			result, err := discoverAzure(context.Background(), options, client)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, []string{"dep-astra", "dep-sol"}, result.Models)
			require.Equal(t, []AzureDeployment{{ID: "dep-astra", Model: "gpt-6-astra"}, {ID: "dep-sol", Model: "gpt-6-sol"}}, result.Deployments)
			require.Equal(t, "deployments", result.ModelSource)
			require.Equal(t, "East US 2", result.Region)
			require.Equal(t, "response_header", result.RegionSource)
			require.Empty(t, result.ModelError)
			require.Empty(t, result.RegionError)
		})
	}
}

func TestAzureDiscoveryKeyDeploymentsPagination(t *testing.T) {
	options := azureTestOptions(t)
	options.BaseURL = "https://proxy.example.com/azure/openai/v1/"
	options.Target = "models"
	calls := 0
	client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "proxy.example.com", r.URL.Host)
		require.Equal(t, "/azure/openai/deployments", r.URL.Path)
		require.Equal(t, azureDeploymentListVersion, r.URL.Query().Get("api-version"))
		if r.URL.Query().Get("page") == "2" {
			return azureTestReply(200, `{"data":[{"id":"first","model":"gpt-6-sol","status":"succeeded"},{"id":"second","model":"gpt-6-astra","status":"succeeded"}]}`, "")
		}
		return azureTestReply(200, `{"data":[{"id":"first","model":"gpt-6-sol","status":"succeeded"}],"nextLink":"?api-version=2023-03-15-preview&page=2"}`, "")
	})}
	result, err := discoverAzure(context.Background(), options, client)
	require.NoError(t, err)
	require.Empty(t, result.ModelError)
	require.Equal(t, 2, calls)
	require.Equal(t, []string{"first", "second"}, result.Models)
	require.Len(t, result.Deployments, 2)
}

func TestAzureDiscoveryKeyDeploymentsFallbackAndErrors(t *testing.T) {
	for _, status := range []int{200, 400, 401, 403, 404, 405, 410, 429, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			options := azureTestOptions(t)
			options.Target = "models"
			catalogCalls := 0
			client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/deployments") {
					return azureTestReply(status, `{"data":[]}`, "")
				}
				catalogCalls++
				require.Equal(t, options.APIVersion, r.URL.Query().Get("api-version"))
				return azureTestReply(200, `{"data":[{"id":"catalog-model"}]}`, "")
			})}
			result, err := discoverAzure(context.Background(), options, client)
			require.NoError(t, err)
			if status == 404 || status == 405 || status == 410 {
				require.Equal(t, 1, catalogCalls)
				require.Equal(t, "catalog", result.ModelSource)
				require.Equal(t, []string{"catalog-model"}, result.Models)
				require.Empty(t, result.ModelError)
			} else {
				require.Zero(t, catalogCalls)
				require.Equal(t, "deployments", result.ModelSource)
				require.Empty(t, result.Models)
				require.Equal(t, status != 200, result.ModelError != "")
			}
		})
	}
}

func TestAzureDiscoveryDeniedManagementUsesKeyDeployments(t *testing.T) {
	options := azureTestOptions(t)
	options.ManagementToken = "management-secret"
	client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "management.azure.com" {
			return azureTestReply(403, `{}`, "")
		}
		require.Equal(t, "/openai/deployments", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		require.Equal(t, "data-key", r.Header.Get("api-key"))
		return azureTestReply(200, `{"data":[{"id":"production","model":"gpt-6-sol","status":"succeeded"}]}`, "East US")
	})}
	result, err := discoverAzure(context.Background(), options, client)
	require.NoError(t, err)
	require.True(t, result.ManagementFallback)
	require.Equal(t, "deployments", result.ModelSource)
	require.Equal(t, []string{"production"}, result.Models)
	require.Equal(t, "East US", result.Region)
}

func TestAzureDiscoveryKeyDeploymentsPartialResults(t *testing.T) {
	for _, tc := range []struct {
		name, deployments, region string
		catalogStatus             int
		modelError, regionError   bool
	}{
		{"missing region", `{"data":[]}`, "", 200, false, false},
		{"region error", `{"data":[{"id":"production","model":"gpt-6-sol","status":"succeeded"}]}`, "", 503, false, true},
		{"invalid deployment list", `{"unexpected":true}`, "East US", 200, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := azureTestOptions(t)
			client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/deployments") {
					return azureTestReply(200, tc.deployments, tc.region)
				}
				return azureTestReply(tc.catalogStatus, `{"data":[{"id":"never-fill-this"}]}`, "")
			})}
			result, err := discoverAzure(context.Background(), options, client)
			require.NoError(t, err)
			require.Equal(t, "deployments", result.ModelSource)
			require.Equal(t, tc.modelError, result.ModelError != "")
			require.Equal(t, tc.regionError, result.RegionError != "")
			require.NotContains(t, result.Models, "never-fill-this")
			if tc.name == "region error" {
				require.Equal(t, []string{"production"}, result.Models)
			}
		})
	}
}

func TestAzureDiscoveryKeyDeploymentsPaginationSafety(t *testing.T) {
	for _, next := range []string{
		"https://other.example.com/openai/deployments?api-version=2023-03-15-preview",
		"http://resource.openai.azure.com/openai/deployments?api-version=2023-03-15-preview",
		"/other?api-version=2023-03-15-preview",
		"?api-version=2024-10-21",
		"?api-version=2023-03-15-preview",
	} {
		t.Run(next, func(t *testing.T) {
			options := azureTestOptions(t)
			options.Target = "models"
			calls := 0
			client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body, err := common.Marshal(map[string]any{"data": []any{}, "nextLink": next})
				require.NoError(t, err)
				return azureTestReply(200, string(body), "")
			})}
			result, err := discoverAzure(context.Background(), options, client)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Contains(t, result.ModelError, "分页")
			require.Empty(t, result.Models)
		})
	}
	options := azureTestOptions(t)
	options.Target = "models"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: azureTestTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	result, err := discoverAzure(ctx, options, client)
	require.NoError(t, err)
	require.Contains(t, result.ModelError, "超时")
	require.Empty(t, result.Models)
}
