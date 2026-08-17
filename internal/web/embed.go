// Package web serves the React administration UI embedded in production builds.
//
// The embed is build-tag gated: development uses Vite on :5173, while production
// builds the frontend into dist before compiling with the embed tag.
package web

import (
	"io/fs"
	"net/http"
	"strings"
)

func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if !fileExists(sub, path) {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

func fileExists(files fs.FS, name string) bool {
	_, err := fs.Stat(files, name)
	return err == nil
}
