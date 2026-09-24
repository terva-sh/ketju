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
//
// Only a file that exists is marked immutable. A 404 says no-store, so a
// client that asks for a new build's asset before this server has it (a
// rolling deploy, a stale tab) asks again rather than keeping the 404 for a
// year.
func Assets() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // dist is embedded at build time; a missing one fails the build
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if !strings.HasPrefix(name, "assets/") {
			notFound(w, r)
			return
		}
		// fs.Stat refuses a name that is not clean (.., //, a trailing
		// slash), so this also keeps the file server from redirecting one.
		if info, err := fs.Stat(root, name); err != nil || info.IsDir() {
			notFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.NotFound(w, r)
}
