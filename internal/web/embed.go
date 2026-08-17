// Package web serves the React SPA that ships inside the single binary.
//
// The embed is build-tag-gated, so the Go toolchain only needs the frontend
// build output on disk for a real production build:
//
//   - embed_on.go  (//go:build embed)  — //go:embed all:dist, the baked-in SPA.
//   - embed_off.go (//go:build !embed) — an empty FS, no dist/ required.
//
// So `go test`/`go vet`, gopls, and a fresh clone all compile with no dist/ at
// all; only `go build -tags embed` (wired into `mise run server:build`, which
// runs web:build first) bakes the SPA in. In dev the SPA is served by Vite
// (:5173), not from here.
package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// Handler serves the embedded SPA, falling back to index.html so client-side
// routing resolves. Without the SPA embedded (no `-tags embed`) the FS is empty
// and every request 404s. Callers must not route /api through it.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if !fileExists(sub, path) {
			r.URL.Path = "/" // unknown path → SPA shell for the client router
		}
		fileServer.ServeHTTP(w, r)
	})
}

func fileExists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, name)
	return err == nil
}
