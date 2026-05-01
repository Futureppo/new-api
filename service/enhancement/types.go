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
}

type UserSummary struct {
	Id                int    `json:"id"`
	Username          string `json:"username"`
	DisplayName       string `json:"display_name"`
	Role              int    `json:"role"`
	Status            int    `json:"status"`
	DisableReason     string `json:"disable_reason,omitempty"`
	Email             string `json:"email"`
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

type ModelStatus struct {
	ModelName         string  `json:"model_name"`
	Status            string  `json:"status"`
	Requests          int64   `json:"requests"`
	ErrorCount        int64   `json:"error_count"`
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
