package system_setting

import (
	"fmt"
	"strings"
	"testing"
)

func validSiteBackgroundSettings() SiteBackgroundSettings {
	return SiteBackgroundSettings{
		Enabled:        true,
		Fit:            SiteBackgroundFitCover,
		OverlayOpacity: 25,
		GlassEnabled:   true,
		GlassOpacity:   72,
		Sources: []SiteBackgroundSource{
			{
				Type:     SiteBackgroundSourceJSONAPI,
				URL:      "https://api.nekosia.cat/api/v1/images/catgirl",
				JSONPath: "image.compressed.url",
			},
		},
	}
}

func TestDefaultSiteBackgroundSettings(t *testing.T) {
	settings := DefaultSiteBackgroundSettings()
	if settings.Enabled {
		t.Fatal("site background must be disabled by default")
	}
	if settings.Fit != SiteBackgroundFitCover {
		t.Fatalf("default fit = %q, want cover", settings.Fit)
	}
	if settings.OverlayOpacity != 25 {
		t.Fatalf("default overlay opacity = %d, want 25", settings.OverlayOpacity)
	}
	if settings.GlassEnabled {
		t.Fatal("liquid glass must be disabled by default")
	}
	if settings.GlassOpacity != 72 {
		t.Fatalf("default glass opacity = %d, want 72", settings.GlassOpacity)
	}
	if len(settings.Sources) != 0 {
		t.Fatalf("default sources length = %d, want 0", len(settings.Sources))
	}
	if settings.Sources == nil {
		t.Fatal("default sources must serialize as an empty array, not null")
	}

	publicSettings := GetSiteBackgroundSettings()
	if publicSettings.Sources == nil {
		t.Fatal("public default sources must serialize as an empty array, not null")
	}
}

func TestValidateSiteBackgroundSettings(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SiteBackgroundSettings)
		wantErr bool
	}{
		{name: "valid nekosia config"},
		{name: "contain fit", mutate: func(settings *SiteBackgroundSettings) { settings.Fit = SiteBackgroundFitContain }},
		{name: "fill fit", mutate: func(settings *SiteBackgroundSettings) { settings.Fit = SiteBackgroundFitFill }},
		{name: "overlay lower boundary", mutate: func(settings *SiteBackgroundSettings) { settings.OverlayOpacity = 0 }},
		{name: "overlay upper boundary", mutate: func(settings *SiteBackgroundSettings) { settings.OverlayOpacity = 80 }},
		{name: "glass opacity lower boundary", mutate: func(settings *SiteBackgroundSettings) { settings.GlassOpacity = 0 }},
		{name: "glass opacity upper boundary", mutate: func(settings *SiteBackgroundSettings) { settings.GlassOpacity = 100 }},
		{name: "root relative image", mutate: func(settings *SiteBackgroundSettings) {
			settings.Sources = []SiteBackgroundSource{{Type: SiteBackgroundSourceImageURL, URL: "/background.jpg"}}
		}},
		{name: "direct image api", mutate: func(settings *SiteBackgroundSettings) {
			settings.Sources = []SiteBackgroundSource{{Type: SiteBackgroundSourceImageAPI, URL: "https://example.com/random"}}
		}},
		{name: "empty root json path", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].JSONPath = "" }},
		{name: "disabled without sources", mutate: func(settings *SiteBackgroundSettings) {
			settings.Enabled = false
			settings.Sources = nil
		}},
		{name: "invalid fit", mutate: func(settings *SiteBackgroundSettings) { settings.Fit = "auto" }, wantErr: true},
		{name: "overlay below range", mutate: func(settings *SiteBackgroundSettings) { settings.OverlayOpacity = -1 }, wantErr: true},
		{name: "overlay above range", mutate: func(settings *SiteBackgroundSettings) { settings.OverlayOpacity = 81 }, wantErr: true},
		{name: "glass opacity below range", mutate: func(settings *SiteBackgroundSettings) { settings.GlassOpacity = -1 }, wantErr: true},
		{name: "glass opacity above range", mutate: func(settings *SiteBackgroundSettings) { settings.GlassOpacity = 101 }, wantErr: true},
		{name: "enabled without sources", mutate: func(settings *SiteBackgroundSettings) { settings.Sources = nil }, wantErr: true},
		{name: "too many sources", mutate: func(settings *SiteBackgroundSettings) {
			settings.Sources = make([]SiteBackgroundSource, MaxSiteBackgroundSources+1)
			for index := range settings.Sources {
				settings.Sources[index] = SiteBackgroundSource{Type: SiteBackgroundSourceImageURL, URL: fmt.Sprintf("https://example.com/%d.jpg", index)}
			}
		}, wantErr: true},
		{name: "invalid source type", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].Type = "other" }, wantErr: true},
		{name: "empty source url", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].URL = "" }, wantErr: true},
		{name: "protocol relative url", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].URL = "//example.com/image.jpg" }, wantErr: true},
		{name: "unsupported url scheme", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].URL = "data:image/png;base64,AA==" }, wantErr: true},
		{name: "url credentials", mutate: func(settings *SiteBackgroundSettings) {
			settings.Sources[0].URL = "https://user:pass@example.com/image.jpg"
		}, wantErr: true},
		{name: "invalid json path", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].JSONPath = "image..url" }, wantErr: true},
		{name: "json path on image source", mutate: func(settings *SiteBackgroundSettings) { settings.Sources[0].Type = SiteBackgroundSourceImageURL }, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			settings := validSiteBackgroundSettings()
			if test.mutate != nil {
				test.mutate(&settings)
			}
			err := ValidateSiteBackgroundSettings(settings)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateSiteBackgroundSettings() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestValidateSiteBackgroundConfig(t *testing.T) {
	validJSON := `{"enabled":true,"fit":"cover","overlay_opacity":25,"glass_enabled":true,"glass_opacity":72,"sources":[{"type":"json_api","url":"https://api.nekosia.cat/api/v1/images/catgirl","json_path":"image.compressed.url"}]}`
	if err := ValidateSiteBackgroundConfig(validJSON); err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}

	if err := ValidateSiteBackgroundConfig(`{"enabled":`); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("invalid JSON error = %v, want JSON parse error", err)
	}
}

func TestSiteBackgroundLegacyConfigUsesGlassDefaults(t *testing.T) {
	legacyJSON := []byte(`{"enabled":false,"fit":"cover","overlay_opacity":25,"sources":[]}`)
	var settings SiteBackgroundSettings
	if err := settings.UnmarshalJSON(legacyJSON); err != nil {
		t.Fatalf("legacy config rejected: %v", err)
	}
	if settings.GlassEnabled {
		t.Fatal("legacy config must keep liquid glass disabled")
	}
	if settings.GlassOpacity != 72 {
		t.Fatalf("legacy glass opacity = %d, want 72", settings.GlassOpacity)
	}
}
