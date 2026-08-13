package secrets

import (
	"crypto/rand"
	"encoding/base64"
)

// SealUnboundForTests produces what Seal produced before bindings existed:
// no additional data, no prefix.
//
// It is here so a test can prove that such a value still opens, which is the
// upgrade path for every credential already stored in every deployment. The
// alternative is a fixture — a base64 string checked into the test file —
// which would be sealed under a key that test would then have to hold, and
// would say nothing about the code that actually wrote those values.
//
// In a _test.go file, so no build of the product can produce an unbound
// value.
func SealUnboundForTests(v *Vault, plaintext string) (string, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(v.aead.Seal(nonce, nonce, []byte(plaintext), nil)), nil
}
