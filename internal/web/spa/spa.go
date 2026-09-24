// Package spa embeds the built Preact client (docs/07 "Embedding").
//
// dist/ is Vite's output from web/client and is committed, so `go build`
// alone produces a binary that serves the client. `go generate` rebuilds it,
// and CI does exactly that and fails on any difference, so the committed
// build can never drift from the source it came from.
package spa

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:generate sh -c "cd ../../../web/client && npm ci --no-audit --no-fund && npm run build"

//go:embed all:dist
var dist embed.FS

// Shell returns the built index.html, which is a Go template: web.NewShell
// parses it and renders it per response with the CSP nonce, the page's title
// and description, and the bootstrap /me block.
func Shell() (string, error) {
	b, err := dist.ReadFile("dist/index.html")
	return string(b), err
}

// Assets serves /assets/*, the hashed files Vite emits.
//
// A file's name carries a hash of its contents, so a response is valid
// forever and says so; a new build is new names, referenced from a shell
// that is never cached. Directory listings are refused rather than served:
// the listing is not a hashed file and nothing references it.
func Assets() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time; a missing one fails the build
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/assets/") || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}
