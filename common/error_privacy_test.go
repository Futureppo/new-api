package common

import "testing"

func TestIsPublicErrorCode(t *testing.T) {
	for _, code := range []string{"invalid_api_key", "401", "channel:no_available_key", "response.failed", "INVALID_ARGUMENT"} {
		if !IsPublicErrorCode(code) {
			t.Errorf("rejected machine-readable code %q", code)
		}
	}
	for _, code := range []string{"", "provider account unavailable", "https://provider.example", `{"provider":"internal"}`, "账户错误"} {
		if IsPublicErrorCode(code) {
			t.Errorf("accepted error detail %q", code)
		}
	}
}
