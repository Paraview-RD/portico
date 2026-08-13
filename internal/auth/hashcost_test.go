package auth_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/Paraview-RD/portico/internal/auth"
)

// The one knob in this package that weakens a security property.
//
// UseWeakHashingForTests exists because bcrypt at its real cost dominated the
// test suite — internal/server builds a full stack per test, and the race
// detector makes each hash far more expensive than it looks. The saving is
// real and so is the hazard: a hash written at bcrypt's minimum cost is
// roughly sixty-four times cheaper to attack than one written at the default,
// which is what the default is for.
//
// So the knob is allowed and its misuse is made loud, which is the same trade
// this repository makes elsewhere — see the error-code guard, which reads a
// TypeScript bundle as text for the same reason.

// TestNoShippedCodeWeakensHashing walks the repository and fails if anything
// that is not a test calls it.
//
// Text rather than types, because there is no type-level way to say "only
// from a test". A call site in a non-test file compiles perfectly well and
// would produce stored hashes nobody could tell apart from real ones.
func TestNoShippedCodeWeakensHashing(t *testing.T) {
	const knob = "UseWeakHashingForTests"

	root := filepath.Join("..", "..")
	var offenders []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Nothing to find in dependencies or build output, and walking
			// them makes this slow enough that somebody would delete it.
			switch info.Name() {
			case ".git", "node_modules", "dist", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// The declaration itself lives in a non-test file, which is the point
		// — a test in another package has to be able to call it.
		if strings.HasSuffix(path, filepath.Join("internal", "auth", "password.go")) {
			return nil
		}
		if strings.Contains(string(source), knob) {
			offenders = append(offenders, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	if len(offenders) > 0 {
		t.Errorf("%s is called from code that ships:\n  %s\n\n"+
			"A hash written at bcrypt's minimum cost is about sixty-four times "+
			"cheaper to attack than one written at the default, and nothing "+
			"about the stored value says which it is. If a slow hash is the "+
			"problem, the answer is fewer hashes, not weaker ones.",
			knob, strings.Join(offenders, "\n  "))
	}
}

// The default is what an unconfigured process uses.
//
// This test does not call the knob, so it observes the shipped cost — which
// is the only place in the suite where that is true, because TestMain in the
// packages that need speed calls it before anything else.
func TestAHashIsWrittenAtTheRealCostUnlessAskedOtherwise(t *testing.T) {
	hash, err := auth.HashPassword("a-password-long-enough")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("read the cost back: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("a hash was written at cost %d, want bcrypt's default of %d. "+
			"Either the knob leaked into this package's tests, or the shipped "+
			"cost has been changed without changing this test.",
			cost, bcrypt.DefaultCost)
	}
}

// The comparison burned for an unknown username must cost what a real one
// costs.
//
// BurnPasswordComparison exists so that a sign-in for an account that does
// not exist takes as long as one for an account that does — otherwise the
// response time answers a question the response body carefully does not.
// What it compares against used to be a hash written into the source at
// cost 10, which agreed with bcrypt.DefaultCost by coincidence and by
// nothing else.
//
// bcrypt's cost is exponential, so the two drifting apart by one step makes
// the unknown-username path twice as fast, and by three steps eight times.
// Nothing would have failed. The whole property is a timing one, and the
// only place it shows up is in measurements nobody takes.
func TestTheBurnedComparisonCostsWhatARealOneCosts(t *testing.T) {
	if got, want := auth.BurnComparisonCost(), auth.CurrentHashCost(); got != want {
		t.Errorf("BurnPasswordComparison compares against a cost-%d hash while "+
			"passwords are stored at cost %d.\n"+
			"A sign-in for an unknown username is therefore %dx faster than one "+
			"for a known username, which is the timing difference that function "+
			"exists to remove.",
			got, want, 1<<max(0, want-got))
	}
}
