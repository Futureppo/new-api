package common

// IsPublicErrorCode accepts compact machine-readable codes, not arbitrary
// upstream messages, URLs or serialized objects placed in code/type fields.
func IsPublicErrorCode(code string) bool {
	if len(code) == 0 || len(code) > 128 {
		return false
	}
	for _, ch := range code {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' || ch == ':' {
			continue
		}
		return false
	}
	return true
}
