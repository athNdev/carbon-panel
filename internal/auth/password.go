package auth

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrPasswordTooWeak is returned when a password does not satisfy the policy.
var ErrPasswordTooWeak = errors.New("password does not meet the minimum requirements")

const (
	// MinPasswordLength is the shortest accepted local password.
	MinPasswordLength = 12
	// MaxPasswordLength bounds the bcrypt input: bcrypt only considers the
	// first 72 bytes, so longer inputs must be rejected rather than silently
	// truncated.
	MaxPasswordLength = 72
)

// commonWeakPasswords are rejected even when they satisfy the length and
// character-class rules.
var commonWeakPasswords = map[string]struct{}{
	"password":       {},
	"password1":      {},
	"password123":    {},
	"passw0rd":       {},
	"123456789012":   {},
	"1234567890123":  {},
	"qwertyuiopas":   {},
	"qwertyuiop":     {},
	"letmein12345":   {},
	"administrator":  {},
	"changeme1234":   {},
	"iloveyou1234":   {},
	"carbonpanel":    {},
	"carbonpanel123": {},
	"carbon-panel":   {},
	"minecraft":      {},
	"minecraft123":   {},
}

// ValidatePassword enforces the minimum policy for local accounts: at least
// MinPasswordLength characters, at most MaxPasswordLength bytes, at least two
// of four character classes, and not a well-known weak password.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("%w: must be at least %d characters", ErrPasswordTooWeak, MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("%w: must be at most %d bytes", ErrPasswordTooWeak, MaxPasswordLength)
	}

	var hasLower, hasUpper, hasDigit, hasSymbol bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSymbol = true
		}
	}

	classes := 0
	for _, present := range []bool{hasLower, hasUpper, hasDigit, hasSymbol} {
		if present {
			classes++
		}
	}
	if classes < 2 {
		return fmt.Errorf("%w: use at least two of lowercase, uppercase, digits and symbols", ErrPasswordTooWeak)
	}

	if _, weak := commonWeakPasswords[strings.ToLower(password)]; weak {
		return fmt.Errorf("%w: this password is too common", ErrPasswordTooWeak)
	}

	return nil
}
