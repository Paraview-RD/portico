package server_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Portico's session is a bearer token the console keeps in localStorage,
// and localStorage is scoped to an origin. That is not a stylistic
// preference; it is what docs/deployment.md tells anybody considering
// giving each tenant its own subdomain, because two subdomains are two
// origins and share nothing, while two subdomains are *not* two cookie
// scopes unless the cookie is written host-only.
//
// This guards the premise of that paragraph and not its conclusion. It can
// see a cookie being set. It cannot see a Domain attribute chosen badly on
// the day somebody legitimately needs one — by then the call is correct Go
// and the mistake is a single field inside it. So the value here is the
// notification: the first cookie in this codebase should be a decision
// somebody made on purpose, having read why the attribute matters, rather
// than a line that arrived with a library example and was never looked at.
//
// zitadel/oidc is not searched, being outside this repository. Its op
// package sets no cookie either — the CookieHandler in its pkg/http is the
// relying-party side, and op.Config.CryptoKey encrypts tokens rather than
// cookies — which is checked by reading it, not by this test. A dependency
// upgrade that changed it would not fail here.
func TestNothingSetsACookie(t *testing.T) {
	// Both spellings: the helper, and the header written by hand. A cookie
	// set by assembling the header directly would be invisible to a search
	// for http.SetCookie alone.
	cookie := regexp.MustCompile(`http\.SetCookie\(|http\.Cookie\{|"Set-Cookie"`)

	// internal/ and cmd/, which together are the binary. examples/ is
	// deliberately outside it: the sample service provider there is
	// relying-party code, and a relying party holding its session in a
	// cookie is doing the correct thing. Failing on it would send whoever
	// wrote it to read a paragraph about tenant boundaries that does not
	// describe their program.
	var checked int
	for _, root := range []string{"../../internal", "../../cmd"} {
		if err := filepath.WalkDir(root, visit(t, cookie, &checked)); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	// A walk that found nothing would pass silently and prove nothing —
	// the failure mode of every test that searches for an absence.
	if checked < 100 {
		t.Fatalf("only %d Go files were searched, which is too few to have "+
			"covered internal/ and cmd/ — the walk is broken, not clean",
			checked)
	}
}

// visit is the walk function, shared by both roots.
func visit(t *testing.T, cookie *regexp.Regexp, checked *int) fs.WalkDirFunc {
	t.Helper()
	return func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Generated or built trees under internal/: the compiled
			// console and the built manual.
			switch entry.Name() {
			case "node_modules", "dist", "site":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Tests are exempt. A test for what happens when a client sends a
		// cookie has to construct one, and that is not this repository
		// serving cookies to anybody.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		*checked++
		if match := cookie.Find(source); match != nil {
			rel, _ := filepath.Rel("../..", path)
			t.Errorf("%s sets a cookie (%s).\n"+
				"If this is deliberate, read the subdomain section of "+
				"docs/deployment.md first — a cookie without Domain is "+
				"host-only and a cookie with it is not, and that difference "+
				"is a tenant boundary. Then update that section and this "+
				"test together.", rel, match)
		}
		return nil
	}
}
