package secrets_test

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/secrets"
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

	sealed, err := vault.Seal(binding, original)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(sealed, "p@ssw0rd") {
		t.Fatal("the sealed form contains the plaintext, so this is not encryption")
	}

	opened, err := vault.Open(binding, sealed)
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

	first, err := vault.Seal(binding, "same-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := vault.Seal(binding, "same-password")
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
// binding is what every test here seals under unless it is about bindings.
var binding = secrets.Binding{Purpose: "test-purpose", TenantID: "tenant-1"}

func TestTamperedCiphertextIsRefused(t *testing.T) {
	vault := newVault(t)

	sealed, err := vault.Seal(binding, "bind-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Flip a character in the base64 body — past the prefix, so this is the
	// authentication tag refusing rather than base64 failing to decode.
	// Whichever byte it lands on, the tag no longer matches.
	tampered := []byte(sealed)
	for i := len("b.") + 1; i < len(tampered); i++ {
		if tampered[i] != 'A' {
			tampered[i] = 'A'
			break
		}
	}

	if _, err := vault.Open(binding, string(tampered)); !errors.Is(err, secrets.ErrCorrupt) {
		t.Errorf("opening tampered ciphertext = %v, want ErrCorrupt; an "+
			"unauthenticated cipher would have returned attacker-influenced "+
			"plaintext instead", err)
	}
}

func TestAnotherKeyCannotOpenIt(t *testing.T) {
	sealed, err := newVault(t).Seal(binding, "bind-password")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := newVault(t).Open(binding, sealed); !errors.Is(err, secrets.ErrCorrupt) {
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
	if _, err := vault.Seal(binding, "bind-password"); !errors.Is(err, secrets.ErrNotConfigured) {
		t.Errorf("sealing with no key = %v, want ErrNotConfigured; anything "+
			"else risks the value being written in the clear", err)
	}
}

// Empty is absent, not encrypted-empty: an anonymous bind is a real
// configuration and should read as blank in the database.
func TestEmptyStaysEmpty(t *testing.T) {
	vault := newVault(t)

	sealed, err := vault.Seal(binding, "")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed != "" {
		t.Errorf("sealed empty to %q, want empty", sealed)
	}

	// And opening empty works even with no key at all, so reading a row that
	// holds no credential never depends on configuration.
	var unconfigured *secrets.Vault
	opened, err := unconfigured.Open(binding, "")
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

// A sealed value opens only for what it was sealed for.
//
// Everything in this package assumed the key was the whole answer: hold it
// and you can open anything, hold it not and you can open nothing. That left
// a ciphertext portable. Anybody able to write to the database could lift
// one column's value into another — a webhook's headers into a directory's
// bind password, one tenant's credential into another tenant's row — and the
// server would decrypt it and use it, because it was sealed under the key it
// is being opened with.
//
// Whoever can do that already has write access, so this is not the
// difference between safe and compromised. It is the difference between a
// credential that can be moved around silently and one that cannot.
func TestAValueSealedForOnePurposeDoesNotOpenAsAnother(t *testing.T) {
	vault := newVault(t)

	sealed, err := vault.Seal(
		secrets.Binding{Purpose: secrets.PurposeWebhookHeaders, TenantID: "tenant-1"},
		"authorization: Bearer somebody-elses-token")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	for name, wrong := range map[string]secrets.Binding{
		"as another kind of secret": {
			Purpose: secrets.PurposeDirectoryBindPassword, TenantID: "tenant-1"},
		"as another tenant's": {
			Purpose: secrets.PurposeWebhookHeaders, TenantID: "tenant-2"},
		"as neither": {
			Purpose: secrets.PurposeDirectoryBindPassword, TenantID: "tenant-2"},
	} {
		if _, err := vault.Open(wrong, sealed); !errors.Is(err, secrets.ErrCorrupt) {
			t.Errorf("opening %s = %v, want ErrCorrupt. A ciphertext that opens "+
				"under a binding it was not sealed under can be moved between "+
				"rows by anybody who can write to the database.", name, err)
		}
	}

	// And under its own binding it still opens, so the check above is not
	// passing because nothing opens at all.
	opened, err := vault.Open(
		secrets.Binding{Purpose: secrets.PurposeWebhookHeaders, TenantID: "tenant-1"},
		sealed)
	if err != nil {
		t.Fatalf("open under its own binding: %v", err)
	}
	if opened != "authorization: Bearer somebody-elses-token" {
		t.Errorf("round trip returned %q", opened)
	}
}

// Values written before bindings existed keep opening.
//
// Every deployment already holds some: a directory's bind password, a
// subscription's headers. Refusing them would mean an upgrade silently
// taking those credentials away — a certain harm traded for a hypothetical
// one — so an unprefixed value is opened the way it was written, and gains
// the binding the next time it is saved.
func TestAValueSealedBeforeBindingsStillOpens(t *testing.T) {
	vault := newVault(t)

	legacy, err := secrets.SealUnboundForTests(vault, "bind-password")
	if err != nil {
		t.Fatalf("seal the old way: %v", err)
	}

	opened, err := vault.Open(binding, legacy)
	if err != nil {
		t.Fatalf("open a value written before bindings: %v.\n"+
			"Every existing deployment holds values in this format; refusing "+
			"them turns an upgrade into a credential loss.", err)
	}
	if opened != "bind-password" {
		t.Errorf("round trip returned %q, want %q", opened, "bind-password")
	}
}
