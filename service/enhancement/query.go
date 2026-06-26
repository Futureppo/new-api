package enhancement

import (
	"strconv"
	"strings"
)

type ListQuery struct {
	Page     int
	PageSize int
	Keyword  string
	Sort     string
	Order    string
	Filters  map[string]string
}

func normalizeListQuery(query ListQuery) ListQuery {
	query.Page = clampPage(query.Page)
	query.PageSize = clampLimit(query.PageSize)
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.Sort = strings.TrimSpace(query.Sort)
	query.Order = normalizeSortOrder(query.Order)
	if query.Filters == nil {
		query.Filters = map[string]string{}
	}
	for key, value := range query.Filters {
		value = strings.TrimSpace(value)
		if value == "" {
			delete(query.Filters, key)
			continue
		}
		query.Filters[key] = value
	}
	return query
}

func pageResult[T any](items []T, page int, pageSize int) PageResult[T] {
	page = clampPage(page)
	pageSize = clampLimit(pageSize)
	total := int64(len(items))
	return PageResult[T]{
		Items:    paginateSlice(items, page, pageSize),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
}

func lowerContains(value string, keyword string) bool {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(value), keyword)
}

func matchesKeyword(keyword string, values ...string) bool {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return true
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), keyword) {
			return true
		}
	}
	return false
}

func matchText(value string) func(string) bool {
	return func(filter string) bool {
		return lowerContains(value, filter)
	}
}

func matchInt(value int64) func(string) bool {
	return func(filter string) bool {
		number, err := strconv.ParseInt(strings.TrimSpace(filter), 10, 64)
		if err != nil {
			return false
		}
		return value == number
	}
}

func matchFloat(value float64) func(string) bool {
	return func(filter string) bool {
		number, err := strconv.ParseFloat(strings.TrimSpace(filter), 64)
		if err != nil {
			return false
		}
		return value == number
	}
}

func matchBool(value bool) func(string) bool {
	return func(filter string) bool {
		normalized := strings.ToLower(strings.TrimSpace(filter))
		switch normalized {
		case "true", "1", "yes", "是", "开启":
			return value
		case "false", "0", "no", "否", "关闭":
			return !value
		default:
			return false
		}
	}
}

func matchesFilters(filters map[string]string, matchers map[string]func(string) bool) bool {
	for key, value := range filters {
		if strings.TrimSpace(value) == "" {
			continue
		}
		matcher, ok := matchers[key]
		if !ok {
			continue
		}
		if !matcher(value) {
			return false
		}
	}
	return true
}

func sortDesc(order string) bool {
	return !strings.EqualFold(strings.TrimSpace(order), "asc")
}

func compareInt(left int64, right int64, desc bool) int {
	if left == right {
		return 0
	}
	if desc {
		if left > right {
			return -1
		}
		return 1
	}
	if left < right {
		return -1
	}
	return 1
}

func compareFloat(left float64, right float64, desc bool) int {
	if left == right {
		return 0
	}
	if desc {
		if left > right {
			return -1
		}
		return 1
	}
	if left < right {
		return -1
	}
	return 1
}

func compareString(left string, right string, desc bool) int {
	result := strings.Compare(strings.ToLower(left), strings.ToLower(right))
	if desc {
		return -result
	}
	return result
}
