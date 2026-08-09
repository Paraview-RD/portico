package secrets_test

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/paraview/portico/internal/secrets"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, secrets.KeyLength)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("read key: %v", err)
	}
	return key
}

func newVault(t *testing.T) *secrets.Vault {
	t.Helper()
	vault, err := secrets.NewVault(newKey(t))
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	return vault
}

func TestSealedCredentialComesBackIntact(t *testing.T) {
	vault := newVault(t)

	// A bind password with the things that break naive encoding: non-ASCII,
	// a NUL, and enough length to cross a block boundary.
	original := "p@ssw0rd — 密码\x00" + strings.Repeat("x", 40)

	sealed, err := vault.Seal(original)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(sealed, "p@ssw0rd") {
		t.Fatal("the sealed form contains the plaintext, so this is not encryption")
	}

	opened, err := vault.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if opened != original {
		t.Errorf("opened %q, want %q", opened, original)
	}
}

// The same plaintext must not seal to the same ciphertext twice.
//
// Not an aesthetic point. Two directory connectors configured with the same
// service account would otherwise be visibly identical in the database, and
// anyone with read access learns that a password was reused without breaking
// anything.
func TestSealingTwiceProducesDifferentCiphertext(t *testing.T) {
	vault := newVault(t)

	first, err := vault.Seal("same-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := vault.Seal("same-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if first == second {
		t.Error("identical plaintexts sealed identically; the nonce is not " +
			"random, so equal credentials are visible as equal in the database")
	}
}

// A modified ciphertext must fail rather than decrypt to something else.
// This is the property GCM is chosen for, so it is the property asserted.
func TestTamperedCiphertextIsRefused(t *testing.T) {
	vault := newVault(t)

	sealed, err := vault.Seal("bind-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Flip a character in the base64 body. Whichever byte it lands on, the
	// authentication tag no longer matches.
	tampered := []byte(sealed)
	for i := range tampered {
		if tampered[i] != 'A' {
			tampered[i] = 'A'
			break
		}
	}

	if _, err := vault.Open(string(tampered)); !errors.Is(err, secrets.ErrCorrupt) {
		t.Errorf("opening tampered ciphertext = %v, want ErrCorrupt; an "+
			"unauthenticated cipher would have returned attacker-influenced "+
			"plaintext instead", err)
	}
}

func TestAnotherKeyCannotOpenIt(t *testing.T) {
	sealed, err := newVault(t).Seal("bind-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := newVault(t).Open(sealed); !errors.Is(err, secrets.ErrCorrupt) {
		t.Errorf("opening under a different key = %v, want ErrCorrupt", err)
	}
}

// An unconfigured deployment must refuse to store a credential, not store it
// in the clear. The nil vault is the shape that makes forgetting impossible.
func TestUnconfiguredVaultRefusesToStoreAnything(t *testing.T) {
	var vault *secrets.Vault

	if vault.Configured() {
		t.Error("a nil vault reports itself as configured")
	}
	if _, err := vault.Seal("bind-password"); !errors.Is(err, secrets.ErrNotConfigured) {
		t.Errorf("sealing with no key = %v, want ErrNotConfigured; anything "+
			"else risks the value being written in the clear", err)
	}
}

// Empty is absent, not encrypted-empty: an anonymous bind is a real
// configuration and should read as blank in the database.
func TestEmptyStaysEmpty(t *testing.T) {
	vault := newVault(t)

	sealed, err := vault.Seal("")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed != "" {
		t.Errorf("sealed empty to %q, want empty", sealed)
	}

	// And opening empty works even with no key at all, so reading a row that
	// holds no credential never depends on configuration.
	var unconfigured *secrets.Vault
	opened, err := unconfigured.Open("")
	if err != nil || opened != "" {
		t.Errorf("opening empty = (%q, %v), want (\"\", nil)", opened, err)
	}
}

func TestKeyMustBeExactlyThirtyTwoBytes(t *testing.T) {
	for _, length := range []int{0, 16, 31, 33, 64} {
		if _, err := secrets.NewVault(make([]byte, length)); err == nil {
			t.Errorf("a %d-byte key was accepted; only AES-256 is intended, "+
				"and a shorter key would silently select a weaker cipher", length)
		}
	}
}
