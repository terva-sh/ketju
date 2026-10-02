package spa

import (
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestManifest is AC1 of TKT-01M29CGBYY, from the build itself: the
// manifest for a site named Club is exactly the settled fields, and every
// icon it names is a PNG of the size it claims, served from /assets.
func TestManifest(t *testing.T) {
	ic := icons()
	want := `{
  "id": "/",
  "name": "Club",
  "short_name": "Club",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "background_color": "#F6F7FA",
  "theme_color": "#F6F7FA",
  "icons": [
    {
      "src": "` + ic.Icon192 + `",
      "sizes": "192x192",
      "type": "image/png"
    },
    {
      "src": "` + ic.Icon512 + `",
      "sizes": "512x512",
      "type": "image/png"
    },
    {
      "src": "` + ic.Maskable512 + `",
      "sizes": "512x512",
      "type": "image/png",
      "purpose": "maskable"
    }
  ]
}
`
	if got := string(Manifest("Club")); got != want {
		t.Errorf("the manifest is\n%s\nwant\n%s", got, want)
	}

	for src, side := range map[string]int{ic.Icon192: 192, ic.Icon512: 512, ic.Maskable512: 512} {
		if !strings.HasPrefix(src, "/assets/icon-") || !strings.HasSuffix(src, ".png") {
			t.Errorf("the manifest names %q, not a hashed icon under /assets", src)
			continue
		}
		rec := httptest.NewRecorder()
		Assets().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, src, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s serves %d", src, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("%s is served as %q", src, ct)
		}
		cfg, err := png.DecodeConfig(rec.Body)
		if err != nil {
			t.Errorf("%s is not a PNG: %v", src, err)
			continue
		}
		if cfg.Width != side || cfg.Height != side {
			t.Errorf("%s is %dx%d, and the manifest says %d", src, cfg.Width, cfg.Height, side)
		}
	}
}

// TestManifestEscapesTheSiteName: an admin types the site name, and the
// manifest is JSON whatever they typed.
func TestManifestEscapesTheSiteName(t *testing.T) {
	name := `The "Club" \ </script>`
	var m struct {
		Name      string `json:"name"`
		ShortName string `json:"short_name"`
	}
	if err := json.Unmarshal(Manifest(name), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != name || m.ShortName != name {
		t.Errorf("name %q, short_name %q, want %q", m.Name, m.ShortName, name)
	}
}

// TestManifestHandler: the content type browsers expect, no-cache with a
// validator so a rename shows on the next check and an unchanged manifest
// costs a 304, a body only for GET, a refusal for anything but GET and HEAD,
// and a 500 rather than a guessed name when the setting cannot be read.
func TestManifestHandler(t *testing.T) {
	name := "Club"
	h := ManifestHandler(func(context.Context) (string, error) { return name, nil }, nil)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != string(Manifest("Club")) {
		t.Fatalf("GET: %d\n%s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control %q", cc)
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag, so every check downloads the manifest again")
	}

	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("an unchanged manifest revalidates with %d, want 304", rec.Code)
	}

	name = "Renamed"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name": "Renamed"`) {
		t.Errorf("after a rename the old ETag got %d:\n%s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d with %d bytes", rec.Code, rec.Body.Len())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d, want 405", rec.Code)
	}

	failing := ManifestHandler(func(context.Context) (string, error) { return "", errors.New("database closed") }, nil)
	rec = httptest.NewRecorder()
	failing.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "database closed") {
		t.Errorf("a failed read: %d\n%s", rec.Code, rec.Body)
	}
}

// TestTheDistRegistersNoServiceWorker is AC4's build half: v1 has no
// service worker (docs/06 "Installable", docs/10), so no script the build
// ships registers one.
func TestTheDistRegistersNoServiceWorker(t *testing.T) {
	entries, err := dist.ReadDir("dist/assets")
	if err != nil {
		t.Fatal(err)
	}
	scripts := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		scripts++
		b, err := dist.ReadFile("dist/assets/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "serviceWorker") {
			t.Errorf("%s mentions serviceWorker", e.Name())
		}
	}
	if scripts == 0 {
		t.Fatal("the build has no scripts, so this checked nothing")
	}
}
