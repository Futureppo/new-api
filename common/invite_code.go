package common

import (
	"crypto/rand"
	"math/big"
	"strings"
)

const (
	InviteCodeLength   = 16
	inviteCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

var inviteCodeAlphabetSize = big.NewInt(int64(len(inviteCodeAlphabet)))

// GenerateInviteCode returns a cryptographically secure, human-friendly invite code.
func GenerateInviteCode() (string, error) {
	code := make([]byte, InviteCodeLength)
	for i := range code {
		index, err := rand.Int(rand.Reader, inviteCodeAlphabetSize)
		if err != nil {
			return "", err
		}
		code[i] = inviteCodeAlphabet[index.Int64()]
	}
	return string(code), nil
}

func NormalizeInviteCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func IsValidInviteCode(code string) bool {
	if len(code) != InviteCodeLength {
		return false
	}
	for i := range code {
		if !strings.ContainsRune(inviteCodeAlphabet, rune(code[i])) {
			return false
		}
	}
	return true
}
