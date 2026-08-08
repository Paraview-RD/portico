// Package web serves the built single-page application from inside the
// binary, so deploying Portico means copying one file.
//
// The embedded directory must exist at compile time even when the frontend
// has not been built, which is why web/dist carries a committed .gitkeep and
// this package tolerates an empty tree: a backend-only `go build` in a fresh
// clone still succeeds, and the server says plainly that the UI is missing
// rather than serving a blank page.
package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Available reports whether a built frontend is present.
func Available() bool {
	_, err := fs.Stat(assets(), "index.html")
	return err == nil
}

func assets() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		// The directory is embedded above, so this cannot fail in a build
		// that compiled.
		panic("web: embedded dist is missing: " + err.Error())
	}
	return sub
}

// Handler serves the built application.
//
// Unknown paths fall back to index.html so that client-side routes survive a
// page refresh, but paths that look like assets return 404 instead — serving
// HTML for a missing .js file produces a confusing MIME error in the browser
// rather than an obvious "not found".
func Handler() http.Handler {
	if !Available() {
		return http.HandlerFunc(notBuilt)
	}

	files := assets()
	fileServer := http.FileServer(http.FS(files))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requested == "" || requested == "." {
			requested = "index.html"
		}

		if _, err := fs.Stat(files, requested); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if path.Ext(requested) != "" {
				http.NotFound(w, r)
				return
			}
			// A client-side route: hand back the app shell.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
			requested = "index.html"
		}

		setCaching(w, requested)
		fileServer.ServeHTTP(w, r)
	})
}

// setCaching tells the browser which of these files it may keep.
//
// Without this the answer is "whatever it likes". Files embedded with
// go:embed have a zero modification time, so net/http sends no Last-Modified
// and no ETag either — there is nothing for a browser to revalidate against
// and nothing telling it not to guess. What it guesses can be a cached
// index.html pointing at an asset hash the new binary no longer serves,
// which is a deploy that reaches nobody until they clear their cache.
//
// The two answers are opposite because the two kinds of file are opposite:
//
//   - Built assets carry a content hash in the name. The bytes behind one can
//     never change, so it is cacheable forever, and `immutable` additionally
//     says not to revalidate it on a reload.
//   - index.html names those hashes, so it must never be used from cache
//     without asking. `no-cache` does not mean "do not store" — it means
//     "store it, but check with me before using it", which is what makes the
//     next deploy arrive.
func setCaching(w http.ResponseWriter, requested string) {
	if strings.HasPrefix(requested, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(
		"The Portico web UI was not included in this build.\n\n" +
			"Build it with:\n" +
			"  cd web && npm install && npm run build\n" +
			"then rebuild the server binary.\n\n" +
			"The API is unaffected and is available under /api/v1.\n",
	))
}
