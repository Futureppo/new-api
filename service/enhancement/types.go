package enhancement

import "time"

const (
	DefaultPageSize       = 20
	MaxPageSize           = 100
	DefaultQueryWindow    = 24 * time.Hour
	MaxPublicQueryWindow  = 24 * time.Hour
	MaxAdminQueryWindow   = 30 * 24 * time.Hour
	MaxPublicModelCount   = 50
	MaxAdminModelCount    = 200
	MaxGenerateRedemption = 100
	MaxBatchOperation     = 100
)

type PageResult[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

type UsageTotals struct {
	Requests         int64   `json:"requests"`
	Quota            int64   `json:"quota"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	AvgUseTime       float64 `json:"avg_use_time"`
}

type TimePoint struct {
	Time             string `json:"time"`
	Timestamp        int64  `json:"timestamp"`
	Requests         int64  `json:"requests"`
	Quota            int64  `json:"quota"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
}

type ModelUsage struct {
	ModelName        string  `json:"model_name"`
	Requests         int64   `json:"requests"`
	Quota            int64   `json:"quota"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ErrorCount       int64   `json:"error_count"`
	AvgUseTime       float64 `json:"avg_use_time"`
}

type UserUsage struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	Group        string `json:"group,omitempty"`
	Requests     int64  `json:"requests"`
	Quota        int64  `json:"quota"`
	DistinctIPs  int64  `json:"distinct_ips,omitempty"`
	RiskScore    int    `json:"risk_score,omitempty"`
	Status       int    `json:"status,omitempty"`
	LastActivity int64  `json:"last_activity,omitempty"`
}

type ChannelSummary struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	Type         int    `json:"type"`
	Status       int    `json:"status"`
	Group        string `json:"group"`
	Models       int    `json:"models"`
	UsedQuota    int64  `json:"used_quota"`
	ResponseTime int    `json:"response_time"`
	TestTime     int64  `json:"test_time"`
}

type TokenSummary struct {
	Id                 int    `json:"id"`
	UserId             int    `json:"user_id"`
	Name               string `json:"name"`
	Key                string `json:"key"`
	Status             int    `json:"status"`
	Group              string `json:"group"`
	CreatedTime        int64  `json:"created_time"`
	AccessedTime       int64  `json:"accessed_time"`
	ExpiredTime        int64  `json:"expired_time"`
	RemainQuota        int    `json:"remain_quota"`
	UsedQuota          int    `json:"used_quota"`
	UnlimitedQuota     bool   `json:"unlimited_quota"`
	ModelLimitsEnabled bool   `json:"model_limits_enabled"`
	ModelLimits        string `json:"model_limits"`
	AllowIps           string `json:"allow_ips"`
}

type UserSummary struct {
	Id                int    `json:"id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Role              int    `json:"role"`
	Status            int    `json:"status"`
	DisableReason     string `json:"disable_reason,omitempty"`
	Email             string `json:"email"`
	GitHubId          string `json:"github_id,omitempty"`
	Quota             int    `json:"quota"`
	UsedQuota         int    `json:"used_quota"`
	RequestCount      int    `json:"request_count"`
	TodayRequestCount int64  `json:"today_request_count"`
	TodayUsedTokens   int64  `json:"today_used_tokens"`
	Group             string `json:"group"`
	InviterId         int    `json:"inviter_id"`
	AffCount          int    `json:"aff_count"`
	LinuxDOId         string `json:"linux_do_id,omitempty"`
}

type GitHubAgeBanRequest struct {
	MinimumAgeSeconds int64  `json:"minimum_age_seconds"`
	Reason            string `json:"reason"`
	DryRun            bool   `json:"dry_run"`
}

type GitHubAgeBanUser struct {
	Id                     int    `json:"id"`
	Username               string `json:"username"`
	DisplayName            string `json:"display_name,omitempty"`
	Email                  string `json:"email,omitempty"`
	GitHubId               string `json:"github_id"`
	GitHubLogin            string `json:"github_login,omitempty"`
	GitHubAccountCreatedAt string `json:"github_account_created_at"`
	GitHubAccountAge       int64  `json:"github_account_age_seconds"`
}

type GitHubAgeBanSkippedUser struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
	GitHubId string `json:"github_id,omitempty"`
	Reason   string `json:"reason"`
}

type GitHubAgeBanFailure struct {
	Id       int    `json:"id"`
	Username string `json:"username"`
	GitHubId string `json:"github_id,omitempty"`
	Message  string `json:"message"`
}

type GitHubAgeBanResult struct {
	MinimumAgeSeconds int64                     `json:"minimum_age_seconds"`
	DryRun            bool                      `json:"dry_run"`
	TotalCandidates   int                       `json:"total_candidates"`
	Checked           int                       `json:"checked"`
	Matched           int                       `json:"matched"`
	Banned            int                       `json:"banned"`
	Skipped           int                       `json:"skipped"`
	Failures          int                       `json:"failures"`
	RateLimited       bool                      `json:"rate_limited"`
	RateLimitReset    int64                     `json:"rate_limit_reset,omitempty"`
	MatchedUsers      []GitHubAgeBanUser        `json:"matched_users"`
	SkippedUsers      []GitHubAgeBanSkippedUser `json:"skipped_users,omitempty"`
	FailureUsers      []GitHubAgeBanFailure     `json:"failure_users,omitempty"`
}

type IPLogCoverage struct {
	TotalUsers    int64   `json:"total_users"`
	EnabledUsers  int64   `json:"enabled_users"`
	DisabledUsers int64   `json:"disabled_users"`
	EnabledRatio  float64 `json:"enabled_ratio"`
	GeneratedAt   int64   `json:"generated_at"`
}

type IPRiskQuery struct {
	Page     int
	PageSize int
	Start    int64
	End      int64
	Sort     string
	Order    string
	Keyword  string
	Filters  map[string]string
}

type IPRiskUserRef struct {
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	RequestCount int64  `json:"request_count"`
}

type IPRiskTokenRef struct {
	TokenId      int    `json:"token_id"`
	TokenName    string `json:"token_name"`
	UserId       int    `json:"user_id"`
	Username     string `json:"username"`
	RequestCount int64  `json:"request_count"`
}

type SharedTokenIPRisk struct {
	IP           string           `json:"ip"`
	TokenCount   int64            `json:"token_count"`
	UserCount    int64            `json:"user_count"`
	RequestCount int64            `json:"request_count"`
	ErrorCount   int64            `json:"error_count"`
	Quota        int64            `json:"quota"`
	FirstSeenAt  int64            `json:"first_seen_at"`
	LastSeenAt   int64            `json:"last_seen_at"`
	Users        []IPRiskUserRef  `json:"users"`
	Tokens       []IPRiskTokenRef `json:"tokens"`
}

type TokenMultiIPRisk struct {
	TokenId      int      `json:"token_id"`
	TokenName    string   `json:"token_name"`
	UserId       int      `json:"user_id"`
	Username     string   `json:"username"`
	IPCount      int64    `json:"ip_count"`
	RequestCount int64    `json:"request_count"`
	ErrorCount   int64    `json:"error_count"`
	Quota        int64    `json:"quota"`
	FirstSeenAt  int64    `json:"first_seen_at"`
	LastSeenAt   int64    `json:"last_seen_at"`
	IPs          []string `json:"ips"`
}

type RedemptionSummary struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	Key          string `json:"key"`
	Status       int    `json:"status"`
	Name         string `json:"name"`
	Quota        int    `json:"quota"`
	CreatedTime  int64  `json:"created_time"`
	RedeemedTime int64  `json:"redeemed_time"`
	UsedUserId   int    `json:"used_user_id"`
	UsedUsername string `json:"used_username,omitempty"`
	ExpiredTime  int64  `json:"expired_time"`
}

type ModelStatusSlot struct {
	Slot          int     `json:"slot"`
	StartTime     int64   `json:"start_time"`
	EndTime       int64   `json:"end_time"`
	TotalRequests int64   `json:"total_requests"`
	SuccessCount  int64   `json:"success_count"`
	ErrorCount    int64   `json:"error_count"`
	SuccessRate   float64 `json:"success_rate"`
	Status        string  `json:"status"`
}

type ModelStatus struct {
	ModelName     string            `json:"model_name"`
	Group         string            `json:"group"`
	GroupName     string            `json:"group_name"`
	DisplayName   string            `json:"display_name"`
	TimeWindow    string            `json:"time_window"`
	TotalRequests int64             `json:"total_requests"`
	SuccessCount  int64             `json:"success_count"`
	ErrorCount    int64             `json:"error_count"`
	SuccessRate   float64           `json:"success_rate"`
	CurrentStatus string            `json:"current_status"`
	SlotData      []ModelStatusSlot `json:"slot_data"`
	GeneratedAt   int64             `json:"generated_at"`

	Status            string  `json:"status"`
	Requests          int64   `json:"requests"`
	ErrorRate         float64 `json:"error_rate"`
	Quota             int64   `json:"quota"`
	AvgUseTime        float64 `json:"avg_use_time"`
	PromptTokens      int64   `json:"prompt_tokens"`
	CompletionTokens  int64   `json:"completion_tokens"`
	LastRequestAt     int64   `json:"last_request_at"`
	TimeWindowMinutes int     `json:"time_window_minutes"`
}

type GenerateRedemptionsRequest struct {
	Count       int    `json:"count"`
	Quota       int    `json:"quota"`
	Name        string `json:"name"`
	ExpiredTime int64  `json:"expired_time"`
}

type BatchIDsRequest struct {
	Ids []int `json:"ids"`
}

type BanUserRequest struct {
	Reason string `json:"reason"`
}

type UpdateTokenRequest struct {
	Name               string `json:"name"`
	Status             int    `json:"status"`
	ExpiredTime        int64  `json:"expired_time"`
	RemainQuota        int    `json:"remain_quota"`
	UnlimitedQuota     bool   `json:"unlimited_quota"`
	ModelLimitsEnabled bool   `json:"model_limits_enabled"`
	ModelLimits        string `json:"model_limits"`
	AllowIps           string `json:"allow_ips"`
	Group              string `json:"group"`
}
