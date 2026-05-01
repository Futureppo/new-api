package enhancement

import (
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

const maxRiskLogRows = 100000

type ipRiskLogRow struct {
	IP        string `gorm:"column:ip"`
	TokenId   int    `gorm:"column:token_id"`
	TokenName string `gorm:"column:token_name"`
	UserId    int    `gorm:"column:user_id"`
	Username  string `gorm:"column:username"`
	Type      int    `gorm:"column:type"`
	Quota     int    `gorm:"column:quota"`
	CreatedAt int64  `gorm:"column:created_at"`
}

type sharedTokenIPBucket struct {
	item      SharedTokenIPRisk
	users     map[int]*IPRiskUserRef
	tokens    map[int]*IPRiskTokenRef
	userSet   map[int]struct{}
	tokenSet  map[int]struct{}
	lowerText string
}

type tokenMultiIPBucket struct {
	item      TokenMultiIPRisk
	ips       map[string]struct{}
	lowerText string
}

func SharedTokenIPs(query IPRiskQuery) (PageResult[SharedTokenIPRisk], error) {
	query = normalizeIPRiskQuery(query)
	rows, err := listIPRiskLogs(query.Start, query.End)
	if err != nil {
		return PageResult[SharedTokenIPRisk]{}, err
	}

	buckets := make(map[string]*sharedTokenIPBucket)
	for _, row := range rows {
		if row.TokenId <= 0 || row.UserId <= 0 {
			continue
		}
		bucket, ok := buckets[row.IP]
		if !ok {
			bucket = &sharedTokenIPBucket{
				item: SharedTokenIPRisk{
					IP:          row.IP,
					FirstSeenAt: row.CreatedAt,
					LastSeenAt:  row.CreatedAt,
				},
				users:    map[int]*IPRiskUserRef{},
				tokens:   map[int]*IPRiskTokenRef{},
				userSet:  map[int]struct{}{},
				tokenSet: map[int]struct{}{},
			}
			buckets[row.IP] = bucket
		}
		addCommonRiskStats(&bucket.item.RequestCount, &bucket.item.ErrorCount, &bucket.item.Quota, &bucket.item.FirstSeenAt, &bucket.item.LastSeenAt, row)
		bucket.userSet[row.UserId] = struct{}{}
		bucket.tokenSet[row.TokenId] = struct{}{}
		if user, ok := bucket.users[row.UserId]; ok {
			user.RequestCount++
			if user.Username == "" {
				user.Username = row.Username
			}
		} else {
			bucket.users[row.UserId] = &IPRiskUserRef{
				UserId:       row.UserId,
				Username:     row.Username,
				RequestCount: 1,
			}
		}
		if token, ok := bucket.tokens[row.TokenId]; ok {
			token.RequestCount++
			if token.TokenName == "" {
				token.TokenName = row.TokenName
			}
			if token.Username == "" {
				token.Username = row.Username
			}
			if token.UserId == 0 {
				token.UserId = row.UserId
			}
		} else {
			bucket.tokens[row.TokenId] = &IPRiskTokenRef{
				TokenId:      row.TokenId,
				TokenName:    row.TokenName,
				UserId:       row.UserId,
				Username:     row.Username,
				RequestCount: 1,
			}
		}
	}

	keyword := strings.ToLower(strings.TrimSpace(query.Keyword))
	items := make([]SharedTokenIPRisk, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket.tokenSet) <= 1 || len(bucket.userSet) <= 1 {
			continue
		}
		bucket.item.TokenCount = int64(len(bucket.tokenSet))
		bucket.item.UserCount = int64(len(bucket.userSet))
		bucket.item.Users = sortedIPRiskUsers(bucket.users)
		bucket.item.Tokens = sortedIPRiskTokens(bucket.tokens)
		if !sharedTokenIPMatchesKeyword(bucket.item, keyword) {
			continue
		}
		items = append(items, bucket.item)
	}

	sortSharedTokenIPRisks(items, query.Sort, query.Order)
	total := int64(len(items))
	items = paginateSlice(items, query.Page, query.PageSize)
	return PageResult[SharedTokenIPRisk]{
		Items:    items,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func TokenMultiIPs(query IPRiskQuery) (PageResult[TokenMultiIPRisk], error) {
	query = normalizeIPRiskQuery(query)
	rows, err := listIPRiskLogs(query.Start, query.End)
	if err != nil {
		return PageResult[TokenMultiIPRisk]{}, err
	}

	buckets := make(map[int]*tokenMultiIPBucket)
	for _, row := range rows {
		if row.TokenId <= 0 {
			continue
		}
		bucket, ok := buckets[row.TokenId]
		if !ok {
			bucket = &tokenMultiIPBucket{
				item: TokenMultiIPRisk{
					TokenId:     row.TokenId,
					TokenName:   row.TokenName,
					UserId:      row.UserId,
					Username:    row.Username,
					FirstSeenAt: row.CreatedAt,
					LastSeenAt:  row.CreatedAt,
				},
				ips: map[string]struct{}{},
			}
			buckets[row.TokenId] = bucket
		}
		if bucket.item.TokenName == "" {
			bucket.item.TokenName = row.TokenName
		}
		if bucket.item.Username == "" {
			bucket.item.Username = row.Username
		}
		if bucket.item.UserId == 0 {
			bucket.item.UserId = row.UserId
		}
		bucket.ips[row.IP] = struct{}{}
		addCommonRiskStats(&bucket.item.RequestCount, &bucket.item.ErrorCount, &bucket.item.Quota, &bucket.item.FirstSeenAt, &bucket.item.LastSeenAt, row)
	}

	keyword := strings.ToLower(strings.TrimSpace(query.Keyword))
	items := make([]TokenMultiIPRisk, 0, len(buckets))
	for _, bucket := range buckets {
		if len(bucket.ips) <= 1 {
			continue
		}
		bucket.item.IPCount = int64(len(bucket.ips))
		bucket.item.IPs = sortedIPStrings(bucket.ips)
		if !tokenMultiIPMatchesKeyword(bucket.item, keyword) {
			continue
		}
		items = append(items, bucket.item)
	}

	sortTokenMultiIPRisks(items, query.Sort, query.Order)
	total := int64(len(items))
	items = paginateSlice(items, query.Page, query.PageSize)
	return PageResult[TokenMultiIPRisk]{
		Items:    items,
		Total:    total,
		Page:     query.Page,
		PageSize: query.PageSize,
	}, nil
}

func normalizeIPRiskQuery(query IPRiskQuery) IPRiskQuery {
	query.Page = clampPage(query.Page)
	query.PageSize = clampLimit(query.PageSize)
	query.Start, query.End = queryWindow(query.Start, query.End, MaxAdminQueryWindow)
	query.Sort = strings.TrimSpace(query.Sort)
	query.Order = normalizeSortOrder(query.Order)
	query.Keyword = strings.TrimSpace(query.Keyword)
	return query
}

func listIPRiskLogs(start int64, end int64) ([]ipRiskLogRow, error) {
	var rows []ipRiskLogRow
	err := model.LOG_DB.Model(&model.Log{}).
		Select("ip, token_id, token_name, user_id, username, type, quota, created_at").
		Where("ip <> '' AND type IN ? AND created_at >= ? AND created_at <= ?", []int{model.LogTypeConsume, model.LogTypeError}, start, end).
		Order("created_at DESC").
		Limit(maxRiskLogRows).
		Find(&rows).Error
	return rows, err
}

func addCommonRiskStats(requestCount *int64, errorCount *int64, quota *int64, firstSeenAt *int64, lastSeenAt *int64, row ipRiskLogRow) {
	*requestCount++
	if row.Type == model.LogTypeError {
		*errorCount++
	}
	*quota += int64(row.Quota)
	if *firstSeenAt == 0 || row.CreatedAt < *firstSeenAt {
		*firstSeenAt = row.CreatedAt
	}
	if row.CreatedAt > *lastSeenAt {
		*lastSeenAt = row.CreatedAt
	}
}

func normalizeSortOrder(order string) string {
	if strings.EqualFold(order, "asc") {
		return "asc"
	}
	return "desc"
}

func sortSharedTokenIPRisks(items []SharedTokenIPRisk, sortKey string, order string) {
	allowed := map[string]func(SharedTokenIPRisk) int64{
		"user_count":    func(item SharedTokenIPRisk) int64 { return item.UserCount },
		"token_count":   func(item SharedTokenIPRisk) int64 { return item.TokenCount },
		"request_count": func(item SharedTokenIPRisk) int64 { return item.RequestCount },
		"error_count":   func(item SharedTokenIPRisk) int64 { return item.ErrorCount },
		"quota":         func(item SharedTokenIPRisk) int64 { return item.Quota },
		"first_seen_at": func(item SharedTokenIPRisk) int64 { return item.FirstSeenAt },
		"last_seen_at":  func(item SharedTokenIPRisk) int64 { return item.LastSeenAt },
	}
	getValue, ok := allowed[sortKey]
	desc := order != "asc"
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if ok {
			leftValue := getValue(left)
			rightValue := getValue(right)
			if leftValue != rightValue {
				if desc {
					return leftValue > rightValue
				}
				return leftValue < rightValue
			}
		} else {
			if left.UserCount != right.UserCount {
				return left.UserCount > right.UserCount
			}
			if left.TokenCount != right.TokenCount {
				return left.TokenCount > right.TokenCount
			}
			if left.RequestCount != right.RequestCount {
				return left.RequestCount > right.RequestCount
			}
		}
		return left.IP < right.IP
	})
}

func sortTokenMultiIPRisks(items []TokenMultiIPRisk, sortKey string, order string) {
	allowed := map[string]func(TokenMultiIPRisk) int64{
		"ip_count":      func(item TokenMultiIPRisk) int64 { return item.IPCount },
		"request_count": func(item TokenMultiIPRisk) int64 { return item.RequestCount },
		"error_count":   func(item TokenMultiIPRisk) int64 { return item.ErrorCount },
		"quota":         func(item TokenMultiIPRisk) int64 { return item.Quota },
		"first_seen_at": func(item TokenMultiIPRisk) int64 { return item.FirstSeenAt },
		"last_seen_at":  func(item TokenMultiIPRisk) int64 { return item.LastSeenAt },
		"token_id":      func(item TokenMultiIPRisk) int64 { return int64(item.TokenId) },
	}
	getValue, ok := allowed[sortKey]
	desc := order != "asc"
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if ok {
			leftValue := getValue(left)
			rightValue := getValue(right)
			if leftValue != rightValue {
				if desc {
					return leftValue > rightValue
				}
				return leftValue < rightValue
			}
		} else {
			if left.IPCount != right.IPCount {
				return left.IPCount > right.IPCount
			}
			if left.RequestCount != right.RequestCount {
				return left.RequestCount > right.RequestCount
			}
		}
		return left.TokenId < right.TokenId
	})
}

func sortedIPRiskUsers(users map[int]*IPRiskUserRef) []IPRiskUserRef {
	out := make([]IPRiskUserRef, 0, len(users))
	for _, user := range users {
		out = append(out, *user)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RequestCount != out[j].RequestCount {
			return out[i].RequestCount > out[j].RequestCount
		}
		return out[i].UserId < out[j].UserId
	})
	return out
}

func sortedIPRiskTokens(tokens map[int]*IPRiskTokenRef) []IPRiskTokenRef {
	out := make([]IPRiskTokenRef, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, *token)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RequestCount != out[j].RequestCount {
			return out[i].RequestCount > out[j].RequestCount
		}
		return out[i].TokenId < out[j].TokenId
	})
	return out
}

func sortedIPStrings(ips map[string]struct{}) []string {
	out := make([]string, 0, len(ips))
	for ip := range ips {
		out = append(out, ip)
	}
	sort.Strings(out)
	return out
}

func sharedTokenIPMatchesKeyword(item SharedTokenIPRisk, keyword string) bool {
	if keyword == "" {
		return true
	}
	if strings.Contains(strings.ToLower(item.IP), keyword) {
		return true
	}
	if id, ok := parsePositiveKeywordID(keyword); ok {
		for _, user := range item.Users {
			if user.UserId == id {
				return true
			}
		}
		for _, token := range item.Tokens {
			if token.TokenId == id || token.UserId == id {
				return true
			}
		}
	}
	for _, user := range item.Users {
		if strings.Contains(strings.ToLower(user.Username), keyword) {
			return true
		}
	}
	for _, token := range item.Tokens {
		if strings.Contains(strings.ToLower(token.TokenName), keyword) || strings.Contains(strings.ToLower(token.Username), keyword) {
			return true
		}
	}
	return false
}

func tokenMultiIPMatchesKeyword(item TokenMultiIPRisk, keyword string) bool {
	if keyword == "" {
		return true
	}
	if strings.Contains(strings.ToLower(item.TokenName), keyword) || strings.Contains(strings.ToLower(item.Username), keyword) {
		return true
	}
	if id, ok := parsePositiveKeywordID(keyword); ok && (item.UserId == id || item.TokenId == id) {
		return true
	}
	for _, ip := range item.IPs {
		if strings.Contains(strings.ToLower(ip), keyword) {
			return true
		}
	}
	return false
}

func parsePositiveKeywordID(keyword string) (int, bool) {
	id, err := strconv.Atoi(keyword)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func paginateSlice[T any](items []T, page int, pageSize int) []T {
	page = clampPage(page)
	pageSize = clampLimit(pageSize)
	start := offset(page, pageSize)
	if start >= len(items) {
		return []T{}
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
