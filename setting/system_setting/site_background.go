package system_setting

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const (
	SiteBackgroundSourceImageURL = "image_url"
	SiteBackgroundSourceImageAPI = "image_api"
	SiteBackgroundSourceJSONAPI  = "json_api"

	SiteBackgroundFitCover   = "cover"
	SiteBackgroundFitContain = "contain"
	SiteBackgroundFitFill    = "fill"

	MaxSiteBackgroundSources = 20
)

var siteBackgroundJSONPathSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type SiteBackgroundSource struct {
	Type     string `json:"type"`
	URL      string `json:"url"`
	JSONPath string `json:"json_path,omitempty"`
}

type SiteBackgroundSettings struct {
	Enabled        bool                   `json:"enabled"`
	Fit            string                 `json:"fit"`
	OverlayOpacity int                    `json:"overlay_opacity"`
	GlassEnabled   bool                   `json:"glass_enabled"`
	GlassOpacity   int                    `json:"glass_opacity"`
	Sources        []SiteBackgroundSource `json:"sources"`
}

type siteBackgroundConfig struct {
	Config SiteBackgroundSettings `json:"config"`
}

var defaultSiteBackgroundConfig = siteBackgroundConfig{
	Config: DefaultSiteBackgroundSettings(),
}

func init() {
	config.GlobalConfig.Register("site_background", &defaultSiteBackgroundConfig)
}

func DefaultSiteBackgroundSettings() SiteBackgroundSettings {
	return SiteBackgroundSettings{
		Enabled:        false,
		Fit:            SiteBackgroundFitCover,
		OverlayOpacity: 25,
		GlassEnabled:   false,
		GlassOpacity:   72,
		Sources:        []SiteBackgroundSource{},
	}
}

func (settings *SiteBackgroundSettings) UnmarshalJSON(data []byte) error {
	type siteBackgroundSettingsAlias SiteBackgroundSettings
	var decoded siteBackgroundSettingsAlias
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]any
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, exists := fields["glass_opacity"]; !exists {
		decoded.GlassOpacity = DefaultSiteBackgroundSettings().GlassOpacity
	}
	*settings = SiteBackgroundSettings(decoded)
	return nil
}

func GetSiteBackgroundSettings() SiteBackgroundSettings {
	settings := defaultSiteBackgroundConfig.Config
	if err := ValidateSiteBackgroundSettings(settings); err != nil {
		return DefaultSiteBackgroundSettings()
	}
	settings.Sources = append([]SiteBackgroundSource{}, settings.Sources...)
	return settings
}

func ValidateSiteBackgroundConfig(value string) error {
	var settings SiteBackgroundSettings
	if err := common.UnmarshalJsonStr(value, &settings); err != nil {
		return fmt.Errorf("站点背景配置不是有效的 JSON: %w", err)
	}
	return ValidateSiteBackgroundSettings(settings)
}

func ValidateSiteBackgroundSettings(settings SiteBackgroundSettings) error {
	switch settings.Fit {
	case SiteBackgroundFitCover, SiteBackgroundFitContain, SiteBackgroundFitFill:
	default:
		return fmt.Errorf("站点背景显示模式必须是 cover、contain 或 fill")
	}

	if settings.OverlayOpacity < 0 || settings.OverlayOpacity > 80 {
		return fmt.Errorf("站点背景遮罩强度必须在 0 到 80 之间")
	}
	if settings.GlassOpacity < 0 || settings.GlassOpacity > 100 {
		return fmt.Errorf("液态玻璃不透明度必须在 0 到 100 之间")
	}

	if len(settings.Sources) > MaxSiteBackgroundSources {
		return fmt.Errorf("站点背景来源不能超过 %d 个", MaxSiteBackgroundSources)
	}
	if settings.Enabled && len(settings.Sources) == 0 {
		return fmt.Errorf("启用站点背景前请至少添加一个图片来源")
	}

	for index, source := range settings.Sources {
		if err := validateSiteBackgroundSource(source); err != nil {
			return fmt.Errorf("第 %d 个站点背景来源无效: %w", index+1, err)
		}
	}
	return nil
}

func validateSiteBackgroundSource(source SiteBackgroundSource) error {
	switch source.Type {
	case SiteBackgroundSourceImageURL, SiteBackgroundSourceImageAPI, SiteBackgroundSourceJSONAPI:
	default:
		return fmt.Errorf("不支持的来源类型 %q", source.Type)
	}

	if err := validateSiteBackgroundURL(source.URL); err != nil {
		return err
	}

	jsonPath := strings.TrimSpace(source.JSONPath)
	if source.Type != SiteBackgroundSourceJSONAPI {
		if jsonPath != "" {
			return fmt.Errorf("只有 JSON API 来源可以设置 JSON 路径")
		}
		return nil
	}
	if jsonPath == "" {
		return nil
	}

	for _, segment := range strings.Split(jsonPath, ".") {
		if !siteBackgroundJSONPathSegmentPattern.MatchString(segment) {
			return fmt.Errorf("JSON 路径 %q 无效", source.JSONPath)
		}
	}
	return nil
}

func validateSiteBackgroundURL(rawURL string) error {
	trimmedURL := strings.TrimSpace(rawURL)
	if trimmedURL == "" {
		return fmt.Errorf("图片地址不能为空")
	}
	if strings.HasPrefix(trimmedURL, "/") && !strings.HasPrefix(trimmedURL, "//") {
		return nil
	}

	parsedURL, err := url.Parse(trimmedURL)
	if err != nil || parsedURL.Host == "" {
		return fmt.Errorf("图片地址必须是 HTTP(S) 地址或以 / 开头的同源路径")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("图片地址只支持 HTTP(S)")
	}
	if parsedURL.User != nil {
		return fmt.Errorf("图片地址不能包含用户名或密码")
	}
	return nil
}
