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
