package common

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestSessionIdentityClients(t *testing.T) {
	cases := []struct {
		name                         string
		headers                      map[string]string
		body, session, cache, source string
	}{
		{"claude header", map[string]string{"X-Claude-Code-Session-Id": "cc"}, "", "cc", "", "header:x-claude-code-session-id"},
		{"claude json", nil, `{"metadata":{"user_id":"{\"device_id\":\"device\",\"account_uuid\":\"account\",\"session_id\":\"cc-json\"}"}}`, "cc-json", "", "body:metadata.user_id.session_id"},
		{"claude legacy", nil, `{"metadata":{"user_id":"user_device_account__session_cc-old"}}`, "cc-old", "", "body:metadata.user_id.session_id"},
		{"claude legacy preserves entire id", nil, `{"metadata":{"user_id":"user_device_account_uuid_session_part_session_tail"}}`, "part_session_tail", "", "body:metadata.user_id.session_id"},
		{"codex", map[string]string{"session-id": "new", "session_id": "old"}, `{"prompt_cache_key":"cache"}`, "new", "cache", "header:session-id"},
		{"codex legacy", map[string]string{"session_id": "old"}, "", "old", "", "header:session_id"},
		{"zcode", map[string]string{"x-session-id": "zc", "X-ZCode-App-Version": "1"}, "", "zc", "", "header:x-session-id"},
		{"opencode", map[string]string{"x-opencode-session-id": "ses_current", "x-opencode-session": "ses_root", "x-session-id": "ses_root"}, "", "ses_current", "ses_root", "header:x-opencode-session-id"},
		{"opencode affinity only", map[string]string{"User-Agent": "opencode/1.18", "x-session-id": "affinity"}, "", "", "affinity", ""},
		{"opencode inferred from affinity header", map[string]string{"x-opencode-session": "affinity", "x-session-id": "not-current"}, "", "", "affinity", ""},
		{"opencode body cache priority", map[string]string{"User-Agent": "opencode/1.18", "x-session-id": "fallback"}, `{"prompt_cache_key":"body-cache"}`, "", "body-cache", ""},
		{"deepseek harness", map[string]string{"x-deepseek-harness-session-id": "session-dsh"}, "", "session-dsh", "", "header:x-deepseek-harness-session-id"},
		{"omp anthropic", map[string]string{"X-Claude-Code-Session-Id": "omp"}, `{"prompt_cache_key":"shared"}`, "omp", "shared", "header:x-claude-code-session-id"},
		{"omp openai", map[string]string{"session_id": "omp"}, "", "omp", "", "header:session_id"},
		{"pi responses", map[string]string{"session-id": "pi"}, "", "pi", "", "header:session-id"},
		{"invalid header falls back", map[string]string{"session-id": "a\nb", "session_id": "valid"}, "", "valid", "", "header:session_id"},
		{"invalid nested session", nil, `{"metadata":{"user_id":"{\"session_id\":123}"}}`, "", "", ""},
		{"pi openrouter", map[string]string{"x-session-id": "pi"}, "", "pi", "", "header:x-session-id"},
		{"pi cache only", map[string]string{"x-session-affinity": "pi-cache", "x-client-request-id": "request"}, "", "", "pi-cache", ""},
		{"cache body only", nil, `{"prompt_cache_key":"only-cache"}`, "", "only-cache", ""},
		{"irrelevant ids", map[string]string{"x-client-request-id": "request", "thread-id": "thread", "x-parent-session-id": "parent"}, `{"metadata":{"user_id":"account"},"previous_response_id":"resp_1"}`, "", "", ""},
		{"non-string", nil, `{"prompt_cache_key":123,"metadata":{"user_id":{"session_id":"object"}}}`, "", "", ""},
		{"invalid body", map[string]string{"session_id": "still-valid"}, `{bad`, "still-valid", "", "header:session_id"},
		{"priority", map[string]string{"x-opencode-session-id": "first", "x-deepseek-harness-session-id": "second", "session-id": "third"}, `{"metadata":{"user_id":"{\"session_id\":\"body\"}"}}`, "first", "", "header:x-opencode-session-id"},
		{"cache priority", map[string]string{"x-opencode-session": "first", "x-session-affinity": "second"}, `{"prompt_cache_key":"third"}`, "", "first", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			for k, v := range tt.headers {
				headers.Set(k, v)
			}
			got := ExtractRequestSessionIdentity(headers, []byte(tt.body))
			require.Equal(t, tt.session, got.SessionID)
			require.Equal(t, tt.cache, got.CacheKey)
			require.Equal(t, tt.source, got.SessionSource)
		})
	}
}

func TestRequestSessionIdentifierValidation(t *testing.T) {
	for _, invalid := range []string{"", "  ", "a\nb", "a\x00b", strings.Repeat("a", 513), strings.Repeat("中", 171), string([]byte{255})} {
		require.Empty(t, NormalizeSessionIdentifier(invalid))
	}
	require.Equal(t, "AbC", NormalizeSessionIdentifier("  AbC  "))
	require.Len(t, NormalizeSessionIdentifier(strings.Repeat("a", 512)), 512)
	require.NotEqual(t, SessionIdentifierHash("AbC"), SessionIdentifierHash("abc"))
}

func TestRequestSessionIdentityKimiHarnessAstrBot(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		body    string
		want    RequestSessionIdentity
	}{
		{
			name:    "Kimi desktop chat completions only supplies a cache key",
			headers: map[string]string{"User-Agent": "kimi-code-desktop/1.0.4"},
			body:    `{"prompt_cache_key":"kimi-cache","messages":[]}`,
			want:    RequestSessionIdentity{CacheKey: "kimi-cache", CacheSource: "body:prompt_cache_key"},
		},
		{
			name:    "Kimi CLI Anthropic plain metadata",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"  kimi-Session  "}}`,
			want:    RequestSessionIdentity{SessionID: "kimi-Session", SessionSource: "body:metadata.user_id"},
		},
		{
			name:    "Kimi desktop Anthropic plain metadata",
			headers: map[string]string{"User-Agent": "kimi-code-desktop/1.0.4"},
			body:    `{"metadata":{"user_id":"kimi-session"},"prompt_cache_key":"separate-cache"}`,
			want:    RequestSessionIdentity{SessionID: "kimi-session", SessionSource: "body:metadata.user_id", CacheKey: "separate-cache", CacheSource: "body:prompt_cache_key"},
		},
		{
			name:    "Kimi platform header identifies the client",
			headers: map[string]string{"X-Msh-Platform": "kimi_cli", "X-Msh-Device-Id": "device"},
			body:    `{"metadata":{"user_id":"kimi-session"}}`,
			want:    RequestSessionIdentity{SessionID: "kimi-session", SessionSource: "body:metadata.user_id"},
		},
		{
			name:    "explicit header wins over Kimi metadata",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0", "session-id": "header-session"},
			body:    `{"metadata":{"user_id":"kimi-session"}}`,
			want:    RequestSessionIdentity{SessionID: "header-session", SessionSource: "header:session-id"},
		},
		{
			name:    "Kimi structured metadata still extracts only the session",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"{\"account_uuid\":\"account\",\"session_id\":\"nested-session\"}"}}`,
			want:    RequestSessionIdentity{SessionID: "nested-session", SessionSource: "body:metadata.user_id.session_id"},
		},
		{
			name:    "Kimi device header alone is not a session",
			headers: map[string]string{"X-Msh-Platform": "kimi_cli", "X-Msh-Device-Id": "device"},
		},
		{
			name:    "Kimi rejects structured metadata without a valid session",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"{\"account_uuid\":\"account\",\"session_id\":123}"}}`,
		},
		{
			name:    "Kimi numeric session name is still a string identifier",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"12345"}}`,
			want:    RequestSessionIdentity{SessionID: "12345", SessionSource: "body:metadata.user_id"},
		},
		{
			name:    "Kimi rejects blank metadata",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"  "}}`,
		},
		{
			name:    "Kimi rejects non-string metadata",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":123}}`,
		},
		{
			name:    "Kimi rejects control characters",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"session\u0000tail"}}`,
		},
		{
			name:    "Kimi rejects oversized metadata without truncating",
			headers: map[string]string{"User-Agent": "KimiCLI/1.0.0"},
			body:    `{"metadata":{"user_id":"` + strings.Repeat("a", 513) + `"}}`,
		},
		{
			name:    "generic Anthropic SDK user ID remains an account identifier",
			headers: map[string]string{"User-Agent": "Anthropic/Python 0.80.0"},
			body:    `{"metadata":{"user_id":"uuid-shaped-account-id"}}`,
		},
		{
			name:    "client name mentioned in an unrelated UA is insufficient",
			headers: map[string]string{"User-Agent": "proxy/1.0 (KimiCLI/1.0.0)"},
			body:    `{"metadata":{"user_id":"account"}}`,
		},
		{
			name:    "Harness direct adapter current session wins over account and request IDs",
			headers: map[string]string{"User-Agent": "deepseek-harness/0.1.7-rc.2", "x-deepseek-harness-session-id": "current", "x-deepseek-harness-user-id": "account", "x-client-request-id": "request"},
			want:    RequestSessionIdentity{SessionID: "current", SessionSource: "header:x-deepseek-harness-session-id"},
		},
		{
			name:    "Harness pi adapter without identifiers cannot be reconstructed",
			headers: map[string]string{"User-Agent": "deepseek-harness/0.2.0-rc.2", "x-client-request-id": "request"},
			body:    `{"messages":[],"metadata":{"user_id":"account"}}`,
		},
		{
			name:    "Harness pi cache key stays separate from a session",
			headers: map[string]string{"User-Agent": "deepseek-harness/0.2.0-rc.2"},
			body:    `{"prompt_cache_key":"harness-cache"}`,
			want:    RequestSessionIdentity{CacheKey: "harness-cache", CacheSource: "body:prompt_cache_key"},
		},
		{
			name:    "AstrBot conversation header with any provider",
			headers: map[string]string{"x-astrbot-conversation-id": "  astrbot-Session  "},
			want:    RequestSessionIdentity{SessionID: "astrbot-Session", SessionSource: "header:x-astrbot-conversation-id"},
		},
		{
			name:    "AstrBot dedicated header wins over generic affinity and metadata",
			headers: map[string]string{"User-Agent": "AstrBot/4.29.0 AstrNa/1.7.1", "x-astrbot-conversation-id": "conversation", "x-opencode-session": "cache", "x-session-id": "affinity"},
			body:    `{"metadata":{"user_id":"user_device_account__session_metadata"}}`,
			want:    RequestSessionIdentity{SessionID: "conversation", SessionSource: "header:x-astrbot-conversation-id", CacheKey: "cache", CacheSource: "header:x-opencode-session"},
		},
		{
			name:    "invalid AstrBot header falls back to a valid generic session",
			headers: map[string]string{"x-astrbot-conversation-id": strings.Repeat("a", 513), "session-id": "fallback"},
			want:    RequestSessionIdentity{SessionID: "fallback", SessionSource: "header:session-id"},
		},
		{
			name:    "old AstrBot without a conversation header remains unprovided",
			headers: map[string]string{"User-Agent": "astrbot/4.28.2", "X-AstrBot-Desktop-Session": "dashboard-login"},
			body:    `{"messages":[],"user":"account"}`,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			headers := http.Header{}
			for name, value := range tt.headers {
				headers.Set(name, value)
			}
			require.Equal(t, tt.want, ExtractRequestSessionIdentity(headers, []byte(tt.body)))
		})
	}
}
