package docs_test

// The manual is served out of the binary, and the two things worth asserting
// are the ones that would be discovered by a reader rather than by a build:
// that a page reached by its directory URL is the page, and that a mistyped
// address says so instead of quietly handing back the home page.

import (
	"net/http"
	"net/http/httptest"
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
	if !strings.Contains(rec.Body.String(), "这是你正在运行") {
		t.Error("the Chinese home page is not the Chinese one")
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
