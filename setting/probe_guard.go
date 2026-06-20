package setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type ProbeGuardSetting struct {
	Enabled               bool `json:"enabled"`
	DryRun                bool `json:"dry_run"`
	WindowSeconds         int  `json:"window_seconds"`
	DistinctModelCount    int  `json:"distinct_model_count"`
	FirstIPBanMinutes     int  `json:"first_ip_ban_minutes"`
	SecondIPBanMinutes    int  `json:"second_ip_ban_minutes"`
	PermanentOffenseCount int  `json:"permanent_offense_count"`
	OffenseDedupeSeconds  int  `json:"offense_dedupe_seconds"`
	MaxIPsPerOffense      int  `json:"max_ips_per_offense"`
}

var probeGuardSetting = ProbeGuardSetting{
	Enabled:               false,
	DryRun:                true,
	WindowSeconds:         60,
	DistinctModelCount:    5,
	FirstIPBanMinutes:     10,
	SecondIPBanMinutes:    60,
	PermanentOffenseCount: 3,
	OffenseDedupeSeconds:  60,
	MaxIPsPerOffense:      32,
}

func init() {
	config.GlobalConfig.Register("probe_guard_setting", &probeGuardSetting)
}

func GetProbeGuardSetting() ProbeGuardSetting {
	cfg := probeGuardSetting
	cfg.Normalize()
	return cfg
}

func (cfg *ProbeGuardSetting) Normalize() {
	if cfg.WindowSeconds < 10 {
		cfg.WindowSeconds = 60
	}
	if cfg.DistinctModelCount < 2 {
		cfg.DistinctModelCount = 5
	}
	if cfg.FirstIPBanMinutes < 1 {
		cfg.FirstIPBanMinutes = 10
	}
	if cfg.SecondIPBanMinutes < 1 {
		cfg.SecondIPBanMinutes = 60
	}
	if cfg.PermanentOffenseCount < 1 {
		cfg.PermanentOffenseCount = 3
	}
	if cfg.OffenseDedupeSeconds < 10 {
		cfg.OffenseDedupeSeconds = 60
	}
	if cfg.MaxIPsPerOffense < 1 {
		cfg.MaxIPsPerOffense = 32
	}
}

func CheckProbeGuardOption(key string, value string) error {
	key = strings.TrimPrefix(key, "probe_guard_setting.")
	switch key {
	case "enabled", "dry_run":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		return nil
	case "window_seconds":
		return checkProbeGuardIntRange(key, value, 10, 3600)
	case "distinct_model_count":
		return checkProbeGuardIntRange(key, value, 2, 1000)
	case "first_ip_ban_minutes", "second_ip_ban_minutes":
		return checkProbeGuardIntRange(key, value, 1, 43200)
	case "permanent_offense_count":
		return checkProbeGuardIntRange(key, value, 1, 100)
	case "offense_dedupe_seconds":
		return checkProbeGuardIntRange(key, value, 10, 3600)
	case "max_ips_per_offense":
		return checkProbeGuardIntRange(key, value, 1, 1024)
	default:
		return nil
	}
}

func checkProbeGuardIntRange(key string, value string, min int, max int) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s must be an integer", key)
	}
	if parsed < min || parsed > max {
		return fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return nil
}
