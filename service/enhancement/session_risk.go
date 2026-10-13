package enhancement

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

type SharedSessionRisk struct {
	Kind         string           `json:"kind"`
	Identifier   string           `json:"identifier"`
	Sources      []string         `json:"sources"`
	UserCount    int64            `json:"user_count"`
	TokenCount   int64            `json:"token_count"`
	RequestCount int64            `json:"request_count"`
	ErrorCount   int64            `json:"error_count"`
	Quota        int64            `json:"quota"`
	FirstSeenAt  int64            `json:"first_seen_at"`
	LastSeenAt   int64            `json:"last_seen_at"`
	Users        []IPRiskUserRef  `json:"users"`
	Tokens       []IPRiskTokenRef `json:"tokens"`
	IPs          []string         `json:"ips"`
}

type SharedSessionPage struct {
	PageResult[SharedSessionRisk]
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type sessionRiskStats struct {
	Stats      ipRiskLogStats `gorm:"embedded"`
	Identifier string
	Hash       string
	Source     string
}

func sessionRiskColumns(kind string) (string, string, string, error) {
	switch kind {
	case "session":
		return "session_id", "session_hash", "session_source", nil
	case "cache":
		return "cache_key", "cache_hash", "cache_source", nil
	default:
		return "", "", "", errors.New("invalid session risk kind")
	}
}

func listSessionRiskStats(kind string, start, end int64, identifier string) ([]sessionRiskStats, error) {
	idCol, hashCol, sourceCol, err := sessionRiskColumns(kind)
	if err != nil {
		return nil, err
	}
	// Columns come only from the allowlist above. Aggregate the complete window;
	// never limit raw requests before identifying accounts that share an ID.
	tx := model.LOG_DB.Model(&model.Log{}).
		Select(fmt.Sprintf(`MIN(%s) AS identifier, %s AS hash, %s AS source,
			ip, token_id, token_name, user_id, username, COUNT(*) AS request_count,
			SUM(CASE WHEN type = ? THEN 1 ELSE 0 END) AS error_count,
			SUM(quota) AS quota, MIN(created_at) AS first_seen_at, MAX(created_at) AS last_seen_at`, idCol, hashCol, sourceCol), model.LogTypeError).
		Where(hashCol+" <> '' AND user_id > 0 AND type IN ? AND created_at >= ? AND created_at <= ?", []int{model.LogTypeConsume, model.LogTypeError}, start, end).
		Group(hashCol + ", " + sourceCol + ", ip, token_id, token_name, user_id, username")
	if identifier != "" {
		tx = tx.Where(hashCol+" = ?", common.SessionIdentifierHash(identifier))
	}
	var rows []sessionRiskStats
	err = tx.Find(&rows).Error
	return rows, err
}

func sharedSessionItems(rows []sessionRiskStats, kind string) []SharedSessionRisk {
	type bucket struct {
		item         SharedSessionRisk
		users        map[int]*IPRiskUserRef
		tokens       map[int]*IPRiskTokenRef
		ips, sources map[string]struct{}
	}
	buckets := make(map[string]*bucket)
	for _, row := range rows {
		stats := row.Stats
		b := buckets[row.Hash]
		if b == nil {
			b = &bucket{item: SharedSessionRisk{Kind: kind, Identifier: row.Identifier}, users: map[int]*IPRiskUserRef{}, tokens: map[int]*IPRiskTokenRef{}, ips: map[string]struct{}{}, sources: map[string]struct{}{}}
			buckets[row.Hash] = b
		}
		addCommonRiskStats(&b.item.RequestCount, &b.item.ErrorCount, &b.item.Quota, &b.item.FirstSeenAt, &b.item.LastSeenAt, stats)
		if b.users[stats.UserId] == nil {
			b.users[stats.UserId] = &IPRiskUserRef{UserId: stats.UserId, Username: stats.Username}
		}
		b.users[stats.UserId].RequestCount += stats.RequestCount
		if stats.TokenId > 0 {
			if b.tokens[stats.TokenId] == nil {
				b.tokens[stats.TokenId] = &IPRiskTokenRef{TokenId: stats.TokenId, TokenName: stats.TokenName, UserId: stats.UserId, Username: stats.Username}
			}
			b.tokens[stats.TokenId].RequestCount += stats.RequestCount
		}
		if stats.IP != "" {
			b.ips[stats.IP] = struct{}{}
		}
		if row.Source != "" {
			b.sources[row.Source] = struct{}{}
		}
	}
	items := make([]SharedSessionRisk, 0, len(buckets))
	for _, b := range buckets {
		if len(b.users) < 2 {
			continue
		}
		b.item.UserCount, b.item.TokenCount = int64(len(b.users)), int64(len(b.tokens))
		b.item.Users, b.item.Tokens = sortedIPRiskUsers(b.users), sortedIPRiskTokens(b.tokens)
		b.item.IPs, b.item.Sources = sortedIPStrings(b.ips), sortedIPStrings(b.sources)
		items = append(items, b.item)
	}
	return items
}

func SharedSessions(kind string, query IPRiskQuery) (SharedSessionPage, error) {
	query = normalizeIPRiskQuery(query)
	rows, err := listSessionRiskStats(kind, query.Start, query.End, "")
	if err != nil {
		return SharedSessionPage{}, err
	}
	items := make([]SharedSessionRisk, 0)
	for _, item := range sharedSessionItems(rows, kind) {
		// Reuse existing user/token keyword and column-filter semantics only after
		// building the complete association, so a search cannot hide its other users.
		ref := SharedTokenIPRisk{IP: item.Identifier, Users: item.Users, Tokens: item.Tokens, UserCount: item.UserCount, TokenCount: item.TokenCount, RequestCount: item.RequestCount, ErrorCount: item.ErrorCount, Quota: item.Quota, FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt}
		if !sharedTokenIPMatchesKeyword(ref, strings.ToLower(query.Keyword)) && !matchesKeyword(query.Keyword, strings.Join(item.IPs, " "), strings.Join(item.Sources, " ")) {
			continue
		}
		if !sharedTokenIPMatchesFilters(ref, query.Filters) || !matchesFilters(query.Filters, map[string]func(string) bool{
			"identifier": matchText(item.Identifier), "sources": matchText(strings.Join(item.Sources, " ")), "ips": matchText(strings.Join(item.IPs, " ")),
		}) {
			continue
		}
		items = append(items, item)
	}
	numeric := func(item SharedSessionRisk, key string) int64 {
		switch key {
		case "token_count":
			return item.TokenCount
		case "request_count":
			return item.RequestCount
		case "error_count":
			return item.ErrorCount
		case "quota":
			return item.Quota
		case "first_seen_at":
			return item.FirstSeenAt
		case "last_seen_at":
			return item.LastSeenAt
		default:
			return item.UserCount
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if query.Sort == "identifier" {
			if items[i].Identifier != items[j].Identifier {
				if query.Order == "asc" {
					return items[i].Identifier < items[j].Identifier
				}
				return items[i].Identifier > items[j].Identifier
			}
		} else if cmp := compareInt(numeric(items[i], query.Sort), numeric(items[j], query.Sort), query.Order != "asc"); cmp != 0 {
			return cmp < 0
		}
		if items[i].LastSeenAt != items[j].LastSeenAt {
			return items[i].LastSeenAt > items[j].LastSeenAt
		}
		return items[i].Identifier < items[j].Identifier
	})
	return SharedSessionPage{PageResult: pageResult(items, query.Page, query.PageSize), Start: query.Start, End: query.End}, nil
}

type SessionRiskBanRequest struct {
	BanUserRequest
	Kind       string `json:"kind"`
	Identifier string `json:"identifier"`
	Start      int64  `json:"start"`
	End        int64  `json:"end"`
}

func BanSharedSessionUsers(req SessionRiskBanRequest, operatorID, operatorRole int) (map[string]interface{}, error) {
	identifier := common.NormalizeSessionIdentifier(req.Identifier)
	if identifier == "" {
		return nil, errors.New("invalid session identifier")
	}
	if req.Start <= 0 || req.End < req.Start {
		return nil, errors.New("a valid session risk time window is required")
	}
	selected, hasSelected, err := selectedUserIDSet(req.UserIds)
	if err != nil {
		return nil, err
	}
	if !hasSelected || len(selected) == 0 || len(selected) > MaxBatchOperation {
		return nil, errors.New("select a valid number of associated users")
	}
	period, err := model.NewUserDisablePeriod(int64(req.DurationMinutes), time.Now().Unix())
	if err != nil {
		return nil, err
	}
	rows, err := listSessionRiskStats(req.Kind, req.Start, req.End, identifier)
	if err != nil {
		return nil, err
	}
	items := sharedSessionItems(rows, req.Kind)
	if len(items) != 1 || items[0].Identifier != identifier {
		return nil, errors.New("no shared session found in the selected window")
	}
	associated := map[int]bool{}
	for _, user := range items[0].Users {
		associated[user.UserId] = true
	}
	for id := range selected {
		if !associated[id] {
			return nil, errors.New("selected user is not associated with this identifier in the selected window")
		}
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "shared " + req.Kind + " risk ban"
	}
	if len([]rune(reason)) > 255 {
		return nil, errors.New("reason is too long")
	}
	success := 0
	failures := make([]map[string]interface{}, 0)
	ids := make([]int, 0, len(selected))
	for id := range selected {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if err := banUserWithPeriod(id, operatorID, operatorRole, reason, period); err != nil {
			failures = append(failures, map[string]interface{}{"id": id, "message": err.Error()})
		} else {
			success++
		}
	}
	audit(operatorID, "enhancements.risk", "ban_shared_session_users", map[string]interface{}{
		"kind": req.Kind, "identifier": identifier, "start": req.Start, "end": req.End,
		"user_ids": ids, "reason": reason, "success": success, "failures": failures,
		"disable_duration_minutes": period.Minutes, "disable_until": period.Until,
	})
	return map[string]interface{}{"total_users": len(ids), "success": success, "failures": failures}, nil
}

func SessionRiskSettings() map[string]interface{} {
	return map[string]interface{}{"recording_enabled": setting.IsSessionRiskRecordingEnabled()}
}

func SaveSessionRiskSettings(enabled bool, operatorID int) (map[string]interface{}, error) {
	if err := model.UpdateOption("enhancement_setting.session_risk_recording_enabled", strconv.FormatBool(enabled)); err != nil {
		return nil, err
	}
	audit(operatorID, "enhancements.risk", "session_recording", map[string]interface{}{"recording_enabled": enabled})
	return SessionRiskSettings(), nil
}
