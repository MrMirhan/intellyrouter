// Package dashboard serves the single-page dashboard built into the binary.
package dashboard

import (
	"io/fs"
	"net/http"
	"strings"
)

// CSP allows only same-origin scripts and connections. Charts set inline
// style attributes, so styles also allow 'unsafe-inline'.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; " +
	"font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// Handler serves static assets and falls back to index.html for client-side
// routes. A nil assets FS means the binary was built without the dashboard.
func Handler(assets fs.FS) http.Handler {
	if assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "This binary was built without the dashboard. Build it with: make build", http.StatusNotFound)
		})
	}
	files := http.FileServerFS(assets)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" && name != "index.html" {
			if info, err := fs.Stat(assets, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					// Vite puts a content hash in every asset file name.
					h.Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		h.Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, assets, "index.html")
	})
}
