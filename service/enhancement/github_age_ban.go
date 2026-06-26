package enhancement

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const githubAgeBanPreviewLimit = 100

type githubAgeBanLookupInfo struct {
	Id        int64
	Login     string
	CreatedAt time.Time
}

type githubAgeBanRateLimit struct {
	Limited bool
	Reset   int64
}

type githubAgeBanUserResponse struct {
	Id        int64  `json:"id"`
	Login     string `json:"login"`
	CreatedAt string `json:"created_at"`
}

var (
	githubAgeBanAPIBaseURL = "https://api.github.com"
	githubAgeBanHTTPClient = &http.Client{Timeout: 20 * time.Second}
	githubAgeBanNow        = time.Now
	githubAgeBanLookupUser = lookupGitHubAgeBanUser
)

func BatchBanYoungGitHubUsers(ctx context.Context, req GitHubAgeBanRequest, operatorId int) (GitHubAgeBanResult, error) {
	result := GitHubAgeBanResult{
		MinimumAgeSeconds: req.MinimumAgeSeconds,
		DryRun:            req.DryRun,
		MatchedUsers:      []GitHubAgeBanUser{},
	}
	if req.MinimumAgeSeconds <= 0 {
		return result, errors.New("minimum_age_seconds must be greater than 0")
	}

	reason := strings.TrimSpace(req.Reason)
	if len([]rune(reason)) > 255 {
		return result, errors.New("reason is too long")
	}
	if reason == "" {
		reason = fmt.Sprintf("GitHub account age <= %ds", req.MinimumAgeSeconds)
	}

	var candidates []model.User
	if err := model.DB.Omit("password").
		Where("role = ? AND status = ? AND github_id <> ?", common.RoleCommonUser, common.UserStatusEnabled, "").
		Order("id ASC").
		Find(&candidates).Error; err != nil {
		return result, err
	}
	result.TotalCandidates = len(candidates)

	now := githubAgeBanNow()
	matchedIDs := make([]int, 0)
	for _, user := range candidates {
		if user.Id == operatorId {
			appendGitHubAgeBanSkipped(&result, user, "current operator is protected")
			continue
		}

		githubID := strings.TrimSpace(user.GitHubId)
		accountID, err := strconv.ParseInt(githubID, 10, 64)
		if err != nil || accountID <= 0 {
			appendGitHubAgeBanSkipped(&result, user, "legacy non-numeric GitHub ID")
			continue
		}

		result.Checked++
		info, rateLimit, err := githubAgeBanLookupUser(ctx, githubID)
		if rateLimit.Limited {
			result.RateLimited = true
			result.RateLimitReset = rateLimit.Reset
			break
		}
		if err != nil {
			appendGitHubAgeBanFailure(&result, user, err.Error())
			continue
		}

		ageSeconds := int64(now.Sub(info.CreatedAt).Seconds())
		if ageSeconds > req.MinimumAgeSeconds {
			continue
		}

		result.Matched++
		matchedIDs = append(matchedIDs, user.Id)
		if len(result.MatchedUsers) < githubAgeBanPreviewLimit {
			result.MatchedUsers = append(result.MatchedUsers, GitHubAgeBanUser{
				Id:                     user.Id,
				Username:               user.Username,
				DisplayName:            user.DisplayName,
				Email:                  user.Email,
				GitHubId:               githubID,
				GitHubLogin:            info.Login,
				GitHubAccountCreatedAt: info.CreatedAt.Format(time.RFC3339),
				GitHubAccountAge:       ageSeconds,
			})
		}
	}

	if req.DryRun || result.RateLimited || len(matchedIDs) == 0 {
		return result, nil
	}

	if err := model.DB.Model(&model.User{}).
		Where("id IN ? AND role = ? AND status = ?", matchedIDs, common.RoleCommonUser, common.UserStatusEnabled).
		Updates(map[string]interface{}{
			"status":         common.UserStatusDisabled,
			"disable_reason": reason,
		}).Error; err != nil {
		return result, err
	}

	var banned int64
	if err := model.DB.Model(&model.User{}).
		Where("id IN ? AND status = ? AND disable_reason = ?", matchedIDs, common.UserStatusDisabled, reason).
		Count(&banned).Error; err != nil {
		return result, err
	}
	result.Banned = int(banned)

	for _, userID := range matchedIDs {
		_ = model.InvalidateUserCache(userID)
		_ = model.InvalidateUserTokensCache(userID)
	}
	audit(operatorId, "enhancements.users", "github_age_batch_ban", map[string]interface{}{
		"minimum_age_seconds": req.MinimumAgeSeconds,
		"matched":             result.Matched,
		"banned":              result.Banned,
		"reason":              reason,
		"rate_limited":        result.RateLimited,
	})
	return result, nil
}

func appendGitHubAgeBanSkipped(result *GitHubAgeBanResult, user model.User, reason string) {
	result.Skipped++
	if len(result.SkippedUsers) >= githubAgeBanPreviewLimit {
		return
	}
	result.SkippedUsers = append(result.SkippedUsers, GitHubAgeBanSkippedUser{
		Id:       user.Id,
		Username: user.Username,
		GitHubId: strings.TrimSpace(user.GitHubId),
		Reason:   reason,
	})
}

func appendGitHubAgeBanFailure(result *GitHubAgeBanResult, user model.User, message string) {
	result.Failures++
	if len(result.FailureUsers) >= githubAgeBanPreviewLimit {
		return
	}
	result.FailureUsers = append(result.FailureUsers, GitHubAgeBanFailure{
		Id:       user.Id,
		Username: user.Username,
		GitHubId: strings.TrimSpace(user.GitHubId),
		Message:  message,
	})
}

func lookupGitHubAgeBanUser(ctx context.Context, accountID string) (githubAgeBanLookupInfo, githubAgeBanRateLimit, error) {
	endpoint := strings.TrimRight(githubAgeBanAPIBaseURL, "/") + "/user/" + accountID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return githubAgeBanLookupInfo{}, githubAgeBanRateLimit{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "new-api-enhancement-github-age-ban")
	if common.GitHubClientId != "" && common.GitHubClientSecret != "" {
		req.SetBasicAuth(common.GitHubClientId, common.GitHubClientSecret)
	}

	res, err := githubAgeBanHTTPClient.Do(req)
	if err != nil {
		return githubAgeBanLookupInfo{}, githubAgeBanRateLimit{}, err
	}
	defer res.Body.Close()

	rateLimit := githubAgeBanRateLimit{Reset: parseGitHubRateLimitReset(res.Header.Get("X-RateLimit-Reset"))}
	if isGitHubRateLimited(res) {
		rateLimit.Limited = true
		return githubAgeBanLookupInfo{}, rateLimit, nil
	}

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(res.StatusCode)
		}
		return githubAgeBanLookupInfo{}, rateLimit, fmt.Errorf("github lookup failed: status %d: %s", res.StatusCode, message)
	}

	var payload githubAgeBanUserResponse
	if err := common.DecodeJson(res.Body, &payload); err != nil {
		return githubAgeBanLookupInfo{}, rateLimit, err
	}
	if payload.CreatedAt == "" {
		return githubAgeBanLookupInfo{}, rateLimit, errors.New("github created_at is empty")
	}
	createdAt, err := time.Parse(time.RFC3339, payload.CreatedAt)
	if err != nil {
		return githubAgeBanLookupInfo{}, rateLimit, fmt.Errorf("invalid github created_at: %w", err)
	}

	return githubAgeBanLookupInfo{
		Id:        payload.Id,
		Login:     payload.Login,
		CreatedAt: createdAt,
	}, rateLimit, nil
}

func isGitHubRateLimited(res *http.Response) bool {
	if res.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if res.StatusCode != http.StatusForbidden {
		return false
	}
	return strings.TrimSpace(res.Header.Get("X-RateLimit-Remaining")) == "0"
}

func parseGitHubRateLimitReset(value string) int64 {
	reset, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0
	}
	return reset
}
