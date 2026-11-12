package model

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRankingsAggregateOnlyAndAdjacentBoundaries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "rankings.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	// Deliberately no logs or users table: rankings must not read either.
	require.NoError(t, db.AutoMigrate(&QuotaData{}))
	require.NoError(t, db.Create(&[]QuotaData{
		{ModelName: "a", UserID: 1, CreatedAt: 3600, TokenUsed: 10},
		{ModelName: "a", UserID: 1, CreatedAt: 7200, TokenUsed: 20},
		{ModelName: "a", UserID: 2, CreatedAt: 7200, TokenUsed: 30},
		{ModelName: "b", UserID: 2, CreatedAt: 10800, TokenUsed: 50},
		{ModelName: "excluded", CreatedAt: 14400, TokenUsed: 9000},
		{ModelName: "", CreatedAt: 7200, TokenUsed: 9000},
	}).Error)
	current, err := GetRankingQuotaTotals(context.Background(), 7200, 14400)
	require.NoError(t, err)
	require.Equal(t, []RankingQuotaTotal{{ModelName: "a", TotalTokens: 50}, {ModelName: "b", TotalTokens: 50}}, current)
	prior, err := GetRankingQuotaTotals(context.Background(), 0, 7200)
	require.NoError(t, err)
	require.Equal(t, []RankingQuotaTotal{{ModelName: "a", TotalTokens: 10}}, prior)
	buckets, err := GetRankingQuotaBuckets(context.Background(), 7200, 14400, 7200)
	require.NoError(t, err)
	require.Len(t, buckets, 2)
	for _, bucket := range buckets {
		require.Equal(t, int64(7200), bucket.Bucket)
		require.Equal(t, int64(50), bucket.Tokens)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = GetRankingQuotaTotals(ctx, 7200, 14400)
	require.ErrorIs(t, err, context.Canceled)
}
