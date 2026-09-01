package common

import "strings"

// GetEmailDomain returns the normalized domain part of an email address.
func GetEmailDomain(email string) string {
	email = strings.TrimSpace(email)
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

func IsEmailDomainBlacklisted(email string) bool {
	return IsDomainListed(GetEmailDomain(email), EmailDomainBlacklist)
}

// IsDomainEmailRegistrationAllowed reports whether a verified password
// registration may bypass both invitation and registration code requirements.
func IsDomainEmailRegistrationAllowed(email string) bool {
	if !DomainEmailRegistrationEnabled || IsEmailDomainBlacklisted(email) {
		return false
	}
	return IsDomainListed(GetEmailDomain(email), DomainEmailRegistrationWhitelist)
}
