package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/perm"
	"github.com/terva-sh/ketju/internal/service/users"
	"github.com/terva-sh/ketju/internal/settings"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/web"
	"github.com/terva-sh/ketju/internal/web/api"
	"github.com/terva-sh/ketju/internal/web/spa"
)

// TestManifestCarriesTheSiteName is AC1 of TKT-01M29CGBYY on the server as
// serve wires it: /manifest.webmanifest names the site by the site_name
// setting, read per request, so an admin's rename is in the next response,
// as it is in the shell's apple-mobile-web-app-title. Nothing between the
// setting and either response caches it: the settings are read from the
// database on each request (settings.Key.Get), the manifest is no-cache and
// the shell no-store. The route carries the security headers every
// response does, and only the exact path is the manifest's.
func TestManifestCarriesTheSiteName(t *testing.T) {
	d := dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	shell, err := web.NewShell(mustShell(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	shell.SetPage(api.ShellPage(&users.Service{DB: d, Perm: perm.New(d)}))
	h := web.New(web.Groups{
		Manifest: spa.ManifestHandler(func(ctx context.Context) (string, error) { return settings.SiteName.Get(ctx, d) }, nil),
		SPA:      shell,
	}, web.Options{})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	name := func() string {
		t.Helper()
		rec := get(web.ManifestPath)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d\n%s", web.ManifestPath, rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
			t.Errorf("Content-Type %q", ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("Cache-Control %q", cc)
		}
		for _, k := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
			if rec.Header().Get(k) == "" {
				t.Errorf("the manifest goes out without %s", k)
			}
		}
		var m struct {
			Name      string `json:"name"`
			ShortName string `json:"short_name"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("the manifest is not JSON: %v", err)
		}
		if m.Name != m.ShortName {
			t.Errorf("name %q and short_name %q differ", m.Name, m.ShortName)
		}
		return m.Name
	}
	title := func() string {
		t.Helper()
		body := get("/").Body.String()
		const open = `<meta name="apple-mobile-web-app-title" content="`
		i := strings.Index(body, open)
		if i < 0 {
			t.Fatalf("the shell has no apple-mobile-web-app-title:\n%s", body)
		}
		rest := body[i+len(open):]
		return rest[:strings.IndexByte(rest, '"')]
	}

	if got := name(); got != settings.SiteName.Default {
		t.Errorf("with no site_name set the manifest names %q, want the default %q", got, settings.SiteName.Default)
	}
	if err := settings.Set(t.Context(), d, "site_name", json.RawMessage(`"Club & Co"`), 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := name(); got != "Club & Co" {
		t.Errorf("after the rename the manifest names %q", got)
	}
	if got := title(); got != "Club &amp; Co" {
		t.Errorf("after the rename the shell's apple-mobile-web-app-title is %q", got)
	}

	// Only the exact path: anything below it is an application route.
	if rec := get(web.ManifestPath + "/x"); !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("GET %s/x did not reach the shell: %d", web.ManifestPath, rec.Code)
	}
}

func mustShell(t *testing.T) string {
	t.Helper()
	s, err := spa.Shell()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
