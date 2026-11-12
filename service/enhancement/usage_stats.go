package enhancement

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

type UsageStatRow struct {
	CreatedAt   int64  `json:"created_at"`
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	ModelName   string `json:"model_name"`
	Requests    int64  `json:"requests"`
	TokenUsed   int64  `json:"token_used"`
	Quota       int64  `json:"quota"`
	ActiveUsers int64  `json:"active_users"`
}

type UsageStatsSnapshot struct {
	Period                     string         `json:"period"`
	Start                      int64          `json:"start"`
	End                        int64          `json:"end"`
	UserID                     int            `json:"user_id"`
	GeneratedAt                int64          `json:"generated_at"`
	AggregationEnabled         bool           `json:"aggregation_enabled"`
	AggregationIntervalMinutes int            `json:"aggregation_interval_minutes"`
	Totals                     UsageStatRow   `json:"totals"`
	Trend                      []UsageStatRow `json:"trend"`
	Models                     []UsageStatRow `json:"models"`
	Users                      []UsageStatRow `json:"users"`
}

type usageStatsCacheEntry struct {
	data    *UsageStatsSnapshot
	expires time.Time
}

var usageStatsCache = struct {
	sync.Mutex
	entries map[string]usageStatsCacheEntry
}{entries: map[string]usageStatsCacheEntry{}}
var usageStatsFlights singleflight.Group

func GetUsageStats(period string, userID int) (*UsageStatsSnapshot, error) {
	if period == "" {
		period = "24h"
	}
	hours := map[string]int{"24h": 24, "7d": 168, "30d": 720}[period]
	if hours == 0 || userID < 0 {
		return nil, errors.New("invalid usage statistics period or user ID")
	}
	now := time.Now()
	end := now.Truncate(time.Hour).Unix() + 3600
	start := end - int64(hours)*3600
	key := fmt.Sprintf("%p:%s:%d:%d:%t:%d", model.DB, period, start, userID, common.DataExportEnabled, common.DataExportInterval)
	lookup := func() *UsageStatsSnapshot {
		usageStatsCache.Lock()
		defer usageStatsCache.Unlock()
		if e, ok := usageStatsCache.entries[key]; ok && time.Now().Before(e.expires) {
			return e.data
		}
		return nil
	}
	if data := lookup(); data != nil {
		return data, nil
	}
	value, err, _ := usageStatsFlights.Do(key, func() (interface{}, error) {
		if data := lookup(); data != nil {
			return data, nil
		}
		data, err := buildUsageStats(period, userID, start, end)
		if err != nil {
			return nil, err
		}
		usageStatsCache.Lock()
		for k, e := range usageStatsCache.entries {
			if !time.Now().Before(e.expires) {
				delete(usageStatsCache.entries, k)
			}
		}
		// Arbitrary administrator user filters must not grow this cache forever.
		if len(usageStatsCache.entries) >= 64 {
			var oldest string
			var expiry time.Time
			for k, e := range usageStatsCache.entries {
				if oldest == "" || e.expires.Before(expiry) {
					oldest, expiry = k, e.expires
				}
			}
			delete(usageStatsCache.entries, oldest)
		}
		usageStatsCache.entries[key] = usageStatsCacheEntry{data: data, expires: time.Now().Add(time.Minute)}
		usageStatsCache.Unlock()
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*UsageStatsSnapshot), nil
}

// The statistics page reads the hourly aggregates already populated by
// LogQuotaData. No request to this endpoint reads or modifies relay logs.
func buildUsageStats(period string, userID int, start, end int64) (*UsageStatsSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db := model.DB.WithContext(ctx)
	base := func() *gorm.DB {
		q := db.Model(&model.QuotaData{}).Where("created_at >= ? AND created_at < ?", start, end)
		if userID > 0 {
			q = q.Where("user_id = ?", userID)
		}
		return q
	}
	sums := "COALESCE(SUM(" + quotedColumn(db, "count") + "),0) AS requests, COALESCE(SUM(token_used),0) AS token_used, COALESCE(SUM(quota),0) AS quota"
	result := &UsageStatsSnapshot{Period: period, Start: start, End: end, UserID: userID, GeneratedAt: time.Now().Unix(), AggregationEnabled: common.DataExportEnabled, AggregationIntervalMinutes: common.DataExportInterval, Trend: []UsageStatRow{}, Models: []UsageStatRow{}, Users: []UsageStatRow{}}
	if err := base().Select(sums + ", COUNT(DISTINCT user_id) AS active_users").Find(&result.Totals).Error; err != nil {
		return nil, err
	}
	var hours []UsageStatRow
	if err := base().Select("created_at, " + sums).Group("created_at").Order("created_at").Find(&hours).Error; err != nil {
		return nil, err
	}
	byHour := make(map[int64]UsageStatRow, len(hours))
	for _, row := range hours {
		byHour[row.CreatedAt] = row
	}
	for ts := start; ts < end; ts += 3600 {
		row := byHour[ts]
		row.CreatedAt = ts
		result.Trend = append(result.Trend, row)
	}
	if err := base().Select("model_name, " + sums).Group("model_name").Order("quota DESC, model_name ASC").Limit(20).Find(&result.Models).Error; err != nil {
		return nil, err
	}
	if err := base().Select("user_id, MAX(username) AS username, " + sums).Group("user_id").Order("quota DESC, user_id ASC").Limit(20).Find(&result.Users).Error; err != nil {
		return nil, err
	}
	// Display the current name without splitting historical totals when a user
	// renames their account. Deleted users retain the aggregate's saved name.
	ids := make([]int, 0, len(result.Users))
	for _, row := range result.Users {
		ids = append(ids, row.UserID)
	}
	if len(ids) > 0 {
		var users []model.User
		if err := db.Model(&model.User{}).Select("id, username").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return nil, err
		}
		names := map[int]string{}
		for _, u := range users {
			names[u.Id] = u.Username
		}
		for i := range result.Users {
			if name, ok := names[result.Users[i].UserID]; ok {
				result.Users[i].Username = name
			}
		}
	}
	return result, nil
}
