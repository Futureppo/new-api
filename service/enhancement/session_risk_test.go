package enhancement

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func sessionRiskLog(user, token int, id, cache string, when int64) model.Log {
	return model.Log{UserId: user, TokenId: token, Username: fmt.Sprintf("user-%d", user), TokenName: fmt.Sprintf("token-%d", token), CreatedAt: when, Type: model.LogTypeConsume, Quota: 10,
		SessionId: id, SessionHash: common.SessionIdentifierHash(id), SessionSource: "header:session-id", CacheKey: cache, CacheHash: common.SessionIdentifierHash(cache), CacheSource: "body:prompt_cache_key", SessionRecorded: true}
}

func TestSharedSessionsWindowIdentityAndSearch(t *testing.T) {
	setupEnhancementListTestDB(t)
	now := common.GetTimestamp()
	logs := []model.Log{
		sessionRiskLog(1, 11, "shared", "cache", now-60), sessionRiskLog(2, 22, "shared", "cache", now),
		sessionRiskLog(1, 12, "shared", "cache", now-10),
		sessionRiskLog(3, 31, "single-user", "other", now), sessionRiskLog(3, 32, "single-user", "other", now),
		sessionRiskLog(1, 11, "SHARED", "", now), sessionRiskLog(4, 41, "shared", "", now-61),
		sessionRiskLog(5, 51, "shared", "", now+60),
		// Identical text in different identifier kinds must never form a group.
		sessionRiskLog(6, 61, "split", "", now), sessionRiskLog(7, 71, "", "split", now),
	}
	logs[0].Ip = "203.0.113.1"
	logs[1].Ip = "203.0.113.2"
	logs[1].Type, logs[1].Quota = model.LogTypeError, 0
	require.NoError(t, model.LOG_DB.Create(&logs).Error)
	for _, query := range []IPRiskQuery{{}, {Keyword: "user-2"}, {Keyword: "token-22"}, {Filters: map[string]string{"user_count": "2"}}} {
		query.Start, query.End = now-60, now
		page, err := SharedSessions("session", query)
		require.NoError(t, err)
		require.EqualValues(t, 1, page.Total)
		require.Len(t, page.Items, 1)
		item := page.Items[0]
		require.Equal(t, "shared", item.Identifier)
		require.EqualValues(t, 2, item.UserCount)
		require.EqualValues(t, 3, item.TokenCount)
		require.EqualValues(t, 3, item.RequestCount)
		require.EqualValues(t, 1, item.ErrorCount)
		require.EqualValues(t, 20, item.Quota)
		require.Len(t, item.Users, 2)
		require.Len(t, item.IPs, 2)
		require.Equal(t, now-60, item.FirstSeenAt)
	}
	page, err := SharedSessions("cache", IPRiskQuery{Start: now - 60, End: now})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Equal(t, "cache", page.Items[0].Identifier)
	_, err = SharedSessions("session_id; DROP TABLE logs", IPRiskQuery{})
	require.Error(t, err)
}

func TestSharedSessionsFullWindowPagination(t *testing.T) {
	db := setupEnhancementListTestDB(t)
	now := common.GetTimestamp()
	logs := []model.Log{sessionRiskLog(1, 1, "busy", "", now-10), sessionRiskLog(2, 2, "busy", "", now-60), sessionRiskLog(3, 3, "second", "", now), sessionRiskLog(4, 4, "second", "", now)}
	require.NoError(t, db.Create(&logs).Error)
	// Repeatedly double only the busy account's requests past the old raw-log cap.
	for i := 0; i < 17; i++ {
		require.NoError(t, db.Exec(`INSERT INTO logs (user_id, token_id, username, token_name, created_at, type, quota, session_id, session_hash, session_source)
			SELECT user_id, token_id, username, token_name, created_at, type, quota, session_id, session_hash, session_source FROM logs WHERE user_id = 1`).Error)
	}
	query := IPRiskQuery{Start: now - 60, End: now, PageSize: 1, Sort: "request_count", Order: "desc"}
	page, err := SharedSessions("session", query)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Equal(t, "busy", page.Items[0].Identifier)
	require.EqualValues(t, 131073, page.Items[0].RequestCount)
	query.Page = 2
	page, err = SharedSessions("session", query)
	require.NoError(t, err)
	require.Equal(t, "second", page.Items[0].Identifier)
}

func TestSharedSessionBanValidatesUsersAndAudits(t *testing.T) {
	setupRiskActionTestDB(t)
	first, second, outsider := seedRiskUser(t, "sess-first"), seedRiskUser(t, "sess-second"), seedRiskUser(t, "sess-outsider")
	now := common.GetTimestamp()
	require.NoError(t, model.LOG_DB.Create(&[]model.Log{sessionRiskLog(first.Id, 1, "shared", "", now), sessionRiskLog(second.Id, 2, "shared", "", now)}).Error)
	ids := []int{first.Id, outsider.Id}
	req := SessionRiskBanRequest{Kind: "session", Identifier: "shared", Start: now - 60, End: now, BanUserRequest: BanUserRequest{UserIds: &ids, Reason: "session review", DurationMinutes: 5}}
	_, err := BanSharedSessionUsers(req, 900, common.RoleRootUser)
	require.ErrorContains(t, err, "not associated")
	var unchanged model.User
	require.NoError(t, model.DB.First(&unchanged, first.Id).Error)
	require.Equal(t, common.UserStatusEnabled, unchanged.Status)
	ids = []int{first.Id, second.Id}
	// The current operator is protected; another selected account is still processed.
	result, err := BanSharedSessionUsers(req, first.Id, common.RoleAdminUser)
	require.NoError(t, err)
	require.Equal(t, 1, result["success"])
	require.Len(t, result["failures"], 1)
	var banned model.User
	require.NoError(t, model.DB.First(&banned, second.Id).Error)
	require.Equal(t, common.UserStatusDisabled, banned.Status)
	require.GreaterOrEqual(t, banned.DisableUntil, now+300)
	var auditCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ? AND content LIKE ?", model.LogTypeManage, "%ban_shared_session_users%").Count(&auditCount).Error)
	require.EqualValues(t, 1, auditCount)
}
