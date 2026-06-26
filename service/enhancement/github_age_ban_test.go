package enhancement

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

type gitHubAgeBanMockResult struct {
	login     string
	createdAt time.Time
	err       error
	rateLimit githubAgeBanRateLimit
}

func stubGitHubAgeBanLookup(t *testing.T, now time.Time, results map[string]gitHubAgeBanMockResult) {
	t.Helper()

	originalLookup := githubAgeBanLookupUser
	originalNow := githubAgeBanNow
	githubAgeBanNow = func() time.Time { return now }
	githubAgeBanLookupUser = func(ctx context.Context, accountID string) (githubAgeBanLookupInfo, githubAgeBanRateLimit, error) {
		result, ok := results[accountID]
		if !ok {
			return githubAgeBanLookupInfo{}, githubAgeBanRateLimit{}, fmt.Errorf("unexpected github id %s", accountID)
		}
		if result.rateLimit.Limited {
			return githubAgeBanLookupInfo{}, result.rateLimit, nil
		}
		if result.err != nil {
			return githubAgeBanLookupInfo{}, githubAgeBanRateLimit{}, result.err
		}
		return githubAgeBanLookupInfo{
			Login:     result.login,
			CreatedAt: result.createdAt,
		}, githubAgeBanRateLimit{}, nil
	}
	t.Cleanup(func() {
		githubAgeBanLookupUser = originalLookup
		githubAgeBanNow = originalNow
	})
}

func seedGitHubAgeBanUser(t *testing.T, id int, role int, status int, githubID string) model.User {
	t.Helper()

	user := model.User{
		Id:       id,
		Username: fmt.Sprintf("github_age_user_%d", id),
		Password: "password",
		Role:     role,
		Status:   status,
		Email:    fmt.Sprintf("github_age_user_%d@example.com", id),
		GitHubId: githubID,
		AffCode:  fmt.Sprintf("github_age_aff_%d", id),
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func requireGitHubAgeBanUserStatus(t *testing.T, id int, status int, reason string) {
	t.Helper()

	var user model.User
	require.NoError(t, model.DB.Where("id = ?", id).First(&user).Error)
	require.Equal(t, status, user.Status)
	require.Equal(t, reason, user.DisableReason)
}

func TestBatchBanYoungGitHubUsersRejectsNonPositiveThreshold(t *testing.T) {
	_, err := BatchBanYoungGitHubUsers(context.Background(), GitHubAgeBanRequest{MinimumAgeSeconds: 0, DryRun: true}, 900)
	require.Error(t, err)
	require.Contains(t, err.Error(), "minimum_age_seconds")

	_, err = BatchBanYoungGitHubUsers(context.Background(), GitHubAgeBanRequest{MinimumAgeSeconds: -1, DryRun: true}, 900)
	require.Error(t, err)
	require.Contains(t, err.Error(), "minimum_age_seconds")
}

func TestBatchBanYoungGitHubUsersDryRunDoesNotModifyUsers(t *testing.T) {
	setupUserPurgeTestDB(t)
	now := time.Unix(2_000_000_000, 0)
	seedGitHubAgeBanUser(t, 101, common.RoleCommonUser, common.UserStatusEnabled, "1001")
	stubGitHubAgeBanLookup(t, now, map[string]gitHubAgeBanMockResult{
		"1001": {login: "young", createdAt: now.Add(-10 * time.Second)},
	})

	result, err := BatchBanYoungGitHubUsers(context.Background(), GitHubAgeBanRequest{
		MinimumAgeSeconds: 60,
		DryRun:            true,
	}, 900)

	require.NoError(t, err)
	require.Equal(t, 1, result.TotalCandidates)
	require.Equal(t, 1, result.Checked)
	require.Equal(t, 1, result.Matched)
	require.Equal(t, 0, result.Banned)
	require.Len(t, result.MatchedUsers, 1)
	requireGitHubAgeBanUserStatus(t, 101, common.UserStatusEnabled, "")
}

func TestBatchBanYoungGitHubUsersExecutesOnlyMatchingEnabledCommonUsers(t *testing.T) {
	setupUserPurgeTestDB(t)
	now := time.Unix(2_000_000_000, 0)
	reason := "young github account"
	seedGitHubAgeBanUser(t, 201, common.RoleCommonUser, common.UserStatusEnabled, "2001")
	seedGitHubAgeBanUser(t, 202, common.RoleCommonUser, common.UserStatusEnabled, "2002")
	seedGitHubAgeBanUser(t, 203, common.RoleCommonUser, common.UserStatusEnabled, "2003")
	seedGitHubAgeBanUser(t, 204, common.RoleAdminUser, common.UserStatusEnabled, "2004")
	seedGitHubAgeBanUser(t, 205, common.RoleRootUser, common.UserStatusEnabled, "2005")
	seedGitHubAgeBanUser(t, 206, common.RoleCommonUser, common.UserStatusDisabled, "2006")
	seedGitHubAgeBanUser(t, 207, common.RoleCommonUser, common.UserStatusEnabled, "octocat")
	seedGitHubAgeBanUser(t, 208, common.RoleCommonUser, common.UserStatusEnabled, "")
	seedGitHubAgeBanUser(t, 209, common.RoleCommonUser, common.UserStatusEnabled, "2009")
	stubGitHubAgeBanLookup(t, now, map[string]gitHubAgeBanMockResult{
		"2001": {login: "young", createdAt: now.Add(-99 * time.Second)},
		"2002": {login: "equal", createdAt: now.Add(-100 * time.Second)},
		"2003": {login: "old", createdAt: now.Add(-101 * time.Second)},
	})

	result, err := BatchBanYoungGitHubUsers(context.Background(), GitHubAgeBanRequest{
		MinimumAgeSeconds: 100,
		Reason:            reason,
	}, 209)

	require.NoError(t, err)
	require.Equal(t, 5, result.TotalCandidates)
	require.Equal(t, 3, result.Checked)
	require.Equal(t, 2, result.Matched)
	require.Equal(t, 2, result.Banned)
	require.Equal(t, 2, result.Skipped)
	require.Len(t, result.MatchedUsers, 2)
	require.Len(t, result.SkippedUsers, 2)
	requireGitHubAgeBanUserStatus(t, 201, common.UserStatusDisabled, reason)
	requireGitHubAgeBanUserStatus(t, 202, common.UserStatusDisabled, reason)
	requireGitHubAgeBanUserStatus(t, 203, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 204, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 205, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 206, common.UserStatusDisabled, "")
	requireGitHubAgeBanUserStatus(t, 207, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 208, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 209, common.UserStatusEnabled, "")
}

func TestBatchBanYoungGitHubUsersReportsFailures(t *testing.T) {
	setupUserPurgeTestDB(t)
	now := time.Unix(2_000_000_000, 0)
	seedGitHubAgeBanUser(t, 301, common.RoleCommonUser, common.UserStatusEnabled, "3001")
	seedGitHubAgeBanUser(t, 302, common.RoleCommonUser, common.UserStatusEnabled, "3002")
	stubGitHubAgeBanLookup(t, now, map[string]gitHubAgeBanMockResult{
		"3001": {err: errors.New("github lookup failed: status 404")},
		"3002": {err: errors.New("invalid github created_at")},
	})

	result, err := BatchBanYoungGitHubUsers(context.Background(), GitHubAgeBanRequest{
		MinimumAgeSeconds: 60,
		DryRun:            true,
	}, 900)

	require.NoError(t, err)
	require.Equal(t, 2, result.Checked)
	require.Equal(t, 2, result.Failures)
	require.Len(t, result.FailureUsers, 2)
	require.False(t, result.RateLimited)
	requireGitHubAgeBanUserStatus(t, 301, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 302, common.UserStatusEnabled, "")
}

func TestBatchBanYoungGitHubUsersStopsOnRateLimit(t *testing.T) {
	setupUserPurgeTestDB(t)
	now := time.Unix(2_000_000_000, 0)
	seedGitHubAgeBanUser(t, 401, common.RoleCommonUser, common.UserStatusEnabled, "4001")
	seedGitHubAgeBanUser(t, 402, common.RoleCommonUser, common.UserStatusEnabled, "4002")
	stubGitHubAgeBanLookup(t, now, map[string]gitHubAgeBanMockResult{
		"4001": {rateLimit: githubAgeBanRateLimit{Limited: true, Reset: now.Add(time.Hour).Unix()}},
	})

	result, err := BatchBanYoungGitHubUsers(context.Background(), GitHubAgeBanRequest{
		MinimumAgeSeconds: 60,
	}, 900)

	require.NoError(t, err)
	require.True(t, result.RateLimited)
	require.Equal(t, now.Add(time.Hour).Unix(), result.RateLimitReset)
	require.Equal(t, 1, result.Checked)
	require.Equal(t, 0, result.Banned)
	requireGitHubAgeBanUserStatus(t, 401, common.UserStatusEnabled, "")
	requireGitHubAgeBanUserStatus(t, 402, common.UserStatusEnabled, "")
}
