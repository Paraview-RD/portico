// Package secrets encrypts the few credentials this server has to store and
// later use, as opposed to the many it only has to verify.
//
// Almost everything here is a hash: a password, a client secret, a SCIM
// token. Those are checked against something a caller presents, so the
// original never has to be recoverable, and that is much the safer
// arrangement — a stolen database yields nothing usable.
//
// A directory connector breaks that pattern, and it is worth being precise
// about why rather than treating it as an exception. Portico binds to
// somebody's AD as a service account. It has to send that password, on a
// schedule, unattended. There is no version of that where the value is
// unrecoverable, so the only question is what protects it at rest.
//
// The answer here is AES-256-GCM under a key that lives in the environment
// rather than the database, so a dump of the database is not enough. That is
// the honest limit of what this buys: an operator who can read the process
// environment can read the credentials, and nothing short of an HSM changes
// that. It defends against the leak that actually happens — a backup, a
// replica, a snapshot handed to somebody for debugging.
//
// GCM rather than CBC because it authenticates: a modified ciphertext fails
// to open instead of decrypting to something attacker-influenced. The nonce
// is random per message and stored in front of the ciphertext, which is the
// standard construction; at 12 bytes, collisions are not a concern until far
// more messages than a settings table will ever hold.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeyLength is the key size this package requires: AES-256.
//
// Not "at least" — exactly. A shorter key would silently select AES-128 or
// fail deep inside the cipher, and accepting a longer one would invite
// passing a passphrase where a key belongs.
const KeyLength = 32

// ErrNotConfigured is returned by a nil Vault. Every caller has to handle it
// rather than storing the value in the clear, which is the whole point of
// making the zero value refuse to work.
var ErrNotConfigured = errors.New("no encryption key is configured; set PORTICO_ENCRYPTION_KEY")

// ErrCorrupt is returned when a stored value cannot be opened: it was
// truncated, it was modified, or it was written under a different key.
//
// The three are deliberately not distinguished. A caller can do nothing
// different about any of them, and reporting "wrong key" as distinct from
// "modified" tells anyone who can provoke the error which of the two they
// achieved.
var ErrCorrupt = errors.New("stored secret cannot be decrypted")

// Vault seals and opens stored credentials.
//
// A nil *Vault is valid and refuses everything with ErrNotConfigured, so a
// deployment that has configured no key runs normally right up until
// somebody tries to store a credential — and is then told why, instead of
// having the value written in the clear.
type Vault struct {
	aead cipher.AEAD
}

// NewVault builds a vault from a key of exactly KeyLength bytes.
func NewVault(key []byte) (*Vault, error) {
	if len(key) != KeyLength {
		return nil, fmt.Errorf("encryption key must be %d bytes, got %d", KeyLength, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build GCM: %w", err)
	}
	return &Vault{aead: aead}, nil
}

// Configured reports whether this vault can actually seal anything, so a
// caller can refuse a request up front rather than at the point of writing.
func (v *Vault) Configured() bool { return v != nil && v.aead != nil }

// Seal encrypts a credential for storage, returning base64 text safe to put
// in a text column.
//
// An empty plaintext seals to empty rather than to a ciphertext. "No bind
// password" is a real configuration — an anonymous bind — and it should read
// as absent in the database rather than as an encrypted empty string that
// nobody can tell apart from a real one without the key.
func (v *Vault) Seal(plaintext string) (string, error) {
	if !v.Configured() {
		return "", ErrNotConfigured
	}
	if plaintext == "" {
		return "", nil
	}

	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}

	sealed := v.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts what Seal produced.
func (v *Vault) Open(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !v.Configured() {
		return "", ErrNotConfigured
	}

	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", ErrCorrupt
	}
	if len(raw) < v.aead.NonceSize() {
		return "", ErrCorrupt
	}

	nonce, body := raw[:v.aead.NonceSize()], raw[v.aead.NonceSize():]
	plaintext, err := v.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", ErrCorrupt
	}
	return string(plaintext), nil
}
