package setting

import "github.com/QuantumNous/new-api/setting/config"

type EnhancementSetting struct {
	PublicEmbedEnabled          bool     `json:"public_embed_enabled"`
	SelectedModels              []string `json:"selected_models"`
	ModelStatusTimeWindowMins   int      `json:"model_status_time_window_mins"`
	ModelStatusRefreshSeconds   int      `json:"model_status_refresh_seconds"`
	ModelStatusSlotMinutes      int      `json:"model_status_slot_minutes"`
	ModelStatusGreenThreshold   float64  `json:"model_status_green_threshold"`
	ModelStatusYellowThreshold  float64  `json:"model_status_yellow_threshold"`
	ModelStatusShowZeroRequests bool     `json:"model_status_show_zero_requests"`
	ModelStatusTheme            string   `json:"model_status_theme"`
	ModelStatusSortMode         string   `json:"model_status_sort_mode"`
	ModelStatusSiteTitle        string   `json:"model_status_site_title"`
	AIBanEnabled                bool     `json:"ai_ban_enabled"`
	AIBanDryRun                 bool     `json:"ai_ban_dry_run"`
	AIBanModel                  string   `json:"ai_ban_model"`
	AIBanBaseURL                string   `json:"ai_ban_base_url"`
	AIBanAPIKey                 string   `json:"ai_ban_api_key"`
}

var enhancementSetting = EnhancementSetting{
	PublicEmbedEnabled:          false,
	SelectedModels:              []string{},
	ModelStatusTimeWindowMins:   60,
	ModelStatusRefreshSeconds:   60,
	ModelStatusSlotMinutes:      30,
	ModelStatusGreenThreshold:   95,
	ModelStatusYellowThreshold:  80,
	ModelStatusShowZeroRequests: true,
	ModelStatusTheme:            "light",
	ModelStatusSortMode:         "name",
	ModelStatusSiteTitle:        "Model Status",
	AIBanEnabled:                false,
	AIBanDryRun:                 true,
}

func init() {
	config.GlobalConfig.Register("enhancement_setting", &enhancementSetting)
}

func GetEnhancementSetting() *EnhancementSetting {
	return &enhancementSetting
}
