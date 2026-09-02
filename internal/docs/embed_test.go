package docs_test

// The manual is served out of the binary, and the two things worth asserting
// are the ones that would be discovered by a reader rather than by a build:
// that a page reached by its directory URL is the page, and that a mistyped
// address says so instead of quietly handing back the home page.

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Paraview-RD/portico/internal/docs"
)

func get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	docs.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestTheManualIsServedAtItsMountPoint(t *testing.T) {
	if !docs.Available() {
		t.Skip("manual not built; run ./hack/build-docs.sh")
	}

	rec := get(t, docs.Prefix+"/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Error("the home page is not HTML")
	}
}

// mkdocs writes ldap/index.html and links to it as /docs/ldap/. A file
// server alone answers 404 for that, which would break every link in the
// navigation while the pages themselves existed.
func TestADirectoryURLFindsItsPage(t *testing.T) {
	if !docs.Available() {
		t.Skip("manual not built; run ./hack/build-docs.sh")
	}

	rec := get(t, docs.Prefix+"/ldap/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a directory URL", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "objectGUID") {
		t.Error("the page served is not the directory page")
	}
}

func TestAPageThatDoesNotExistSaysSo(t *testing.T) {
	if !docs.Available() {
		t.Skip("manual not built; run ./hack/build-docs.sh")
	}

	rec := get(t, docs.Prefix+"/no-such-page/")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404. Falling back to the home page here "+
			"would tell a reader their address exists and is about something "+
			"else — the opposite of what the application shell should do, "+
			"because a manual has exactly the pages that were built",
			rec.Code)
	}
}

func TestTheChineseManualIsServedUnderItsOwnPrefix(t *testing.T) {
	if !docs.Available() {
		t.Skip("manual not built; run ./hack/build-docs.sh")
	}

	rec := get(t, docs.Prefix+"/zh/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// The lang attribute, which the build sets from the Chinese config, and
	// not a sentence off the page. This used to look for a phrase from
	// index.zh.md, and the phrase was reworded — so the test failed for
	// somebody rewriting a paragraph, and said "the Chinese home page is not
	// the Chinese one" about a page that was. What it means to assert is
	// that this prefix serves the Chinese build rather than the English one,
	// and that only stops being true if the build stops doing it.
	if !strings.Contains(rec.Body.String(), `<html lang="zh"`) {
		t.Error("/docs/zh/ is not the Chinese build; it has no <html lang=\"zh\">")
	}
}

// BrandingPage's "view docs" link jumps straight to the section rather
// than the top of settings/ — see the doc comment on DocsLink in
// web/src/components/ui.tsx. mkdocs slugifies a heading into that
// heading's own text, so the id it produces for a Chinese "## 品牌定制"
// is 品牌定制, not branding, and the two ids are two literal strings in a
// TSX file with no compiler check tying them to what a build actually
// produces — this is the second time that link has silently gone stale;
// the first time it carried an English id into the Chinese build.
// Fragments never leave the browser, so the check is the same as a
// reader's: does the page this loads actually contain that id anywhere.
func TestTheBrandingDocsLinkAnchorsExistInBothLanguages(t *testing.T) {
	if !docs.Available() {
		t.Skip("manual not built; run ./hack/build-docs.sh")
	}

	cases := map[string]string{
		docs.Prefix + "/settings/":    `id="branding"`,
		docs.Prefix + "/zh/settings/": `id="品牌定制"`,
	}
	for path, want := range cases {
		rec := get(t, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s does not contain %s — the branding page's "+
				"\"view docs\" link for this language lands on the page "+
				"top instead of the section", path, want)
		}
	}
}

func TestLocalePathSendsAReaderToTheirOwnLanguage(t *testing.T) {
	cases := map[string]string{
		"zh-CN":   docs.Prefix + "/zh/",
		"zh":      docs.Prefix + "/zh/",
		"zh-Hans": docs.Prefix + "/zh/",
		"en-US":   docs.Prefix + "/",
		// A language the manual is not built in goes to the default rather
		// than to a path that does not exist.
		"de-DE": docs.Prefix + "/",
		"":      docs.Prefix + "/",
	}
	for locale, want := range cases {
		if got := docs.LocalePath(locale); got != want {
			t.Errorf("LocalePath(%q) = %q, want %q", locale, got, want)
		}
	}
}

// Every inline script in the built manual has a hash the policy will admit.
//
// The property is coverage, not correctness of any one digest: a browser
// blocks a script whose hash is absent, and the way that shows up is a page
// that renders and does nothing. So this walks the site independently of the
// code under test and asserts that nothing was missed — a second parser
// disagreeing with the first is exactly the failure worth catching, and a
// test that called the same function twice would catch nothing.
func TestEveryInlineScriptInTheManualIsAdmittedByThePolicy(t *testing.T) {
	if !docs.Available() {
		t.Skip("manual not built; run ./hack/build-docs.sh")
	}

	admitted := map[string]bool{}
	for _, hash := range docs.InlineScriptHashes() {
		admitted[hash] = true
	}
	if len(admitted) == 0 {
		t.Fatal("no hashes at all, so every inline script in the manual is blocked")
	}

	// Deliberately a different implementation from the one being checked.
	pattern := regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)
	hasSrc := regexp.MustCompile(`(?s)^<script[^>]*\ssrc=`)

	var checked int
	err := fs.WalkDir(os.DirFS("site"), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(name, ".html") {
			return err
		}
		body, err := os.ReadFile(filepath.Join("site", name))
		if err != nil {
			return err
		}
		for _, match := range pattern.FindAllSubmatch(body, -1) {
			if hasSrc.Match(match[0]) {
				continue
			}
			checked++
			sum := sha256.Sum256(match[1])
			hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
			if !admitted[hash] {
				t.Errorf("%s: an inline script has no hash in the policy, so a "+
					"browser will block it: %s", name, hash)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the built site: %v", err)
	}
	if checked == 0 {
		t.Fatal("found no inline scripts to check, so this asserted nothing")
	}
	t.Logf("%d inline scripts across the manual, %d distinct hashes", checked, len(admitted))
}
