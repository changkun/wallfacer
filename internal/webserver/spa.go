// Package webserver serves the wallfacer SPA frontend from embedded assets.
package webserver

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	spaembed "changkun.de/x/wallfacer/internal/webserver/spa"
)

// MountSPA registers handlers for static asset paths from the embedded SPA dist.
func MountSPA(mux *http.ServeMux) bool {
	dist, err := fs.Sub(spaembed.FS, "dist")
	if err != nil {
		slog.Warn("spa: no dist embedded", "err", err)
		return false
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		slog.Info("spa: dist present but no index.html; frontend not built")
		return false
	}
	files := http.FS(dist)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "" || p == "/" {
			serveSPAIndex(w, dist)
			return
		}
		if strings.HasPrefix(p, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.FileServer(files).ServeHTTP(w, r)
			return
		}
		clean := path.Clean(p)
		if _, ferr := fs.Stat(dist, strings.TrimPrefix(clean, "/")); ferr == nil {
			http.FileServer(files).ServeHTTP(w, r)
			return
		}
		serveSPAIndex(w, dist)
	})

	mux.Handle("GET /assets/", handler)
	mux.Handle("GET /fonts/", handler)
	mux.Handle("GET /static/", handler)

	slog.Info("spa: mounted")
	return true
}

// SPAFallback registers a catch-all GET handler that serves index.html for client-side routing.
func SPAFallback(mux *http.ServeMux) {
	dist, err := fs.Sub(spaembed.FS, "dist")
	if err != nil {
		return
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		serveSPAIndex(w, dist)
	})
}

func serveSPAIndex(w http.ResponseWriter, dist fs.FS) {
	b, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		http.Error(w, "spa unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
