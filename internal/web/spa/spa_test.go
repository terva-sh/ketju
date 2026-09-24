package spa

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestTheBuildIsEmbedded is AC1: the binary carries the built shell and the
// hashed assets it references, so `go build` alone serves the client.
func TestTheBuildIsEmbedded(t *testing.T) {
	shell, err := Shell()
	if err != nil {
		t.Fatalf("no shell embedded: %v", err)
	}
	refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(shell, -1)
	var js, css bool
	for _, m := range refs {
		path := m[1]
		js = js || strings.HasSuffix(path, ".js")
		css = css || strings.HasSuffix(path, ".css")
		if !regexp.MustCompile(`-[A-Za-z0-9_-]{8}\.(js|css)$`).MatchString(path) {
			t.Errorf("%s is not a content-hashed name, so it cannot be cached immutably", path)
		}
		rec := httptest.NewRecorder()
		Assets().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		body, _ := io.ReadAll(rec.Body)
		if rec.Code != http.StatusOK || len(body) == 0 {
			t.Errorf("the shell references %s, which serves %d with %d bytes", path, rec.Code, len(body))
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Errorf("%s is served with Cache-Control %q", path, cc)
		}
	}
	if !js || !css {
		t.Fatalf("the shell references js=%v css=%v; want both:\n%s", js, css, shell)
	}
}

// TestAssetsServeOnlyAssets: no listing, nothing outside /assets/, and a
// missing file is a 404 that no cache keeps: an asset a new build references
// may reach this server after a client first asks for it.
func TestAssetsServeOnlyAssets(t *testing.T) {
	for _, path := range []string{"/assets/", "/assets", "/index.html", "/.vite/manifest.json",
		"/assets/missing-00000000.js", "/assets/../index.html", "/assets//x.js"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.Path = path // unclean on purpose, as a raw request can be
		rec := httptest.NewRecorder()
		Assets().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: a 404 with Cache-Control %q, want no-store", path, cc)
		}
	}
}

// TestTheAdminBundleIsSeparate is TKT-01M29CEE52's AC4, read from the build
// itself: the admin screens are a chunk of their own, the entry reaches them
// only by dynamic import, and the shell does not preload them. A static
// import of anything under src/screens/admin from the reader's code would
// fold it into the entry, and this fails.
func TestTheAdminBundleIsSeparate(t *testing.T) {
	b, err := dist.ReadFile("dist/.vite/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]struct {
		File           string   `json:"file"`
		IsEntry        bool     `json:"isEntry"`
		IsDynamicEntry bool     `json:"isDynamicEntry"`
		Imports        []string `json:"imports"`
		DynamicImports []string `json:"dynamicImports"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	const entry, admin = "index.html", "src/screens/admin/AdminRoot.tsx"
	if !manifest[entry].IsEntry {
		t.Fatalf("no entry %s in the manifest", entry)
	}
	a, ok := manifest[admin]
	if !ok || !a.IsDynamicEntry || !slices.Contains(manifest[entry].DynamicImports, admin) {
		t.Fatalf("%s is not a dynamic import of the entry: %+v", admin, a)
	}

	// Everything the entry loads statically, transitively.
	static := map[string]bool{}
	var walk func(string)
	walk = func(k string) {
		if static[k] {
			return
		}
		static[k] = true
		for _, i := range manifest[k].Imports {
			walk(i)
		}
	}
	walk(entry)
	for k := range static {
		if strings.HasPrefix(k, "src/screens/admin/") || manifest[k].File == a.File {
			t.Errorf("the entry loads %s statically", k)
		}
	}

	shell, err := Shell()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(shell, a.File) {
		t.Errorf("the shell references the admin chunk %s", a.File)
	}
	rec := httptest.NewRecorder()
	Assets().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+a.File, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("the admin chunk %s serves %d", a.File, rec.Code)
	}
}
