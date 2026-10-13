package enhancement

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestIPRiskExcludesSameUserSharedIPsButIncludesTokenMultiIPs(t *testing.T) {
	setupEnhancementListTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{
		{UserId: 1, Username: "alice-before-rename", TokenId: 11, TokenName: "first-before-rename", Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 10, CreatedAt: now - 30},
		{UserId: 1, Username: "alice", TokenId: 11, TokenName: "first", Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 20, CreatedAt: now - 20},
		{UserId: 1, Username: "alice", TokenId: 12, TokenName: "second", Ip: "203.0.113.1", Type: model.LogTypeError, CreatedAt: now - 10},
		{UserId: 1, Username: "alice", TokenId: 11, TokenName: "first", Ip: "203.0.113.3", Type: model.LogTypeError, CreatedAt: now - 5},
		{UserId: 2, TokenId: 21, Ip: "203.0.113.2", Type: model.LogTypeConsume, CreatedAt: now - 20},
		{UserId: 2, TokenId: 21, Ip: "203.0.113.2", Type: model.LogTypeConsume, CreatedAt: now - 10},
		{UserId: 2, TokenId: 22, Ip: "203.0.113.2", Type: model.LogTypeConsume, CreatedAt: now - 120},
		{UserId: 2, TokenId: 22, Ip: "203.0.113.2", Type: model.LogTypeManage, CreatedAt: now - 10},
		{UserId: 2, TokenId: 0, Ip: "203.0.113.2", Type: model.LogTypeError, CreatedAt: now - 10},
		{UserId: 0, TokenId: 22, Ip: "203.0.113.2", Type: model.LogTypeError, CreatedAt: now - 10},
		{UserId: 1, TokenId: 11, Ip: "", Type: model.LogTypeConsume, CreatedAt: now - 20},
		{UserId: 1, TokenId: 12, Ip: "", Type: model.LogTypeConsume, CreatedAt: now - 10},
	}).Error)

	query := IPRiskQuery{Start: now - 60, End: now}
	shared, err := SharedTokenIPs(query)
	require.NoError(t, err)
	require.Zero(t, shared.Total)
	require.Empty(t, shared.Items)

	page, err := TokenMultiIPs(query)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Items, 1)
	item := page.Items[0]
	require.Equal(t, 11, item.TokenId)
	require.Equal(t, "first", item.TokenName)
	require.Equal(t, 1, item.UserId)
	require.Equal(t, "alice", item.Username)
	require.Equal(t, int64(2), item.IPCount)
	require.Equal(t, []string{"203.0.113.1", "203.0.113.3"}, item.IPs)
	require.Equal(t, int64(3), item.RequestCount)
	require.Equal(t, int64(1), item.ErrorCount)
	require.Equal(t, int64(30), item.Quota)
	require.Equal(t, now-30, item.FirstSeenAt)
	require.Equal(t, now-5, item.LastSeenAt)
}

func TestSharedTokenIPsFiltersAndSortsBeforePagination(t *testing.T) {
	setupEnhancementListTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{
		{UserId: 1, Username: "alice", TokenId: 11, TokenName: "first", Ip: "203.0.113.1", Type: model.LogTypeConsume, CreatedAt: now - 30},
		{UserId: 1, Username: "alice", TokenId: 11, TokenName: "first", Ip: "203.0.113.1", Type: model.LogTypeConsume, CreatedAt: now - 20},
		{UserId: 4, Username: "dave", TokenId: 12, TokenName: "second", Ip: "203.0.113.1", Type: model.LogTypeConsume, CreatedAt: now - 10},
		{UserId: 2, Username: "bob", TokenId: 21, TokenName: "third", Ip: "203.0.113.2", Type: model.LogTypeConsume, CreatedAt: now - 20},
		{UserId: 3, Username: "carol", TokenId: 31, TokenName: "fourth", Ip: "203.0.113.2", Type: model.LogTypeConsume, CreatedAt: now - 10},
	}).Error)

	for _, tc := range []struct {
		name  string
		query IPRiskQuery
		total int64
		ip    string
	}{
		{name: "default sorting", query: IPRiskQuery{}, total: 2, ip: "203.0.113.1"},
		{name: "second page", query: IPRiskQuery{Page: 2}, total: 2, ip: "203.0.113.2"},
		{name: "request count sort", query: IPRiskQuery{Sort: "request_count", Order: "desc"}, total: 2, ip: "203.0.113.1"},
		{name: "token keyword retains whole IP", query: IPRiskQuery{Keyword: "second"}, total: 1, ip: "203.0.113.1"},
		{name: "token ID", query: IPRiskQuery{Keyword: "12"}, total: 1, ip: "203.0.113.1"},
		{name: "single user filter", query: IPRiskQuery{Filters: map[string]string{"user_count": "1"}}},
		{name: "multiple user filter", query: IPRiskQuery{Filters: map[string]string{"user_count": "2"}}, total: 2, ip: "203.0.113.1"},
		{name: "no match", query: IPRiskQuery{Keyword: "missing"}},
		{name: "past last page", query: IPRiskQuery{Page: 3}, total: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := tc.query
			query.Start, query.End = now-60, now
			query.PageSize = 1
			page, err := SharedTokenIPs(query)
			require.NoError(t, err)
			require.Equal(t, tc.total, page.Total)
			if tc.ip == "" {
				require.NotNil(t, page.Items)
				require.Empty(t, page.Items)
				return
			}
			require.Len(t, page.Items, 1)
			require.Equal(t, tc.ip, page.Items[0].IP)
			require.Equal(t, int64(2), page.Items[0].TokenCount)
			if tc.ip == "203.0.113.1" {
				require.Equal(t, int64(3), page.Items[0].RequestCount)
			}
		})
	}
}

func TestIPRiskIncludesLogsBeyondLatest100000Requests(t *testing.T) {
	db := setupEnhancementListTestDB(t)
	now := common.GetTimestamp()
	// The older request links a second token to the busy IP and a second IP
	// to that token. Both risk lists must include it in the selected window.
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 2, Username: "bob", TokenId: 22, TokenName: "second", Ip: "203.0.113.1", Type: model.LogTypeError, CreatedAt: now - 50},
		{UserId: 2, Username: "bob", TokenId: 22, TokenName: "second", Ip: "203.0.113.2", Type: model.LogTypeConsume, Quota: 7, CreatedAt: now - 10},
	}).Error)
	logs := make([]model.Log, 100000)
	for i := range logs {
		logs[i] = model.Log{UserId: 1, Username: "alice", TokenId: 11, TokenName: "first", Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 3, CreatedAt: now - 20}
	}
	require.NoError(t, db.Select("UserId", "Username", "TokenId", "TokenName", "Ip", "Type", "Quota", "CreatedAt").CreateInBatches(&logs, 500).Error)
	query := IPRiskQuery{Start: now - 60, End: now}

	t.Run("shared IP", func(t *testing.T) {
		page, err := SharedTokenIPs(query)
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)
		require.Len(t, page.Items, 1)
		item := page.Items[0]
		require.Equal(t, int64(2), item.TokenCount)
		require.Equal(t, int64(2), item.UserCount)
		require.Equal(t, int64(100001), item.RequestCount)
		require.Equal(t, int64(1), item.ErrorCount)
		require.Equal(t, int64(300000), item.Quota)
		require.Equal(t, now-50, item.FirstSeenAt)
		require.Equal(t, now-20, item.LastSeenAt)
		require.Equal(t, int64(100000), item.Users[0].RequestCount)
		require.Equal(t, int64(100000), item.Tokens[0].RequestCount)
	})
	t.Run("multiple IPs", func(t *testing.T) {
		page, err := TokenMultiIPs(query)
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Total)
		require.Len(t, page.Items, 1)
		item := page.Items[0]
		require.Equal(t, 22, item.TokenId)
		require.Equal(t, int64(2), item.RequestCount)
		require.Equal(t, int64(1), item.ErrorCount)
		require.Equal(t, int64(7), item.Quota)
		require.Equal(t, now-50, item.FirstSeenAt)
		require.Equal(t, now-10, item.LastSeenAt)
		require.Equal(t, []string{"203.0.113.1", "203.0.113.2"}, item.IPs)
	})
}

func TestIPRiskHonorsEntireSelectedTimeRange(t *testing.T) {
	db := setupEnhancementListTestDB(t)
	now := common.GetTimestamp()
	start := now - int64((60 * 24 * time.Hour).Seconds())
	end := now - int64((24 * time.Hour).Seconds())
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 1, TokenId: 11, Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 3, CreatedAt: start},
		{UserId: 2, TokenId: 22, Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 5, CreatedAt: start + 1},
		{UserId: 1, TokenId: 11, Ip: "203.0.113.2", Type: model.LogTypeConsume, Quota: 7, CreatedAt: end},
		{UserId: 3, TokenId: 33, Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 100, CreatedAt: start - 1},
		{UserId: 4, TokenId: 44, Ip: "203.0.113.1", Type: model.LogTypeConsume, Quota: 100, CreatedAt: end + 1},
		{UserId: 1, TokenId: 11, Ip: "203.0.113.3", Type: model.LogTypeConsume, Quota: 100, CreatedAt: start - 1},
		{UserId: 1, TokenId: 11, Ip: "203.0.113.4", Type: model.LogTypeConsume, Quota: 100, CreatedAt: end + 1},
	}).Error)
	query := IPRiskQuery{Start: start, End: end}

	shared, err := SharedTokenIPs(query)
	require.NoError(t, err)
	require.Equal(t, int64(1), shared.Total)
	require.Len(t, shared.Items, 1)
	require.Equal(t, "203.0.113.1", shared.Items[0].IP)
	require.Equal(t, int64(2), shared.Items[0].UserCount)
	require.Equal(t, int64(2), shared.Items[0].TokenCount)
	require.Equal(t, int64(2), shared.Items[0].RequestCount)
	require.Equal(t, int64(8), shared.Items[0].Quota)
	require.Equal(t, start, shared.Items[0].FirstSeenAt)
	require.Equal(t, start+1, shared.Items[0].LastSeenAt)

	multi, err := TokenMultiIPs(query)
	require.NoError(t, err)
	require.Equal(t, int64(1), multi.Total)
	require.Len(t, multi.Items, 1)
	require.Equal(t, 11, multi.Items[0].TokenId)
	require.Equal(t, int64(2), multi.Items[0].IPCount)
	require.Equal(t, []string{"203.0.113.1", "203.0.113.2"}, multi.Items[0].IPs)
	require.Equal(t, int64(2), multi.Items[0].RequestCount)
	require.Equal(t, int64(10), multi.Items[0].Quota)
	require.Equal(t, start, multi.Items[0].FirstSeenAt)
	require.Equal(t, end, multi.Items[0].LastSeenAt)

	query.Start = start + 2
	shared, err = SharedTokenIPs(query)
	require.NoError(t, err)
	require.Zero(t, shared.Total)
	require.Empty(t, shared.Items)
	multi, err = TokenMultiIPs(query)
	require.NoError(t, err)
	require.Zero(t, multi.Total)
	require.Empty(t, multi.Items)
}
