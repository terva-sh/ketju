package threads

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestFailedLookupIsNotCached: a render during which a resolver failed is an
// error and leaves nothing in the cache, so the next request renders again
// rather than serving a mention drawn as unresolved by an outage (review 392).
func TestFailedLookupIsNotCached(t *testing.T) {
	s := &Service{Render: render.NewCache(1 << 20)}
	p := threadlog.Post{Ordinal: 1, Revision: 1, Source: "hello @alice"}
	k := render.Key{Thread: 1, Ordinal: 1, Revision: 1}
	unresolved := render.Context{Handles: render.HandleFunc(func(string) (int64, string, bool) { return 0, "", false })}

	outage := errors.New("database is locked")
	if _, err := s.html(1, p, unresolved, func() error { return outage }); !errors.Is(err, outage) {
		t.Fatalf("a render with a failed lookup returned %v", err)
	}
	if _, ok := s.Render.Get(k); ok {
		t.Fatal("a render with a failed lookup was cached")
	}

	resolved := render.Context{Handles: render.HandleFunc(func(string) (int64, string, bool) { return 7, "alice", true })}
	html, err := s.html(1, p, resolved, func() error { return nil })
	if err != nil || !strings.Contains(html, `href="/u/alice"`) {
		t.Fatalf("after recovery: %q, %v", html, err)
	}
	if cached, ok := s.Render.Get(k); !ok || cached != html {
		t.Fatal("a clean render was not cached")
	}
}

// TestHandleTellsMissingFromFailed: no such handle is ok=false with no error;
// a database that cannot answer is an error.
func TestHandleTellsMissingFromFailed(t *testing.T) {
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := &Service{DB: d}
	if _, _, ok, err := s.handle(t.Context(), "nobody"); ok || err != nil {
		t.Fatalf("a missing handle: ok=%v err=%v", ok, err)
	}
	resolver, failed := s.handles(t.Context())
	if _, _, ok := resolver.ResolveHandle("nobody"); ok || failed() != nil {
		t.Fatalf("a missing handle through the resolver: ok=%v failed=%v", ok, failed())
	}
	d.Close()
	if _, _, ok, err := s.handle(t.Context(), "nobody"); ok || err == nil {
		t.Fatalf("a closed database: ok=%v err=%v", ok, err)
	}
	if _, _, ok := resolver.ResolveHandle("nobody"); ok || failed() == nil {
		t.Fatalf("a closed database through the resolver: ok=%v, failure not recorded", ok)
	}
}
