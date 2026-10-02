package spa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/terva-sh/ketju/internal/problem"
)

// manifestColor is the light theme's --bg (web/client/src/styles/tokens.css):
// a manifest takes one background and one theme colour, and the shell's two
// theme-color tags carry both themes (TKT-01M29CGBYY).
const manifestColor = "#F6F7FA"

// installIcons are the icons the manifest names, by their hashed /assets
// names, from the dist/icons.json the client build writes
// (web/client/vite.config.ts installIcons).
type installIcons struct {
	Icon192     string `json:"192"`
	Icon512     string `json:"512"`
	Maskable512 string `json:"maskable-512"`
}

// icons reads dist/icons.json once. The file is embedded at build time, so
// one missing or incomplete is a build that went wrong, and it panics, as
// Assets does for a missing dist.
var icons = sync.OnceValue(func() installIcons {
	b, err := dist.ReadFile("dist/icons.json")
	if err != nil {
		panic("spa: the client build wrote no icons.json: " + err.Error())
	}
	var ic installIcons
	if err := json.Unmarshal(b, &ic); err != nil {
		panic("spa: dist/icons.json: " + err.Error())
	}
	if ic.Icon192 == "" || ic.Icon512 == "" || ic.Maskable512 == "" {
		panic("spa: dist/icons.json does not name all three icons: " + string(b))
	}
	return ic
})

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose,omitempty"`
}

type manifest struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ShortName       string         `json:"short_name"`
	StartURL        string         `json:"start_url"`
	Scope           string         `json:"scope"`
	Display         string         `json:"display"`
	BackgroundColor string         `json:"background_color"`
	ThemeColor      string         `json:"theme_color"`
	Icons           []manifestIcon `json:"icons"`
}

// Manifest is the web app manifest for a site named siteName: start_url and
// scope /, standalone, and the 192, 512 and maskable 512 px icons.
//
// id is pinned to / so that the installed app stays the same app if
// start_url ever moves.
func Manifest(siteName string) []byte {
	ic := icons()
	b, err := json.MarshalIndent(manifest{
		ID: "/", Name: siteName, ShortName: siteName, StartURL: "/", Scope: "/", Display: "standalone",
		BackgroundColor: manifestColor, ThemeColor: manifestColor,
		Icons: []manifestIcon{
			{Src: ic.Icon192, Sizes: "192x192", Type: "image/png"},
			{Src: ic.Icon512, Sizes: "512x512", Type: "image/png"},
			{Src: ic.Maskable512, Sizes: "512x512", Type: "image/png", Purpose: "maskable"},
		},
	}, "", "  ")
	if err != nil {
		panic(err) // strings and a slice of structs of strings always marshal
	}
	return append(b, '\n')
}

// ManifestHandler serves GET and HEAD web.ManifestPath with the site name
// siteName returns, read on every request, as /me reads it.
//
// The response is no-cache with an ETag: a browser revalidates each time it
// checks the manifest, so an admin's rename shows on the next check, and
// pays a 304 when nothing changed. The manifest is the same for every
// viewer, so it says nothing about who asked.
//
// It panics now, at wiring, if the build named no icons.
func ManifestHandler(siteName func(context.Context) (string, error), log *slog.Logger) http.Handler {
	if siteName == nil {
		panic("spa.ManifestHandler: the site name is required")
	}
	if log == nil {
		log = slog.Default()
	}
	icons()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			problem.Write(w, r, problem.CodeMethodNotAllowed)
			return
		}
		name, err := siteName(r.Context())
		if err != nil {
			log.ErrorContext(r.Context(), "reading the site name for the web app manifest failed", "error", err)
			problem.Write(w, r, problem.CodeInternal)
			return
		}
		body := Manifest(name)
		sum := sha256.Sum256(body)
		h := w.Header()
		h.Set("Content-Type", "application/manifest+json")
		h.Set("Cache-Control", "no-cache")
		h.Set("ETag", `"`+base64.RawURLEncoding.EncodeToString(sum[:16])+`"`)
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	})
}
