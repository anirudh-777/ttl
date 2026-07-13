package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:web
var webFS embed.FS

// webHandler serves the embedded static SPA. index.html is served for
// any unknown route so client-side paths like /tasks/abc still work
// after a refresh. The /login path serves a dedicated login page that
// does not require authentication.
func webHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/sw.js" {
			w.Header().Set("Service-Worker-Allowed", "/")
		}
		// Never cache the HTML/JS — we want a server reload to be
		// picked up on the next browser refresh.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		// Dedicated login page (no SPA).
		if r.URL.Path == "/login" || r.URL.Path == "/login.html" {
			r.URL.Path = "/login.html"
			fileServer.ServeHTTP(w, r)
			return
		}
		// Shipped guide (static HTML; no SPA).
		if r.URL.Path == "/guide" || r.URL.Path == "/guide.html" {
			r.URL.Path = "/guide.html"
			fileServer.ServeHTTP(w, r)
			return
		}
		// Paths without a file extension are SPA routes — serve index.
		if !strings.Contains(r.URL.Path[strings.LastIndex(r.URL.Path, "/"):], ".") {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}
