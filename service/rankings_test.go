package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func resetRankingsTestCache(t *testing.T) {
	t.Helper()
	rankingCacheMu.Lock()
	rankingCache = map[string]rankingCacheItem{}
	rankingCacheMu.Unlock()
}

func TestRankingsCacheCoalescesConcurrentReadersAndFailures(t *testing.T) {
	resetRankingsTestCache(t)
	config, _ := rankingConfig("week")
	var builds atomic.Int32
	build := func(context.Context, rankingPeriodConfig, time.Time) (*RankingsResponse, error) {
		builds.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &RankingsResponse{TotalTokens: 123}, nil
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := cachedRankingsSnapshot(config, build)
			if err != nil || data.TotalTokens != 123 {
				t.Errorf("unexpected response: %v %v", data, err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), builds.Load())
	resetRankingsTestCache(t)
	builds.Store(0)
	failed := func(context.Context, rankingPeriodConfig, time.Time) (*RankingsResponse, error) {
		builds.Add(1)
		return nil, errors.New("unavailable")
	}
	for range 10 {
		_, err := cachedRankingsSnapshot(config, failed)
		require.Error(t, err)
	}
	require.Equal(t, int32(1), builds.Load(), "failed queries must also have a cooldown")
}

func TestRankingsExpiredSnapshotReturnsImmediatelyDuringRefresh(t *testing.T) {
	resetRankingsTestCache(t)
	config, _ := rankingConfig("today")
	rankingCache[config.id] = rankingCacheItem{data: &RankingsResponse{TotalTokens: 1}}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	build := func(context.Context, rankingPeriodConfig, time.Time) (*RankingsResponse, error) {
		close(started)
		<-release
		close(finished)
		return &RankingsResponse{TotalTokens: 2}, nil
	}
	response := make(chan *RankingsResponse, 1)
	go func() { data, _ := cachedRankingsSnapshot(config, build); response <- data }()
	select {
	case data := <-response:
		require.Equal(t, int64(1), data.TotalTokens)
	case <-time.After(time.Second):
		close(release)
		t.Fatal("stale read blocked on rebuild")
	}
	<-started
	close(release)
	<-finished
	require.Eventually(t, func() bool {
		rankingCacheMu.Lock()
		defer rankingCacheMu.Unlock()
		return rankingCache[config.id].data.TotalTokens == 2
	}, time.Second, time.Millisecond)
}

func TestRankingsTotalsGrowthRemainderAndPeriods(t *testing.T) {
	for _, period := range []string{"today", "week", "month", "year"} {
		config, err := rankingConfig(period)
		require.NoError(t, err)
		start, end := rankingTimeRange(config, time.Date(2026, 11, 12, 15, 30, 0, 0, time.UTC))
		previousStart, previousEnd := previousRankingTimeRange(config, start)
		require.Equal(t, start, previousEnd)
		require.Equal(t, end-start, previousEnd-previousStart)
		require.Zero(t, start%3600)
	}
	_, err := rankingConfig("unbounded")
	require.Error(t, err)
	totals := []model.RankingQuotaTotal{{ModelName: "a", TotalTokens: 150}, {ModelName: "new", TotalTokens: 50}}
	rows := buildRankedModels(totals, 200, map[string]int{"a": 1}, map[string]int64{"a": 100}, nil, true)
	require.Equal(t, float64(0.75), rows[0].Share)
	require.Equal(t, float64(50), rows[0].GrowthPct)
	require.True(t, rows[1].IsNew)
	config, _ := rankingConfig("week")
	for i := 0; i < 22; i++ {
		totals = append(totals, model.RankingQuotaTotal{ModelName: string(rune('c' + i)), TotalTokens: 1})
	}
	history := buildModelHistory(nil, totals, nil, config)
	var total int64
	for _, row := range history.Models {
		total += row.Total
	}
	require.Equal(t, int64(222), total, "top model truncation must preserve the Others remainder")
	require.Len(t, history.Models, rankingHistoryLimit+1)
}
