// Package web serves the built single-page application from inside the
// binary, so deploying Keylite means copying one file.
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
		}

		fileServer.ServeHTTP(w, r)
	})
}

func notBuilt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(
		"The Keylite web UI was not included in this build.\n\n" +
			"Build it with:\n" +
			"  cd web && npm install && npm run build\n" +
			"then rebuild the server binary.\n\n" +
			"The API is unaffected and is available under /api/v1.\n",
	))
}
