// Package auth handles credentials and tokens: hashing passwords, issuing
// and verifying JWTs, and the middleware that turns a token into an
// authenticated principal.
package auth

import (
	"fmt"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLength is the floor, and it is not configurable.
//
// A tenant's policy can require more — see service.PasswordPolicy, which
// adds composition rules, reuse checks, and expiry on top of this. It cannot
// require less: a policy that could lower the floor would make the floor
// advisory, and the first deployment to set it to 4 would discover why it
// was there.
//
// It lives here rather than with the policy because auth is a leaf that the
// service layer depends on, and because HashPassword applies it — so there
// is no path to a stored hash that skipped it.
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

// hashCost is bcrypt's work factor, and it is a variable for exactly one
// reason: the test suite cannot afford the real one.
//
// bcrypt is deliberately slow, and the race detector makes it far slower still
// — every memory access in its inner loop gets instrumented. internal/server
// builds a full stack per test, 260 times, and each one hashes; at the real
// cost that package took 22 minutes of a 25-minute per-package budget, and
// almost all of it was here. See UseWeakHashingForTests.
//
// Unexported, so nothing outside this package can assign it. The only way in
// is the function below, and a test asserts that no shipped code calls it.
var hashCost = bcrypt.DefaultCost

// UseWeakHashingForTests lowers the work factor to bcrypt's minimum.
//
// Named to be unshippable. A hash produced at this cost is not fit to store:
// bcrypt's cost is exponential, so the minimum is roughly sixty-four times
// cheaper to attack than the default, which is the entire point of the
// default. TestNoShippedCodeWeakensHashing fails the build if this identifier
// appears in a file that is not a test.
//
// It is not reversible on purpose. A test that lowered the cost and put it
// back would leave whichever hashes it made in between at whichever cost
// happened to be current, and a test asserting something about a stored hash
// would then depend on ordering.
func UseWeakHashingForTests() {
	hashCost = bcrypt.MinCost
}

// HashPassword returns a bcrypt hash suitable for storage.
func HashPassword(plaintext string) (string, error) {
	if err := ValidatePassword(plaintext); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), hashCost)
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

// dummyHash is a valid bcrypt hash of a value nobody knows, at whatever cost
// this build hashes real passwords with.
//
// It used to be a literal — a cost-10 hash written into the source — which
// was correct only because bcrypt.DefaultCost is also 10. Nothing tied them.
// Raising hashCost, which is the one change anybody would ever make here,
// would have left the comparison below cheaper than a real one by a factor
// of two per step, and the timing difference this function exists to remove
// would have come back silently: an unknown username answering measurably
// faster than a known one is how a login page becomes a list of accounts.
//
// Computed once, on first use rather than at init, because
// UseWeakHashingForTests lowers the cost after the package is loaded — a
// value fixed at init would be a real-cost hash in a test suite whose real
// hashes are cheap, which is the same mismatch in the other direction.
var dummyHash = sync.OnceValue(func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("a value nobody knows"), hashCost)
	if err != nil {
		// bcrypt fails only on a cost outside its own bounds, which is a
		// programming error rather than a runtime condition. Continuing with
		// no hash would mean the burn silently stops burning.
		panic("auth: cannot build the comparison hash: " + err.Error())
	}
	return hash
})

// BurnPasswordComparison performs a throwaway bcrypt comparison. Login calls
// this when no such account exists so that a request for an unknown username
// takes as long as one for a known username, which stops the response time
// from revealing which accounts are real.
func BurnPasswordComparison() {
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte("password"))
}
