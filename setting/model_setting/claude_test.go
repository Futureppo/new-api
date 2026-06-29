package model_setting

import (
	"net/http"
	"testing"
)

func TestClaudeSettingsWriteHeadersMergesConfiguredValuesIntoSingleHeader(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-3-7-sonnet-20250219-thinking": {
				"anthropic-beta": {
					"token-efficient-tools-2025-02-19",
				},
			},
		},
	}

	headers := http.Header{}
	headers.Set("anthropic-beta", "output-128k-2025-02-19")

	settings.WriteHeaders("claude-3-7-sonnet-20250219-thinking", &headers)

	got := headers.Values("anthropic-beta")
	if len(got) != 1 {
		t.Fatalf("expected a single merged header value, got %v", got)
	}
	expected := "output-128k-2025-02-19,token-efficient-tools-2025-02-19"
	if got[0] != expected {
		t.Fatalf("expected merged header %q, got %q", expected, got[0])
	}
}

func TestClaudeSettingsWriteHeadersDeduplicatesAcrossCommaSeparatedAndRepeatedValues(t *testing.T) {
	settings := &ClaudeSettings{
		HeadersSettings: map[string]map[string][]string{
			"claude-3-7-sonnet-20250219-thinking": {
				"anthropic-beta": {
					"token-efficient-tools-2025-02-19",
					"computer-use-2025-01-24",
				},
			},
		},
	}

	headers := http.Header{}
	headers.Add("anthropic-beta", "output-128k-2025-02-19, token-efficient-tools-2025-02-19")
	headers.Add("anthropic-beta", "token-efficient-tools-2025-02-19")

	settings.WriteHeaders("claude-3-7-sonnet-20250219-thinking", &headers)

	got := headers.Values("anthropic-beta")
	if len(got) != 1 {
		t.Fatalf("expected duplicate values to collapse into one header, got %v", got)
	}
	expected := "output-128k-2025-02-19,token-efficient-tools-2025-02-19,computer-use-2025-01-24"
	if got[0] != expected {
		t.Fatalf("expected deduplicated merged header %q, got %q", expected, got[0])
	}
}

func TestClaudeSettingsWriteHeadersRemovesClaudeCodeBillingHeaderWhenEnabled(t *testing.T) {
	settings := &ClaudeSettings{
		RemoveClaudeCodeBillingHeaderEnabled: true,
		HeadersSettings: map[string]map[string][]string{
			"claude-3-7-sonnet-20250219": {
				ClaudeCodeBillingHeader: {"configured-billing"},
			},
		},
	}

	headers := http.Header{}
	headers.Set(ClaudeCodeBillingHeader, "client-billing")

	settings.WriteHeaders("claude-3-7-sonnet-20250219", &headers)

	if got := headers.Get(ClaudeCodeBillingHeader); got != "" {
		t.Fatalf("expected %s to be removed, got %q", ClaudeCodeBillingHeader, got)
	}
}

func TestClaudeSettingsWriteHeadersKeepsClaudeCodeBillingHeaderWhenDisabled(t *testing.T) {
	settings := &ClaudeSettings{
		RemoveClaudeCodeBillingHeaderEnabled: false,
	}

	headers := http.Header{}
	headers.Set(ClaudeCodeBillingHeader, "client-billing")

	settings.WriteHeaders("claude-3-7-sonnet-20250219", &headers)

	if got := headers.Get(ClaudeCodeBillingHeader); got != "client-billing" {
		t.Fatalf("expected %s to be preserved, got %q", ClaudeCodeBillingHeader, got)
	}
}
