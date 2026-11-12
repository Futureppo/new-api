package enhancement

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Optional DSNs must point to empty, disposable databases, never production.
// MySQL 5.7 exercises the fallback without window functions.
func TestStatisticsDatabaseQueries(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(filepath.Join(t.TempDir(), "stats.db"))
			case "mysql":
				dsn := os.Getenv("TEST_STATS_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_STATS_MYSQL_DSN not set")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_STATS_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_STATS_POSTGRES_DSN not set")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			defer sqlDB.Close()
			for _, table := range []string{"logs", "quota_data", "users"} {
				require.False(t, db.Migrator().HasTable(table), "use an empty disposable database")
			}
			require.NoError(t, db.AutoMigrate(&model.Log{}, &model.QuotaData{}, &model.User{}))
			defer db.Migrator().DropTable(&model.Log{}, &model.QuotaData{}, &model.User{})
			previousDB, previousLogDB := model.DB, model.LOG_DB
			model.DB, model.LOG_DB = db, db
			defer func() { model.DB, model.LOG_DB = previousDB, previousLogDB }()
			testBoundedStatusAggregation(t, db)
			// A missing logs table proves usage reads never fall back to raw logs.
			require.NoError(t, db.Migrator().DropTable(&model.Log{}))
			testHourlyUsageAggregation(t, db)
		})
	}
}

func testBoundedStatusAggregation(t *testing.T, db *gorm.DB) {
	t.Helper()
	configureModelStatusIgnoredErrorKeywords(t, true, []string{"50%_OFF!"})
	window := modelStatusWindow{Start: 1000, End: 1120, SlotSeconds: 60, SlotCount: 2, Minutes: 2, Key: "test"}
	logs := []model.Log{
		{CreatedAt: 999, ModelName: "active", Group: "default", Type: model.LogTypeConsume, Other: `{"frt":9999}`},
		{CreatedAt: 1121, ModelName: "active", Group: "default", Type: model.LogTypeConsume, Other: `{"frt":9999}`},
		{CreatedAt: 1000, ModelName: "active", Group: "default", Type: model.LogTypeConsume, Quota: 10, Other: `{"frt":9999}`},
		{CreatedAt: 1060, ModelName: "active", Group: "default", Type: model.LogTypeError, Content: "50xxOFF!"},
		{CreatedAt: 1120, ModelName: "active", Group: "default", Type: model.LogTypeError, Other: `{"error":"50%_off!"}`},
		{CreatedAt: 1120, ModelName: "other", Group: "default", Type: model.LogTypeConsume, Other: `{"frt":9999}`},
		{CreatedAt: 1120, ModelName: "active", Group: "private", Type: model.LogTypeConsume, Other: `{"frt":9999}`},
	}
	// Same timestamp: the latest IDs must win, with exactly ten samples.
	for i := 0; i < 10; i++ {
		logs = append(logs, model.Log{CreatedAt: 1120, ModelName: "active", Group: "default", Type: model.LogTypeConsume, Quota: 2, PromptTokens: 3, CompletionTokens: 20, UseTime: 2, Other: `{"frt":1000}`})
	}
	require.NoError(t, db.Create(&logs).Error)
	var queries atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_status_reads", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries.Add(1)
		}
	}))
	defer db.Callback().Query().Remove("count_status_reads")
	targets := []modelStatusTarget{{Group: "default", Model: "active"}}
	for i := 0; i < 235; i++ {
		targets = append(targets, modelStatusTarget{Group: "default", Model: fmt.Sprintf("empty-%d", i)})
	}
	statuses, err := aggregateModelStatuses(targets, window)
	require.NoError(t, err)
	require.Len(t, statuses, 236)
	require.EqualValues(t, 3, queries.Load(), "empty models must not add log queries")
	s := statuses[0]
	require.EqualValues(t, 12, s.TotalRequests)
	require.EqualValues(t, 11, s.SuccessCount)
	require.EqualValues(t, 1, s.ErrorCount)
	require.EqualValues(t, 30, s.Quota)
	require.EqualValues(t, 30, s.PromptTokens)
	require.EqualValues(t, 200, s.CompletionTokens)
	require.EqualValues(t, 1, s.SlotData[0].TotalRequests)
	require.EqualValues(t, 11, s.SlotData[1].TotalRequests)
	require.EqualValues(t, 1120, s.LastRequestAt)
	require.InDelta(t, 1000, s.RecentAvgFirstResponseTime, .001)
	require.InDelta(t, 10, s.RecentAvgOutputTokenSpeed, .001)
	require.Zero(t, statuses[1].TotalRequests)
	queries.Store(0)
	_, err = aggregateModelStatuses(targets[1:], window)
	require.NoError(t, err)
	require.EqualValues(t, 1, queries.Load(), "no successful records means no sample lookup")
}

func testHourlyUsageAggregation(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "current-name"}).Error)
	rows := []model.QuotaData{
		{CreatedAt: 3600, UserID: 7, Username: "old-name", ModelName: "a", Count: 50001, Quota: 100, TokenUsed: 70000},
		{CreatedAt: 7200, UserID: 7, Username: "new-name", ModelName: "b", Count: 2, Quota: 200, TokenUsed: 30},
		{CreatedAt: 7200, UserID: 8, Username: "deleted-user", ModelName: "a", Count: 4, Quota: 50, TokenUsed: 20},
		{CreatedAt: 0, UserID: 7, ModelName: "outside", Count: 999},
		{CreatedAt: 14400, UserID: 7, ModelName: "outside", Count: 999},
	}
	require.NoError(t, db.Create(&rows).Error)
	data, err := buildUsageStats("24h", 0, 3600, 14400)
	require.NoError(t, err)
	require.EqualValues(t, 50007, data.Totals.Requests)
	require.EqualValues(t, 70050, data.Totals.TokenUsed)
	require.EqualValues(t, 350, data.Totals.Quota)
	require.EqualValues(t, 2, data.Totals.ActiveUsers)
	require.Len(t, data.Trend, 3)
	require.EqualValues(t, 0, data.Trend[2].Requests)
	require.EqualValues(t, 10800, data.Trend[2].CreatedAt)
	require.Len(t, data.Models, 2)
	require.Equal(t, "b", data.Models[0].ModelName)
	require.Len(t, data.Users, 2)
	require.Equal(t, "current-name", data.Users[0].Username)
	require.EqualValues(t, 50003, data.Users[0].Requests)
	require.Equal(t, "deleted-user", data.Users[1].Username)
	filtered, err := buildUsageStats("24h", 7, 3600, 14400)
	require.NoError(t, err)
	require.EqualValues(t, 50003, filtered.Totals.Requests)
	require.EqualValues(t, 300, filtered.Totals.Quota)
	require.Len(t, filtered.Users, 1)
	empty, err := buildUsageStats("24h", 999, 3600, 14400)
	require.NoError(t, err)
	require.Zero(t, empty.Totals.Requests)
	require.Empty(t, empty.Models)
	require.Empty(t, empty.Users)
}

func TestUsageStatsCacheCoalescesReadersAndExpires(t *testing.T) {
	db := setupEnhancementListTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}))
	var queries atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_usage_reads", func(tx *gorm.DB) {
		queries.Add(1)
		time.Sleep(5 * time.Millisecond)
	}))
	defer db.Callback().Query().Remove("count_usage_reads")
	var wg sync.WaitGroup
	results := make(chan *UsageStatsSnapshot, 20)
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := GetUsageStats("24h", 0)
			results <- data
			errors <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	for data := range results {
		require.Len(t, data.Trend, 24)
	}
	require.EqualValues(t, 4, queries.Load())
	_, err := GetUsageStats("24h", 0)
	require.NoError(t, err)
	require.EqualValues(t, 4, queries.Load())
	_, err = GetUsageStats("7d", 7)
	require.NoError(t, err)
	require.EqualValues(t, 8, queries.Load())
	usageStatsCache.Lock()
	for k, entry := range usageStatsCache.entries {
		entry.expires = time.Now().Add(-time.Second)
		usageStatsCache.entries[k] = entry
	}
	usageStatsCache.Unlock()
	data, err := GetUsageStats("24h", 0)
	require.NoError(t, err)
	require.EqualValues(t, 12, queries.Load())
	require.Equal(t, time.Now().Truncate(time.Hour).Unix()+3600, data.End)
	require.EqualValues(t, 24*3600, data.End-data.Start)
	for _, period := range []string{"all", "365d"} {
		_, err := GetUsageStats(period, 0)
		require.Error(t, err)
	}
	_, err = GetUsageStats("24h", -1)
	require.Error(t, err)
	require.EqualValues(t, 12, queries.Load())
}

func TestUsageStatsFailureIsNotCached(t *testing.T) {
	db := setupEnhancementListTestDB(t)
	previous := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() { common.DataExportEnabled = previous })
	_, err := GetUsageStats("30d", 0)
	require.Error(t, err)
	require.NoError(t, db.AutoMigrate(&model.QuotaData{}))
	data, err := GetUsageStats("30d", 0)
	require.NoError(t, err)
	require.Len(t, data.Trend, 720)
	require.False(t, data.AggregationEnabled)
}
