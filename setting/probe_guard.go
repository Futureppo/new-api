package setting

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"golang.org/x/text/unicode/norm"
)

const DefaultProbeGuardTextRules = "Reply with exactly OK\nreply exactly: OK\nReply with exactly: OK\nsay OK\nREPLY `PONG` ONLY\n回复两个字：可用\n请回复\"API 连接正常\"，并告诉我你的模型名称。"
const DefaultProbeGuardFirstReason = "因测活请求，账号已暂停使用 {minutes} 分钟，将于 {until}（北京时间）恢复。请关闭客户端自动测活功能。"
const DefaultProbeGuardSecondReason = "再次检测到测活请求，账号已暂停使用 {minutes} 分钟，将于 {until}（北京时间）恢复。再次违规将永久封禁。"
const DefaultProbeGuardPermanentReason = "因多次测活，账号已被永久封禁。如需申诉，请联系管理员。"
const ProbeGuardMaxRulesBytes = 128 * 1024

type ProbeGuardSetting struct {
	Enabled              bool   `json:"enabled"`
	DryRun               bool   `json:"dry_run"`
	TextRules            string `json:"text_rules"`
	ProbePingEnabled     bool   `json:"probe_ping_enabled"`
	BatchWindowSeconds   int    `json:"batch_window_seconds"`
	BatchRequestCount    int    `json:"batch_request_count"`
	FirstUserBanMinutes  int    `json:"first_user_ban_minutes"`
	SecondUserBanMinutes int    `json:"second_user_ban_minutes"`
	FirstBanReason       string `json:"first_ban_reason"`
	SecondBanReason      string `json:"second_ban_reason"`
	PermanentBanReason   string `json:"permanent_ban_reason"`
	// Legacy IP settings remain readable for upgrades, but are not used by the account guard.
	WindowSeconds         int    `json:"window_seconds"`
	DistinctModelCount    int    `json:"distinct_model_count"`
	FirstIPBanMinutes     int    `json:"first_ip_ban_minutes"`
	SecondIPBanMinutes    int    `json:"second_ip_ban_minutes"`
	PermanentOffenseCount int    `json:"permanent_offense_count"`
	OffenseDedupeSeconds  int    `json:"offense_dedupe_seconds"`
	MaxIPsPerOffense      int    `json:"max_ips_per_offense"`
	WhitelistUserIDs      string `json:"whitelist_user_ids"`
}

var probeGuardSetting = ProbeGuardSetting{
	Enabled:               false,
	DryRun:                true,
	TextRules:             DefaultProbeGuardTextRules,
	ProbePingEnabled:      true,
	BatchWindowSeconds:    30,
	BatchRequestCount:     3,
	FirstUserBanMinutes:   10,
	SecondUserBanMinutes:  60,
	FirstBanReason:        DefaultProbeGuardFirstReason,
	SecondBanReason:       DefaultProbeGuardSecondReason,
	PermanentBanReason:    DefaultProbeGuardPermanentReason,
	WindowSeconds:         60,
	DistinctModelCount:    5,
	FirstIPBanMinutes:     10,
	SecondIPBanMinutes:    60,
	PermanentOffenseCount: 3,
	OffenseDedupeSeconds:  60,
	MaxIPsPerOffense:      32,
	WhitelistUserIDs:      "",
}

func init() {
	config.GlobalConfig.Register("probe_guard_setting", &probeGuardSetting)
}

func GetProbeGuardSetting() ProbeGuardSetting {
	// Production option updates hold this lock while updating the registered struct.
	common.OptionMapRWMutex.RLock()
	cfg := probeGuardSetting
	common.OptionMapRWMutex.RUnlock()
	cfg.Normalize()
	return cfg
}

func (cfg *ProbeGuardSetting) Normalize() {
	// Separate keys prevent old IP-guard thresholds from overriding account defaults.
	if cfg.BatchWindowSeconds < 1 || cfg.BatchWindowSeconds > 3600 {
		cfg.BatchWindowSeconds = 30
	}
	if cfg.BatchRequestCount < 1 || cfg.BatchRequestCount > 1000 {
		cfg.BatchRequestCount = 3
	}
	if cfg.FirstUserBanMinutes < 1 || cfg.FirstUserBanMinutes > 43200 {
		cfg.FirstUserBanMinutes = 10
	}
	if cfg.SecondUserBanMinutes < 1 || cfg.SecondUserBanMinutes > 43200 {
		cfg.SecondUserBanMinutes = 60
	}
	if strings.TrimSpace(cfg.FirstBanReason) == "" {
		cfg.FirstBanReason = DefaultProbeGuardFirstReason
	}
	if strings.TrimSpace(cfg.SecondBanReason) == "" {
		cfg.SecondBanReason = DefaultProbeGuardSecondReason
	}
	if strings.TrimSpace(cfg.PermanentBanReason) == "" {
		cfg.PermanentBanReason = DefaultProbeGuardPermanentReason
	}
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
	cfg.WhitelistUserIDs = strings.TrimSpace(cfg.WhitelistUserIDs)
}

func (cfg ProbeGuardSetting) IsUserWhitelisted(userId int) bool {
	if userId <= 0 {
		return false
	}
	ids, err := ParseProbeGuardWhitelistUserIDs(cfg.WhitelistUserIDs)
	if err != nil {
		return false
	}
	_, ok := ids[userId]
	return ok
}

func ParseProbeGuardWhitelistUserIDs(raw string) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result, nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		id, err := strconv.Atoi(field)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("whitelist user id %q is invalid", field)
		}
		result[id] = struct{}{}
	}
	return result, nil
}

func CheckProbeGuardOption(key string, value string) error {
	key = strings.TrimPrefix(key, "probe_guard_setting.")
	switch key {
	case "enabled", "dry_run", "probe_ping_enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		return nil
	case "window_seconds":
		return checkProbeGuardIntRange(key, value, 10, 3600)
	case "batch_window_seconds":
		return checkProbeGuardIntRange(key, value, 1, 3600)
	case "batch_request_count":
		return checkProbeGuardIntRange(key, value, 1, 1000)
	case "distinct_model_count":
		return checkProbeGuardIntRange(key, value, 2, 1000)
	case "first_ip_ban_minutes", "second_ip_ban_minutes", "first_user_ban_minutes", "second_user_ban_minutes":
		return checkProbeGuardIntRange(key, value, 1, 43200)
	case "permanent_offense_count":
		return checkProbeGuardIntRange(key, value, 1, 100)
	case "offense_dedupe_seconds":
		return checkProbeGuardIntRange(key, value, 10, 3600)
	case "max_ips_per_offense":
		return checkProbeGuardIntRange(key, value, 1, 1024)
	case "whitelist_user_ids":
		_, err := ParseProbeGuardWhitelistUserIDs(value)
		return err
	case "text_rules":
		_, err := ParseProbeGuardTextRules(value)
		return err
	case "first_ban_reason", "second_ban_reason", "permanent_ban_reason":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("封禁原因不能为空")
		}
		expanded := RenderProbeGuardReason(value, 3, 43200, 253402185599)
		if strings.ContainsAny(expanded, "{}") {
			return fmt.Errorf("封禁原因仅支持 {count}、{minutes}、{until} 变量")
		}
		if utf8.RuneCountInString(expanded) > 255 {
			return fmt.Errorf("封禁原因展开变量后不能超过255个字符")
		}
		return nil
	default:
		return nil
	}
}

func NormalizeProbeGuardText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(text))), " ")
}

// An empty list deliberately disables text matching; it must not restore defaults.
func ParseProbeGuardTextRules(raw string) (map[string]string, error) {
	if len(raw) > ProbeGuardMaxRulesBytes {
		return nil, fmt.Errorf("匹配列表不能超过128 KiB")
	}
	rules := make(map[string]string)
	count := 0
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		count++
		if count > 256 || utf8.RuneCountInString(line) > 1024 {
			return nil, fmt.Errorf("匹配列表最多256条，每条最多1024个字符")
		}
		key := NormalizeProbeGuardText(line)
		if _, exists := rules[key]; !exists {
			rules[key] = line
		}
	}
	return rules, nil
}

func RenderProbeGuardReason(template string, count int, minutes, until int64) string {
	deadline := "永久"
	if until > 0 {
		deadline = time.Unix(until, 0).In(time.FixedZone("UTC+8", 8*60*60)).Format("2006-01-02 15:04:05")
	}
	return strings.NewReplacer("{count}", strconv.Itoa(count), "{minutes}", strconv.FormatInt(minutes, 10), "{until}", deadline).Replace(strings.TrimSpace(template))
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
