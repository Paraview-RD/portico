// Package auth handles credentials and tokens: hashing passwords, issuing
// and verifying JWTs, and the middleware that turns a token into an
// authenticated principal.
package auth

import (
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is the only password rule the MVP enforces. Requirements
// §2.3 explicitly excludes a strong-password policy; this floor exists so a
// one-character password cannot be set at all.
const MinPasswordLength = 8

// maxPasswordLength is bcrypt's hard limit: it silently truncates beyond 72
// bytes, which would make two different long passwords interchangeable.
const maxPasswordLength = 72

// ErrPasswordTooShort and ErrPasswordTooLong are returned by ValidatePassword.
var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d bytes", maxPasswordLength)
)

// ValidatePassword checks a plaintext password against the length rules.
func ValidatePassword(plaintext string) error {
	if utf8.RuneCountInString(plaintext) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(plaintext) > maxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns a bcrypt hash suitable for storage.
func HashPassword(plaintext string) (string, error) {
	if err := ValidatePassword(plaintext); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether plaintext matches hash. It returns false for
// any mismatch, including a malformed stored hash, and never distinguishes
// the two to the caller.
func CheckPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}

// dummyHash is a valid bcrypt hash of a value nobody knows. Comparing
// against it costs the same as a real check.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// BurnPasswordComparison performs a throwaway bcrypt comparison. Login calls
// this when no such account exists so that a request for an unknown username
// takes as long as one for a known username, which stops the response time
// from revealing which accounts are real.
func BurnPasswordComparison() {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte("password"))
}
