package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const azureManagementOrigin = "https://management.azure.com"
const azureManagementVersion = "2024-10-01"

type AzureDiscoveryOptions struct {
	BaseURL, Key, APIVersion, Proxy string
	ResourceID, ManagementToken     string
	Target                          string
	Headers                         http.Header
}

// ModelError and RegionError are independent: either query may succeed alone.
type AzureDiscoveryResult struct {
	Models             []string `json:"models"`
	ModelSource        string   `json:"model_source"`
	ModelError         string   `json:"model_error"`
	Region             string   `json:"region"`
	RegionSource       string   `json:"region_source"`
	RegionError        string   `json:"region_error"`
	ManagementFallback bool     `json:"management_fallback"`
}

func DiscoverAzure(ctx context.Context, options AzureDiscoveryOptions) (AzureDiscoveryResult, error) {
	client, err := GetHttpClientWithProxy(options.Proxy)
	if err != nil {
		return AzureDiscoveryResult{}, fmt.Errorf("Azure 代理配置无效")
	}
	return discoverAzure(ctx, options, client)
}

func discoverAzure(ctx context.Context, options AzureDiscoveryOptions, client *http.Client) (AzureDiscoveryResult, error) {
	result := AzureDiscoveryResult{Models: []string{}}
	if options.Target == "" {
		options.Target = "all"
	}
	if options.Target != "all" && options.Target != "models" && options.Target != "region" {
		return result, fmt.Errorf("无效的 Azure 查询目标")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(options.BaseURL), "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return result, fmt.Errorf("请输入有效的 Azure Endpoint")
	}
	options.ManagementToken = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(options.ManagementToken), "Bearer "))
	resourcePath := ""
	if options.ManagementToken != "" {
		resourcePath, err = azureResourcePath(options.ResourceID)
		if err != nil {
			return result, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	wantModels := options.Target != "region"
	wantRegion := options.Target != "models"
	managed := options.ManagementToken != ""
	needCatalog := wantModels && !managed
	managementHeaders := make(http.Header)
	managementHeaders.Set("Authorization", "Bearer "+options.ManagementToken)

	if wantModels && managed {
		result.ModelSource = "deployments"
		models, denied, fetchErr := azureDeploymentNames(ctx, client, resourcePath, managementHeaders)
		if denied {
			result.ManagementFallback = true
			managed = false
			needCatalog = true
		} else if fetchErr != nil {
			result.ModelError = fetchErr.Error()
		} else {
			result.Models = models
		}
	}
	if wantRegion && managed {
		body, _, status, fetchErr := azureDiscoveryGET(ctx, client, azureManagementOrigin+resourcePath+"?api-version="+azureManagementVersion, managementHeaders)
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			result.ManagementFallback = true
			managed = false
		} else if fetchErr != nil {
			result.RegionError = fetchErr.Error()
		} else {
			var account struct {
				Location string `json:"location"`
			}
			if common.Unmarshal(body, &account) != nil {
				result.RegionError = "无法解析 Azure 资源信息"
			} else if strings.TrimSpace(account.Location) != "" {
				result.Region = strings.TrimSpace(account.Location)
				result.RegionSource = "resource"
			}
		}
	}
	// A successful deployment listing (including an empty list) never becomes a catalog.
	needHeader := wantRegion && result.Region == "" && result.RegionError == ""
	if needCatalog || needHeader {
		if needCatalog {
			result.ModelSource = "catalog"
		}
		version := strings.TrimSpace(options.APIVersion)
		if version == "" {
			version = constant.AzureDefaultAPIVersion
		}
		base.Path = strings.TrimRight(base.Path, "/") + "/openai/models"
		base.RawPath = ""
		base.RawQuery = url.Values{"api-version": {version}}.Encode()
		headers := options.Headers.Clone()
		if headers == nil {
			headers = make(http.Header)
		}
		if _, exists := headers[http.CanonicalHeaderKey("api-key")]; !exists {
			headers.Set("api-key", options.Key)
		}
		body, responseHeaders, _, fetchErr := azureDiscoveryGET(ctx, client, base.String(), headers)
		if fetchErr != nil {
			if needCatalog {
				result.ModelError = fetchErr.Error()
			}
			if needHeader {
				result.RegionError = fetchErr.Error()
			}
		} else {
			if needHeader {
				result.Region = strings.TrimSpace(responseHeaders.Get("x-ms-region"))
				if result.Region != "" {
					result.RegionSource = "response_header"
				}
			}
			if needCatalog {
				var catalog struct {
					Data *[]struct {
						ID string `json:"id"`
					} `json:"data"`
				}
				if common.Unmarshal(body, &catalog) != nil || catalog.Data == nil {
					result.ModelError = "无法解析 Azure 模型目录"
				} else {
					for _, entry := range *catalog.Data {
						result.Models = append(result.Models, entry.ID)
					}
					result.Models = azureUniqueModels(result.Models)
				}
			}
		}
	}
	return result, nil
}

func azureResourcePath(resourceID string) (string, error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(resourceID), "/"), "/")
	if len(parts) != 8 || !strings.EqualFold(parts[0], "subscriptions") || !strings.EqualFold(parts[2], "resourceGroups") || !strings.EqualFold(parts[4], "providers") || !strings.EqualFold(parts[5], "Microsoft.CognitiveServices") || !strings.EqualFold(parts[6], "accounts") {
		return "", fmt.Errorf("请输入 Azure OpenAI 资源 ID：/subscriptions/.../resourceGroups/.../providers/Microsoft.CognitiveServices/accounts/...")
	}
	for _, index := range []int{1, 3, 7} {
		if parts[index] == "" || parts[index] == "." || parts[index] == ".." || strings.ContainsAny(parts[index], "?#%\\") {
			return "", fmt.Errorf("Azure 资源 ID 格式无效")
		}
		parts[index] = url.PathEscape(parts[index])
	}
	return "/" + strings.Join(parts, "/"), nil
}

func azureDeploymentNames(ctx context.Context, client *http.Client, resourcePath string, headers http.Header) ([]string, bool, error) {
	names := []string{}
	next := azureManagementOrigin + resourcePath + "/deployments?api-version=" + azureManagementVersion
	seen := make(map[string]bool)
	for page := 0; next != ""; page++ {
		parsed, err := url.Parse(next)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "management.azure.com" || parsed.User != nil || parsed.Fragment != "" || parsed.EscapedPath() != resourcePath+"/deployments" {
			return nil, false, fmt.Errorf("Azure 部署分页地址无效")
		}
		if page >= 100 || seen[next] {
			return nil, false, fmt.Errorf("Azure 部署分页超出限制或重复")
		}
		seen[next] = true
		body, _, status, err := azureDiscoveryGET(ctx, client, next, headers)
		if err != nil {
			return nil, status == http.StatusUnauthorized || status == http.StatusForbidden, err
		}
		var deployments struct {
			Value *[]struct {
				Name       string `json:"name"`
				Properties struct {
					ProvisioningState string `json:"provisioningState"`
				} `json:"properties"`
			} `json:"value"`
			NextLink string `json:"nextLink"`
		}
		if common.Unmarshal(body, &deployments) != nil || deployments.Value == nil {
			return nil, false, fmt.Errorf("无法解析 Azure 部署清单")
		}
		for _, deployment := range *deployments.Value {
			if strings.EqualFold(deployment.Properties.ProvisioningState, "Succeeded") {
				names = append(names, deployment.Name)
			}
		}
		next = deployments.NextLink
	}
	return azureUniqueModels(names), false, nil
}

func azureUniqueModels(models []string) []string {
	names := make([]string, 0, len(models))
	seen := make(map[string]bool)
	for _, name := range models {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func azureDiscoveryGET(ctx context.Context, client *http.Client, address string, headers http.Header) ([]byte, http.Header, int, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(address, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, nil, 0, fmt.Errorf("Azure 查询地址被请求策略拒绝")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("Azure 查询地址无效")
	}
	req.Header = headers.Clone()
	// Do not redirect credentials, including api-key (which net/http does not strip).
	requestClient := *client
	requestClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := requestClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, fmt.Errorf("Azure 查询已取消或超时")
		}
		return nil, nil, 0, fmt.Errorf("Azure 查询连接失败")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, response.Header, response.StatusCode, fmt.Errorf("Azure 查询失败（HTTP %d）", response.StatusCode)
	}
	const maxBody = 8 << 20
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return nil, response.Header, response.StatusCode, fmt.Errorf("Azure 响应读取失败或超出大小限制")
	}
	return body, response.Header, response.StatusCode, nil
}
