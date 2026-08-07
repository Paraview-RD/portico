package auth_test

import (
	"strings"
	"testing"

	"github.com/paraview/portico/internal/auth"
)

func TestHashPasswordThenCheck(t *testing.T) {
	const plaintext = "correct-horse-battery"

	hash, err := auth.HashPassword(plaintext)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == plaintext {
		t.Fatal("the password was stored in plaintext")
	}
	if !auth.CheckPassword(hash, plaintext) {
		t.Error("the correct password did not verify")
	}
	if auth.CheckPassword(hash, "wrong-password") {
		t.Error("an incorrect password verified")
	}
}

// bcrypt salts each hash, so the same password must not produce the same
// stored value twice.
func TestHashPasswordIsSalted(t *testing.T) {
	const plaintext = "correct-horse-battery"

	first, err := auth.HashPassword(plaintext)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := auth.HashPassword(plaintext)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if first == second {
		t.Error("two hashes of the same password are identical; the salt is not working")
	}
	if !auth.CheckPassword(second, plaintext) {
		t.Error("the second hash does not verify")
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantError bool
	}{
		{"at the minimum", strings.Repeat("a", auth.MinPasswordLength), false},
		{"comfortably long", "a-perfectly-reasonable-password", false},
		{"one short", strings.Repeat("a", auth.MinPasswordLength-1), true},
		{"empty", "", true},
		// bcrypt silently truncates past 72 bytes, which would make two
		// different long passwords interchangeable. Reject instead.
		{"past bcrypt's limit", strings.Repeat("a", 73), true},
		// Length is counted in runes, so a short multi-byte password is
		// still rejected rather than passing on byte count.
		{"short multibyte", "密码密码", true},
		{"long enough multibyte", "密码密码密码密码", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidatePassword(tt.password)
			if tt.wantError && err == nil {
				t.Error("expected an error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestHashPasswordRejectsInvalidInput(t *testing.T) {
	if _, err := auth.HashPassword("short"); err == nil {
		t.Error("a too-short password was hashed")
	}
}

func TestCheckPasswordRejectsMalformedHash(t *testing.T) {
	// A corrupt or empty stored hash must fail closed, not panic and not
	// accidentally match.
	for _, hash := range []string{"", "not-a-bcrypt-hash", "$2a$10$tooshort"} {
		if auth.CheckPassword(hash, "anything") {
			t.Errorf("CheckPassword accepted a malformed hash %q", hash)
		}
	}
}
