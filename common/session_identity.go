package common

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

const RequestSessionIdentityKey = "request_session_identity"

// RequestSessionIdentity contains only identifiers supplied by the client.
// Capture it before applying channel overrides; never derive it from outbound requests.
type RequestSessionIdentity struct {
	SessionID     string
	SessionSource string
	CacheKey      string
	CacheSource   string
}

func NormalizeSessionIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || !utf8.ValidString(value) {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return value
}

// Hashing preserves case-sensitive identity even on case-insensitive SQL collations.
func SessionIdentifierHash(value string) string {
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ExtractRequestSessionIdentity(headers http.Header, body []byte) RequestSessionIdentity {
	var identity RequestSessionIdentity
	opencode := headers.Get("x-opencode-client") != "" || headers.Get("x-opencode-project") != "" ||
		headers.Get("x-opencode-session") != "" ||
		strings.HasPrefix(strings.ToLower(headers.Get("User-Agent")), "opencode/") ||
		headers.Get("x-opencode-session-id") != ""
	for _, name := range []string{"x-opencode-session-id", "x-deepseek-harness-session-id", "x-claude-code-session-id", "x-astrbot-conversation-id", "session-id", "session_id", "x-session-id"} {
		if name == "x-session-id" && opencode {
			continue
		}
		if value := NormalizeSessionIdentifier(headers.Get(name)); value != "" {
			identity.SessionID, identity.SessionSource = value, "header:"+name
			break
		}
	}
	for _, name := range []string{"x-opencode-session", "x-session-affinity"} {
		if value := NormalizeSessionIdentifier(headers.Get(name)); value != "" {
			identity.CacheKey, identity.CacheSource = value, "header:"+name
			break
		}
	}
	validBody := len(body) > 0 && gjson.ValidBytes(body)
	if identity.CacheKey == "" && validBody {
		key := gjson.GetBytes(body, "prompt_cache_key")
		if key.Type == gjson.String {
			identity.CacheKey = NormalizeSessionIdentifier(key.Str)
			if identity.CacheKey != "" {
				identity.CacheSource = "body:prompt_cache_key"
			}
		}
	}
	if identity.CacheKey == "" && opencode {
		if value := NormalizeSessionIdentifier(headers.Get("x-session-id")); value != "" {
			identity.CacheKey, identity.CacheSource = value, "header:x-session-id"
		}
	}
	if !validBody {
		return identity
	}
	if identity.SessionID == "" {
		metadata := gjson.GetBytes(body, "metadata.user_id")
		if metadata.Type == gjson.String {
			var embedded struct {
				SessionID string `json:"session_id"`
			}
			if UnmarshalJsonStr(metadata.Str, &embedded) == nil {
				identity.SessionID = NormalizeSessionIdentifier(embedded.SessionID)
			} else if strings.HasPrefix(metadata.Str, "user_") {
				account := strings.Index(metadata.Str, "_account_")
				if account > len("user_") {
					_, session, found := strings.Cut(metadata.Str[account+len("_account_"):], "_session_")
					if found {
						identity.SessionID = NormalizeSessionIdentifier(session)
					}
				}
			}
			if identity.SessionID != "" {
				identity.SessionSource = "body:metadata.user_id.session_id"
			}
			// Kimi's Anthropic adapter puts the session ID directly in user_id.
			// Require an explicit client identity: for a generic Anthropic SDK this
			// same field can be an account ID. Structured metadata must still use
			// the session_id extraction above, never the entire JSON string.
			userAgent := strings.ToLower(strings.TrimSpace(headers.Get("User-Agent")))
			kimi := strings.HasPrefix(userAgent, "kimicli/") || strings.HasPrefix(userAgent, "kimi-code-desktop/") ||
				strings.EqualFold(strings.TrimSpace(headers.Get("X-Msh-Platform")), "kimi_cli")
			plain := NormalizeSessionIdentifier(metadata.Str)
			structured := strings.HasPrefix(plain, "{") || strings.HasPrefix(plain, "[") ||
				(strings.HasPrefix(plain, "user_") && strings.Contains(plain, "_account_"))
			if identity.SessionID == "" && kimi && plain != "" && !structured {
				identity.SessionID, identity.SessionSource = plain, "body:metadata.user_id"
			}
		}
	}
	return identity
}
