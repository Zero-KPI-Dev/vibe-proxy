package runtime

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:web/dist
var spaFiles embed.FS

const spaPrefix = "web/dist"

func spaHandler() http.Handler {
	sub, err := fs.Sub(spaFiles, spaPrefix)
	if err != nil {
		panic("spa embed not found: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}

// spaFallback wraps an http.Handler to serve index.html for SPA routes.
func spaFallback(next http.Handler, index string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the requested file
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = index
		}
		f, err := spaFiles.Open(path.Join(spaPrefix, p))
		if err == nil {
			f.Close()
			next.ServeHTTP(w, r)
			return
		}
		// Fallback to index.html for SPA routing
		r.URL.Path = "/"
		next.ServeHTTP(w, r)
	})
}
