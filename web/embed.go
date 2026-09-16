// Package web serves the compiled Vue 3 single page application.
//
// The Vite build output (web/dist) is embedded into the Go binary, so a
// single container image is enough: no nginx, no separate static server.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the SPA with history fallback.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "web assets missing", http.StatusInternalServerError)
		})
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if upath == "" || upath == "." {
			upath = "index.html"
		}

		if f, err := sub.Open(upath); err == nil {
			_ = f.Close()
			files.ServeHTTP(w, r)
			return
		}

		// Unknown path without an extension -> SPA route (vue-router history mode).
		if !strings.Contains(path.Base(upath), ".") {
			serveIndex(w, r, sub)
			return
		}
		http.NotFound(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "index.html missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}
