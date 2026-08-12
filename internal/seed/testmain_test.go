package seed_test

import (
	"os"
	"testing"

	"github.com/Paraview-RD/portico/internal/auth"
)

// Passwords are hashed at bcrypt's minimum cost for this package's tests.
//
// Not a micro-optimisation. bcrypt is deliberately slow and the race detector
// makes it far slower — it instruments every memory access in the inner loop.
// This package builds a full stack per test and each one hashes; at the
// shipped cost that put it at 22 minutes of a 25-minute per-package budget,
// with almost all of the time in bcrypt rather than in anything under test.
//
// Nothing here asserts anything about how expensive a stored hash is. The
// package that does is internal/auth, whose own tests deliberately do not
// call this — see TestAHashIsWrittenAtTheRealCostUnlessAskedOtherwise, and
// TestNoShippedCodeWeakensHashing for the guard that keeps this out of code
// that ships.
func TestMain(m *testing.M) {
	auth.UseWeakHashingForTests()
	os.Exit(m.Run())
}
