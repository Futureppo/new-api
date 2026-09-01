package common

import "testing"

func TestDomainEmailRegistrationPolicy(t *testing.T) {
	originalEnabled := DomainEmailRegistrationEnabled
	originalWhitelist := append([]string(nil), DomainEmailRegistrationWhitelist...)
	originalBlacklist := append([]string(nil), EmailDomainBlacklist...)
	t.Cleanup(func() {
		DomainEmailRegistrationEnabled = originalEnabled
		DomainEmailRegistrationWhitelist = originalWhitelist
		EmailDomainBlacklist = originalBlacklist
	})

	DomainEmailRegistrationEnabled = true
	DomainEmailRegistrationWhitelist = []string{"*.trusted.test"}
	EmailDomainBlacklist = []string{"*.blocked.trusted.test"}

	if !IsDomainEmailRegistrationAllowed("user@mail.trusted.test") {
		t.Fatal("expected configured domain email to qualify for code-free registration")
	}
	if IsDomainEmailRegistrationAllowed("user@mail.blocked.trusted.test") {
		t.Fatal("expected blacklist to override domain email whitelist")
	}
	if !IsEmailDomainBlacklisted("user@BLOCKED.TRUSTED.TEST") {
		t.Fatal("expected wildcard blacklist to match root domain case-insensitively")
	}
	if IsDomainEmailRegistrationAllowed("user@example.com") {
		t.Fatal("expected unconfigured domain email not to qualify")
	}

	DomainEmailRegistrationEnabled = false
	if IsDomainEmailRegistrationAllowed("user@mail.trusted.test") {
		t.Fatal("expected disabled feature not to bypass registration codes")
	}
}

func TestGetEmailDomain(t *testing.T) {
	tests := map[string]string{
		" Student@Mail.SWJTU.edu.cn ": "mail.swjtu.edu.cn",
		"invalid":                     "",
		"missing-domain@":             "",
	}
	for input, want := range tests {
		if got := GetEmailDomain(input); got != want {
			t.Fatalf("GetEmailDomain(%q) = %q, want %q", input, got, want)
		}
	}
}
