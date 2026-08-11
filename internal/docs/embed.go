// Package docs serves the manual from inside the binary, so the pages an
// operator reads are necessarily the pages for the version they are running.
//
// A documentation site hosted somewhere else drifts from the releases people
// actually have, and the failure mode is somebody following instructions for
// a version they are not running — which reads as the product being wrong
// rather than the page being old.
//
// The embedded directory must exist at compile time even when the site has
// not been built, which is why site/ carries a committed .gitkeep and this
// package tolerates an empty tree: a `go build` in a fresh clone still
// succeeds, and the server says plainly that the manual is missing rather
// than serving a blank page. The same arrangement as internal/web.
package docs

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

//go:embed all:site
var embedded embed.FS

// Prefix is where the manual is mounted. It is a constant rather than a
// caller's choice because the built site contains links generated against
// it: moving the mount without rebuilding would serve a site whose own
// navigation points somewhere else.
const Prefix = "/docs"

// InlineScriptHashes returns a CSP source token — 'sha256-…' — for every
// distinct inline script the built manual contains.
//
// The manual needs them because MkDocs Material puts three inline scripts on
// every page and the application's policy is script-src 'self', which blocks
// all of them. The visible result was not a warning in a console somewhere:
// the blocked script is the one defining __md_get, so Material's bundle threw
// a ReferenceError on load and the manual shipped for months with its search
// box inert and its light/dark toggle unresponsive. Nothing said so, because
// a Content-Security-Policy is enforced by the browser and by nothing in the
// test suite that ran without one.
//
// Hashes rather than 'unsafe-inline'. The manual is same-origin with a
// console that holds a session token, so a policy permitting arbitrary
// inline script there would spend the protection that policy exists to buy.
// Hashes permit exactly the scripts that were compiled into this binary and
// nothing else.
//
// Computed from the embedded files rather than written down, so a rebuilt
// manual cannot fall out of step with the header that admits it — a
// hard-coded list would be one docs change away from silently breaking the
// thing it was added to fix. Eleven distinct scripts across twenty-one pages
// at the time of writing, which is a header of about 600 bytes.
func InlineScriptHashes() []string { return inlineScriptHashes() }

var inlineScriptHashes = sync.OnceValue(func() []string {
	seen := map[string]struct{}{}

	// An error here means the embedded tree could not be walked, which a
	// build that compiled makes impossible; and a malformed page yields no
	// tokens rather than an error. Either way the answer is the hashes found
	// so far: returning none would leave the manual's scripts blocked, which
	// is the state this function exists to leave behind, and it is visible
	// the moment anybody opens a page.
	_ = fs.WalkDir(assets(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(name, ".html") {
			return nil //nolint:nilerr // see above: a page we cannot read contributes nothing
		}
		file, err := assets().Open(name)
		if err != nil {
			return nil
		}
		defer func() { _ = file.Close() }()

		// A tokenizer rather than a regular expression. `script` is a raw
		// text element, so its contents may hold anything that is not the
		// closing tag — including `</` inside a string — and the tokenizer
		// implements that rule where a pattern would guess at it.
		tokenizer := html.NewTokenizer(file)
		inScript := false
		for {
			switch tokenizer.Next() {
			case html.ErrorToken:
				return nil
			case html.StartTagToken:
				name, hasAttr := tokenizer.TagName()
				inScript = string(name) == "script"
				for hasAttr && inScript {
					var key []byte
					key, _, hasAttr = tokenizer.TagAttr()
					// A script with a src is fetched, not inline, and is
					// already covered by 'self'.
					if string(key) == "src" {
						inScript = false
					}
				}
			case html.TextToken:
				if !inScript {
					continue
				}
				// The token is the script body exactly as the browser will
				// hash it: bytes between the tags, no trimming. Trimming
				// would produce a digest that matches nothing.
				sum := sha256.Sum256(tokenizer.Text())
				seen["'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'"] = struct{}{}
				inScript = false
			case html.EndTagToken:
				inScript = false
			}
		}
	})

	hashes := make([]string, 0, len(seen))
	for hash := range seen {
		hashes = append(hashes, hash)
	}
	// Sorted so the header is byte-identical between two servers built from
	// the same tree, which is what makes it comparable in a test and in a
	// diff of two deployments.
	sort.Strings(hashes)
	return hashes
})

// Available reports whether a built manual is present.
func Available() bool {
	_, err := fs.Stat(assets(), "index.html")
	return err == nil
}

func assets() fs.FS {
	sub, err := fs.Sub(embedded, "site")
	if err != nil {
		// The directory is embedded above, so this cannot fail in a build
		// that compiled.
		panic("docs: embedded site is missing: " + err.Error())
	}
	return sub
}

// Handler serves the built manual under Prefix.
//
// Unknown paths return 404 rather than falling back to the index, which is
// the opposite of what the application shell does and for the opposite
// reason: the application has client-side routes that only exist in the
// browser, and a manual has exactly the pages that were built. Handing back
// the home page for a mistyped address would tell a reader the page exists
// and is about something else.
func Handler() http.Handler {
	if !Available() {
		return http.HandlerFunc(notBuilt)
	}

	files := assets()
	fileServer := http.StripPrefix(Prefix, http.FileServer(http.FS(files)))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), Prefix)
		requested = strings.TrimPrefix(requested, "/")
		if requested == "" || requested == "." {
			requested = "index.html"
		}
		// Directory URLs: /docs/ldap/ is a directory holding index.html.
		if !strings.Contains(path.Base(requested), ".") {
			requested = path.Join(requested, "index.html")
		}

		if _, err := fs.Stat(files, requested); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			http.NotFound(w, r)
			return
		}

		// Everything here is regenerated by a build rather than
		// content-hashed, so none of it can be cached indefinitely the way a
		// hashed asset can. no-cache stores it and revalidates, which is what
		// makes a corrected page arrive after an upgrade.
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}

func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(
		"The Portico manual was not included in this build.\n\n" +
			"Build it with:\n" +
			"  ./hack/build-docs.sh\n" +
			"then rebuild the server binary.\n\n" +
			"It is also readable as Markdown in docs/ in the repository.\n",
	))
}

// LocalePath maps one of Portico's locales onto the manual's own language
// prefix, so a link from a Chinese console lands on the Chinese manual.
//
// The two vocabularies are deliberately not the same: Portico speaks BCP 47
// tags because that is what a directory sends, and the manual is built with
// short prefixes because they are what a person sees in a URL. This is the
// one place they meet, and it falls back to the default rather than
// producing a path that does not exist.
func LocalePath(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return Prefix + "/zh/"
	}
	return Prefix + "/"
}
