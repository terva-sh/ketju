package spa

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
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
// missing file is a 404.
func TestAssetsServeOnlyAssets(t *testing.T) {
	for _, path := range []string{"/assets/", "/index.html", "/.vite/manifest.json", "/assets/missing-00000000.js"} {
		rec := httptest.NewRecorder()
		Assets().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, rec.Code)
		}
	}
}
