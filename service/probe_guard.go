package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const probeGuardKeyPrefix = "probe_guard"

type probeGuardEvent struct {
	timestamp int64
	model     string
}

type probeGuardSnapshot struct {
	models []string
	ips    []string
}

var (
	probeGuardWindowLock sync.Mutex
	probeGuardWindows    = map[string][]probeGuardEvent{}
	probeGuardCooldowns  = map[string]int64{}
)

func CheckProbeGuard(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	cfg := setting.GetProbeGuardSetting()
	if !cfg.Enabled || relayInfo == nil || relayInfo.UserId <= 0 {
		return nil
	}
	if isProbeGuardExcludedUser(c, relayInfo.UserId, cfg) {
		return nil
	}

	modelName := strings.TrimSpace(relayInfo.OriginModelName)
	if modelName == "" {
		return nil
	}

	clientIP := ""
	if c != nil {
		clientIP = strings.TrimSpace(c.ClientIP())
	}
	targetIP, ok := normalizePublicProbeGuardIP(clientIP)
	if !ok {
		return nil
	}

	snapshot, triggered, err := recordProbeGuardRequest(c, cfg, targetIP, modelName)
	if err != nil {
		common.SysLog(fmt.Sprintf("probe guard failed open for ip %s user %d: %s", targetIP, relayInfo.UserId, err.Error()))
		return nil
	}
	if !triggered {
		return nil
	}

	if cfg.DryRun {
		recordProbeGuardLog(c, relayInfo.UserId, 0, true, "dry_run", snapshot.models, snapshot.ips)
		return nil
	}

	state, err := model.IncrementProbeIPAbuseOffense(targetIP, relayInfo.UserId, snapshot.models)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeUpdateDataError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}

	action, err := applyProbeGuardPenalty(targetIP, relayInfo.UserId, state.OffenseCount, cfg)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeUpdateDataError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	recordProbeGuardLog(c, relayInfo.UserId, state.OffenseCount, false, action, snapshot.models, snapshot.ips)

	statusCode := http.StatusTooManyRequests
	message := "bulk model probing detected"
	if state.OffenseCount >= cfg.PermanentOffenseCount {
		statusCode = http.StatusForbidden
		message = "bulk model probing detected; IP banned"
	}
	return types.NewErrorWithStatusCode(
		errors.New(message),
		types.ErrorCodeBulkProbeDetected,
		statusCode,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func isProbeGuardExcludedUser(c *gin.Context, userId int, cfg setting.ProbeGuardSetting) bool {
	if cfg.IsUserWhitelisted(userId) {
		return true
	}
	if c != nil && c.GetInt("role") >= common.RoleAdminUser {
		return true
	}
	return model.IsAdmin(userId)
}

func recordProbeGuardRequest(c *gin.Context, cfg setting.ProbeGuardSetting, targetIP string, modelName string) (probeGuardSnapshot, bool, error) {
	if common.RedisEnabled && common.RDB != nil {
		ctx := context.Background()
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		return recordRedisProbeGuardRequest(ctx, cfg, targetIP, modelName)
	}
	snapshot, triggered := recordMemoryProbeGuardRequest(cfg, targetIP, modelName)
	return snapshot, triggered, nil
}

func recordRedisProbeGuardRequest(ctx context.Context, cfg setting.ProbeGuardSetting, targetIP string, modelName string) (probeGuardSnapshot, bool, error) {
	now := common.GetTimestamp()
	cutoff := now - int64(cfg.WindowSeconds)
	ttl := time.Duration(maxProbeGuardInt(cfg.WindowSeconds, cfg.OffenseDedupeSeconds)+60) * time.Second
	targetKey := probeGuardRedisTargetKey(targetIP)
	modelsKey := fmt.Sprintf("%s:models:ip:%s", probeGuardKeyPrefix, targetKey)
	cooldownKey := fmt.Sprintf("%s:cooldown:ip:%s", probeGuardKeyPrefix, targetKey)

	pipe := common.RDB.TxPipeline()
	pipe.ZRemRangeByScore(ctx, modelsKey, "-inf", strconv.FormatInt(cutoff, 10))
	pipe.ZAdd(ctx, modelsKey, &redis.Z{Score: float64(now), Member: modelName})
	pipe.Expire(ctx, modelsKey, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return probeGuardSnapshot{}, false, err
	}

	models, err := common.RDB.ZRange(ctx, modelsKey, 0, -1).Result()
	if err != nil {
		return probeGuardSnapshot{}, false, err
	}

	snapshot := probeGuardSnapshot{
		models: uniqueSortedStrings(models),
		ips:    []string{targetIP},
	}
	if len(snapshot.models) < cfg.DistinctModelCount {
		return snapshot, false, nil
	}
	acquired, err := common.RDB.SetNX(ctx, cooldownKey, "1", time.Duration(cfg.OffenseDedupeSeconds)*time.Second).Result()
	if err != nil || !acquired {
		return snapshot, false, err
	}
	return snapshot, true, nil
}

func recordMemoryProbeGuardRequest(cfg setting.ProbeGuardSetting, targetIP string, modelName string) (probeGuardSnapshot, bool) {
	now := common.GetTimestamp()
	cutoff := now - int64(cfg.WindowSeconds)

	probeGuardWindowLock.Lock()
	defer probeGuardWindowLock.Unlock()

	events := probeGuardWindows[targetIP]
	kept := events[:0]
	for _, event := range events {
		if event.timestamp > cutoff {
			kept = append(kept, event)
		}
	}
	kept = append(kept, probeGuardEvent{timestamp: now, model: modelName})
	probeGuardWindows[targetIP] = kept

	models := make([]string, 0, len(kept))
	for _, event := range kept {
		if event.model != "" {
			models = append(models, event.model)
		}
	}
	snapshot := probeGuardSnapshot{
		models: uniqueSortedStrings(models),
		ips:    []string{targetIP},
	}
	if len(snapshot.models) < cfg.DistinctModelCount {
		return snapshot, false
	}
	if probeGuardCooldowns[targetIP] > now {
		return snapshot, false
	}
	probeGuardCooldowns[targetIP] = now + int64(cfg.OffenseDedupeSeconds)
	return snapshot, true
}

func applyProbeGuardPenalty(targetIP string, userId int, offenseCount int, cfg setting.ProbeGuardSetting) (string, error) {
	now := common.GetTimestamp()
	reason := fmt.Sprintf("bulk probe guard: ip %s requested %d distinct models in %ds (last_user %d, offense %d)", targetIP, cfg.DistinctModelCount, cfg.WindowSeconds, userId, offenseCount)
	expiresAt := int64(0)
	action := "permanent_ip_ban"

	switch {
	case offenseCount >= cfg.PermanentOffenseCount:
	case offenseCount == 1:
		expiresAt = now + int64(cfg.FirstIPBanMinutes*60)
		action = fmt.Sprintf("temporary_ip_ban_%dm", cfg.FirstIPBanMinutes)
	default:
		expiresAt = now + int64(cfg.SecondIPBanMinutes*60)
		action = fmt.Sprintf("temporary_ip_ban_%dm", cfg.SecondIPBanMinutes)
	}

	if err := model.UpsertProbeGuardIPBan(targetIP, reason, expiresAt); err != nil {
		return action, err
	}
	model.InitIPBanCache()
	return action, nil
}

func recordProbeGuardLog(c *gin.Context, userId int, offenseCount int, dryRun bool, action string, models []string, ips []string) {
	content := fmt.Sprintf(
		"bulk probe guard triggered: offense=%d dry_run=%t action=%s models=%s ips=%s",
		offenseCount,
		dryRun,
		action,
		strings.Join(limitProbeGuardStrings(models, 12), ","),
		strings.Join(limitProbeGuardStrings(ips, 12), ","),
	)
	model.RecordLogWithContext(c, userId, model.LogTypeManage, content)
}

func normalizePublicProbeGuardIP(raw string) (string, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return "", false
	}
	for _, prefix := range probeGuardBlockedPrefixes() {
		if prefix.Contains(addr) {
			return "", false
		}
	}
	return addr.String(), true
}

func probeGuardBlockedPrefixes() []netip.Prefix {
	prefixes := []string{
		"0.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::/128",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
		"2001:db8::/32",
	}
	out := make([]netip.Prefix, 0, len(prefixes))
	for _, raw := range prefixes {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil {
			out = append(out, prefix)
		}
	}
	return out
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func limitProbeGuardStrings(values []string, maxCount int) []string {
	if maxCount <= 0 || len(values) <= maxCount {
		return values
	}
	out := append([]string{}, values[:maxCount]...)
	out = append(out, "...")
	return out
}

func maxProbeGuardInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func probeGuardRedisTargetKey(targetIP string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(targetIP))
}

func resetProbeGuardMemoryForTest() {
	probeGuardWindowLock.Lock()
	defer probeGuardWindowLock.Unlock()
	probeGuardWindows = map[string][]probeGuardEvent{}
	probeGuardCooldowns = map[string]int64{}
}
