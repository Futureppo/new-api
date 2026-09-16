package enhancement

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupModelStatusRefreshTest(t *testing.T) *gorm.DB {
	t.Helper()
	configurePublicModelStatusGroups(t, `{"visible":true}`, `{"visible":"Visible"}`)
	configureModelStatusRequestCountHideThreshold(t, 0)
	db := setupModelStatusTestDB(t)
	seedModelStatusTargets(t, db)
	seedModelStatusRequestLogs(t, db, "visible", "zz-visible-model", 3)
	return db
}

func makeModelStatusRefreshDue() {
	modelStatusPublicCache.Lock()
	defer modelStatusPublicCache.Unlock()
	modelStatusPublicCache.lastCompletedAt = time.Now().Add(-48 * time.Hour)
}

func TestModelStatusRequestsOnlyReadServerSnapshot(t *testing.T) {
	db := setupModelStatusRefreshTest(t)
	var queries atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_status_logs", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			queries.Add(1)
		}
	}))

	snapshot, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.False(t, snapshot.Ready)
	require.Empty(t, snapshot.Statuses)
	require.Zero(t, queries.Load())

	// The server populates the snapshot independently of any visitor.
	require.NoError(t, runModelStatusRefreshOnce())
	snapshot, err = GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.True(t, snapshot.Ready)
	require.Positive(t, snapshot.GeneratedAt)
	require.Len(t, snapshot.Statuses, 1)
	require.Equal(t, int64(3), snapshot.Statuses[0].TotalRequests)
	require.Equal(t, snapshot.GeneratedAt, snapshot.Statuses[0].GeneratedAt)
	queryCount := queries.Load()
	require.Positive(t, queryCount)

	// Even overdue data is served unchanged; reads cannot refresh it.
	seedModelStatusRequestLogs(t, db, "visible", "zz-visible-model", 1)
	makeModelStatusRefreshDue()
	for i := 0; i < 5; i++ {
		cached, err := GetModelStatusPublicSnapshot()
		require.NoError(t, err)
		require.Equal(t, snapshot, cached)
		_, err = ModelStatusesForPublicConfig()
		require.NoError(t, err)
		_, err = ModelStatusesForPublicModels(nil)
		require.NoError(t, err)
		_, err = ModelStatusesForPublicModels([]string{"zz-visible-model"})
		require.NoError(t, err)
		_, err = ModelStatusForPublicConfig("visible", "zz-visible-model")
		require.NoError(t, err)
	}
	require.Equal(t, queryCount, queries.Load())

	require.NoError(t, runModelStatusRefreshOnce())
	updated, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.Equal(t, int64(4), updated.Statuses[0].TotalRequests)
	queryCount = queries.Load()
	require.NoError(t, runModelStatusRefreshOnce())
	require.Equal(t, queryCount, queries.Load(), "server must wait for the configured interval")

	cfg := setting.GetEnhancementSetting()
	originalInterval := cfg.ModelStatusRefreshSeconds
	t.Cleanup(func() { cfg.ModelStatusRefreshSeconds = originalInterval })
	modelStatusPublicCache.Lock()
	modelStatusPublicCache.lastCompletedAt = time.Now().Add(-2 * time.Minute)
	modelStatusPublicCache.Unlock()
	cfg.ModelStatusRefreshSeconds = 300
	require.NoError(t, runModelStatusRefreshOnce())
	require.Equal(t, queryCount, queries.Load())
	cfg.ModelStatusRefreshSeconds = 60
	require.NoError(t, runModelStatusRefreshOnce())
	require.Greater(t, queries.Load(), queryCount, "server must pick up interval changes")
}

func TestModelStatusRefreshDoesNotBlockReadsOrOverlapAndDiscardsInvalidatedWork(t *testing.T) {
	db := setupModelStatusRefreshTest(t)
	require.NoError(t, runModelStatusRefreshOnce())
	previous, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	makeModelStatusRefreshDue()

	entered, release := make(chan struct{}), make(chan struct{})
	var blockOnce, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("block_status_logs", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			blockOnce.Do(func() {
				close(entered)
				<-release
			})
		}
	}))
	finished := make(chan error, 1)
	go func() { finished <- runModelStatusRefreshOnce() }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not start")
	}

	read := make(chan ModelStatusSnapshot, 1)
	go func() {
		_ = runModelStatusRefreshOnce() // Must skip while the first run is blocked.
		snapshot, _ := GetModelStatusPublicSnapshot()
		read <- snapshot
	}()
	select {
	case snapshot := <-read:
		require.Equal(t, previous, snapshot)
	case <-time.After(3 * time.Second):
		t.Fatal("a refresh blocked visitors or allowed a second calculation")
	}

	ClearModelStatusPublicCache()
	releaseOnce.Do(func() { close(release) })
	require.NoError(t, <-finished)
	snapshot, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.False(t, snapshot.Ready, "invalidated work must not repopulate the snapshot")
	require.NoError(t, runModelStatusRefreshOnce())
	snapshot, err = GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.True(t, snapshot.Ready)
}

func TestModelStatusRefreshFailureRetainsDataAndBacksOff(t *testing.T) {
	db := setupModelStatusRefreshTest(t)
	require.NoError(t, runModelStatusRefreshOnce())
	previous, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	makeModelStatusRefreshDue()
	var queries atomic.Int64
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail_status_logs", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			queries.Add(1)
			tx.AddError(errors.New("log database unavailable"))
		}
	}))

	require.Error(t, runModelStatusRefreshOnce())
	snapshot, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.True(t, snapshot.Ready)
	require.True(t, snapshot.RefreshFailed)
	require.Equal(t, previous.GeneratedAt, snapshot.GeneratedAt)
	require.Equal(t, previous.Statuses, snapshot.Statuses)
	require.NoError(t, runModelStatusRefreshOnce())
	require.Equal(t, int64(1), queries.Load())
}

func TestModelStatusEmptySnapshotHasServerTimestampAndRespectsVisibility(t *testing.T) {
	configurePublicModelStatusGroups(t, `{"visible":true}`, `{"visible":"Visible"}`)
	setupModelStatusTestDB(t)
	require.NoError(t, runModelStatusRefreshOnce())
	snapshot, err := GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.True(t, snapshot.Ready)
	require.Positive(t, snapshot.GeneratedAt)
	require.Empty(t, snapshot.Statuses)

	setting.GetEnhancementSetting().PublicEmbedEnabled = false
	_, err = GetModelStatusPublicSnapshot()
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, runModelStatusRefreshOnce())
	setting.GetEnhancementSetting().PublicEmbedEnabled = true
	snapshot, err = GetModelStatusPublicSnapshot()
	require.NoError(t, err)
	require.False(t, snapshot.Ready)
}
