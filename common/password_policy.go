package common

import (
	"crypto/rand"
	"errors"
	"math/big"
	"unicode/utf8"
)

const PasswordMaxBytes = 72

var ErrPasswordPolicy = errors.New("user.password_policy")

// ValidateLoginPassword applies only to newly chosen login passwords, not
// existing credentials or other values hashed by Password2Hash (e.g. 2FA codes).
func ValidateLoginPassword(password string) error {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 8 || len(password) > PasswordMaxBytes {
		return ErrPasswordPolicy
	}
	var digit, upper, lower, symbol bool
	for _, ch := range password {
		switch {
		case ch >= '0' && ch <= '9':
			digit = true
		case ch >= 'A' && ch <= 'Z':
			upper = true
		case ch >= 'a' && ch <= 'z':
			lower = true
		case ch >= '!' && ch <= '/' || ch >= ':' && ch <= '@' || ch >= '[' && ch <= '`' || ch >= '{' && ch <= '~':
			symbol = true
		}
	}
	if !digit || !upper || !lower || !symbol {
		return ErrPasswordPolicy
	}
	return nil
}

// GenerateLoginPassword uses rejection sampling so every accepted password is
// uniformly sampled from the printable ASCII alphabet and satisfies the policy.
func GenerateLoginPassword() (string, error) {
	for {
		password := make([]byte, 16)
		for i := range password {
			n, err := rand.Int(rand.Reader, big.NewInt(94))
			if err != nil {
				return "", err
			}
			password[i] = byte(n.Int64()) + '!'
		}
		if ValidateLoginPassword(string(password)) == nil {
			return string(password), nil
		}
	}
}
