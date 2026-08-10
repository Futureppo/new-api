package middleware

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

//go:embed ip_ban_page.html
var ipBanPageTemplateSource string

var ipBanPageTemplate = template.Must(template.New("ip-ban-page").Parse(ipBanPageTemplateSource))

const ipBanBackgroundQueryKey = "_ip_ban_background"

type ipBanPageData struct {
	Lang                 string
	PageTitle            string
	SystemName           string
	Heading              string
	Description          string
	CurrentIPLabel       string
	ClientIP             string
	ReasonLabel          string
	Reason               string
	RestrictionLabel     string
	RestrictionText      string
	ContactAdmin         string
	HasExpiry            bool
	ExpiresAt            int64
	ExpiresAtISO         string
	BackgroundConfigJSON template.JS
}

func shouldRenderIPBanPage(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	return strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/html")
}

func shouldAllowIPBanBackgroundRequest(c *gin.Context) bool {
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		return false
	}
	if c.Query(ipBanBackgroundQueryKey) != "1" {
		return false
	}

	settings := system_setting.GetSiteBackgroundSettings()
	if !settings.Enabled {
		return false
	}

	requestQuery := cloneURLValues(c.Request.URL.Query())
	requestQuery.Del(ipBanBackgroundQueryKey)
	for _, source := range settings.Sources {
		if !source.Enabled {
			continue
		}
		sourceURL, sameOrigin := parseSameOriginBackgroundURL(c, source.URL)
		if !sameOrigin || sourceURL.Path != c.Request.URL.Path {
			continue
		}

		expectedQuery := cloneURLValues(sourceURL.Query())
		candidateQuery := cloneURLValues(requestQuery)
		if source.Type == system_setting.SiteBackgroundSourceImageAPI {
			expectedQuery.Del("_site_background")
			candidateQuery.Del("_site_background")
		}
		if expectedQuery.Encode() == candidateQuery.Encode() {
			return true
		}
	}
	return false
}

func prepareIPBanPageBackgroundSettings(c *gin.Context) system_setting.SiteBackgroundSettings {
	settings := system_setting.GetSiteBackgroundSettings()
	if !settings.Enabled {
		return settings
	}

	for index, source := range settings.Sources {
		if !source.Enabled {
			continue
		}
		sourceURL, sameOrigin := parseSameOriginBackgroundURL(c, source.URL)
		if !sameOrigin {
			continue
		}
		query := sourceURL.Query()
		query.Set(ipBanBackgroundQueryKey, "1")
		sourceURL.RawQuery = query.Encode()
		settings.Sources[index].URL = sourceURL.String()
	}
	return settings
}

func parseSameOriginBackgroundURL(c *gin.Context, rawURL string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, false
	}
	if parsed.Host == "" {
		return parsed, strings.HasPrefix(parsed.Path, "/")
	}
	return parsed, strings.EqualFold(parsed.Host, c.Request.Host)
}

func cloneURLValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, entries := range values {
		cloned[key] = append([]string(nil), entries...)
	}
	return cloned
}

func renderIPBanPage(c *gin.Context, ban *model.IPBan) error {
	backgroundJSON, err := common.Marshal(prepareIPBanPageBackgroundSettings(c))
	if err != nil {
		return fmt.Errorf("marshal site background settings: %w", err)
	}

	lang := appI18n.GetLangFromContext(c)
	systemName := strings.TrimSpace(common.SystemName)
	if systemName == "" {
		systemName = "New API"
	}

	data := ipBanPageData{
		Lang:             lang,
		PageTitle:        appI18n.T(c, appI18n.MsgIPBanPageTitle, map[string]any{"SystemName": systemName}),
		SystemName:       systemName,
		Heading:          appI18n.T(c, appI18n.MsgIPBanPageHeading),
		Description:      appI18n.T(c, appI18n.MsgIPBanPageDescription),
		CurrentIPLabel:   appI18n.T(c, appI18n.MsgIPBanPageCurrentIP),
		ClientIP:         c.ClientIP(),
		ReasonLabel:      appI18n.T(c, appI18n.MsgIPBanPageReason),
		Reason:           ban.Reason,
		RestrictionLabel: appI18n.T(c, appI18n.MsgIPBanPageRestrictionPeriod),
		RestrictionText:  appI18n.T(c, appI18n.MsgIPBanPagePermanent),
		ContactAdmin:     appI18n.T(c, appI18n.MsgIPBanPageContactAdmin),
		// common.Marshal uses encoding/json's HTML-safe escaping. Marking the complete
		// JSON value as template.JS keeps it as data rather than a quoted JS string.
		BackgroundConfigJSON: template.JS(backgroundJSON),
	}

	if ban.ExpiresAt > 0 {
		expiresAt := time.Unix(ban.ExpiresAt, 0)
		data.HasExpiry = true
		data.ExpiresAt = ban.ExpiresAt
		data.ExpiresAtISO = expiresAt.UTC().Format(time.RFC3339)
		data.RestrictionLabel = appI18n.T(c, appI18n.MsgIPBanPageUnblocksAt)
		data.RestrictionText = expiresAt.Local().Format("2006-01-02 15:04:05 MST")
	}

	var page bytes.Buffer
	if err := ipBanPageTemplate.Execute(&page, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}

	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Content-Language", lang)
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self' data: blob: http: https:; connect-src 'self' http: https:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	c.Data(http.StatusForbidden, "text/html; charset=utf-8", page.Bytes())
	return nil
}
