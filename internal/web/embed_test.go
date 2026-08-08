package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What a browser is allowed to keep.
//
// The failure this guards against does not look like a bug: everything works,
// on a build nobody is running any more. A cached index.html names asset
// hashes the new binary does not serve, so a deploy reaches nobody until they
// clear their cache — and the people who report it are the ones who never do.
//
// Both directions matter. A cached shell is a deploy that does not arrive; an
// uncached hashed asset is every page load refetching bytes that cannot have
// changed.
//
// An internal test rather than web_test, so that listing what was actually
// embedded does not require exporting a function for the benefit of a test.

func serve(t *testing.T, path string) http.Header {
	t.Helper()
	if !Available() {
		t.Skip("no built frontend is embedded; run npm run build, then rebuild")
	}
	recorder := httptest.NewRecorder()
	Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder.Result().Header
}

func TestTheAppShellIsNeverServedFromCacheWithoutAsking(t *testing.T) {
	// The last two are client-side routes rather than files. They get the
	// shell, so they have to get the shell's caching too — /users because it
	// is what a browser actually asks for when somebody bookmarks a screen,
	// and the other because its path starts with the prefix the caching rule
	// keys on. A route under /assets that is not a file still gets the shell,
	// and telling a browser to keep that copy for a year would pin one person
	// to one build until they cleared their cache.
	for _, path := range []string{"/", "/index.html", "/users", "/assets/not-a-file"} {
		got := serve(t, path).Get("Cache-Control")
		if got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want \"no-cache\". A cached shell "+
				"names asset hashes that a later build does not serve.", path, got)
		}
	}
}

func TestHashedAssetsAreCachedForever(t *testing.T) {
	asset := anyAsset(t)

	got := serve(t, "/"+asset).Get("Cache-Control")
	if !strings.Contains(got, "immutable") || !strings.Contains(got, "max-age=") {
		t.Errorf("%s: Cache-Control = %q, want a long max-age and immutable. "+
			"The name carries a content hash, so the bytes cannot change.",
			asset, got)
	}
}

// anyAsset returns a built asset path, rather than hard-coding a hashed
// filename that changes on every build.
func anyAsset(t *testing.T) string {
	t.Helper()
	if !Available() {
		t.Skip("no built frontend is embedded")
	}

	var found string
	err := fs.WalkDir(assets(), "assets", func(path string, entry fs.DirEntry, _ error) error {
		if entry == nil || entry.IsDir() || found != "" {
			return nil
		}
		found = path
		return fs.SkipAll
	})
	if err != nil {
		t.Skipf("the build produced no assets directory: %v", err)
	}
	if found == "" {
		t.Skip("the build produced no hashed assets")
	}
	return found
}
