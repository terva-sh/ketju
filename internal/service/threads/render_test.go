package threads

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/render"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(t.TempDir(), "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return d
}

func addUser(t *testing.T, d *db.DB, handle string) {
	t.Helper()
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := q.CreateUser(t.Context(), dbq.CreateUserParams{Handle: handle, DisplayName: handle,
			Status: "active", Role: "member", Kind: "human", CreatedAt: 1, UpdatedAt: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

var mentionPost = threadlog.Post{Ordinal: 1, Revision: 1, Source: "hello @dana"}

// TestMentionOfALaterHandle: a mention of a handle nobody has is cached as
// text, and once somebody takes the handle the next request renders it again
// as a link rather than reusing the cached text (review 393).
func TestMentionOfALaterHandle(t *testing.T) {
	d := openDB(t)
	s := &Service{DB: d, Render: render.NewCache(1 << 20)}

	html, err := s.html(1, mentionPost, render.Context{}, s.handleMemo(t.Context()))
	if err != nil || strings.Contains(html, "/u/dana") {
		t.Fatalf("before dana exists: %q, %v", html, err)
	}
	if _, ok := s.Render.Get(render.Key{Thread: 1, Ordinal: 1, Revision: 1}); !ok {
		t.Fatal("the text rendering was not cached")
	}

	addUser(t, d, "dana")
	html, err = s.html(1, mentionPost, render.Context{}, s.handleMemo(t.Context()))
	if err != nil || !strings.Contains(html, `href="/u/dana"`) {
		t.Fatalf("after dana signed up: %q, %v", html, err)
	}
}

// TestFailedLookupIsNotCached: a render, or a cache check, during which a
// handle lookup failed is an error and caches nothing, so the next request
// renders again rather than serving a mention an outage drew as unresolved
// (review 392).
func TestFailedLookupIsNotCached(t *testing.T) {
	d := openDB(t)
	addUser(t, d, "dana")
	s := &Service{DB: d, Render: render.NewCache(1 << 20)}
	k := render.Key{Thread: 1, Ordinal: 1, Revision: 1}

	// A clean render is cached with its lookup.
	if _, err := s.html(1, mentionPost, render.Context{}, s.handleMemo(t.Context())); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := s.html(1, mentionPost, render.Context{}, s.handleMemo(t.Context())); err == nil {
		t.Fatal("a cache check whose lookup failed served the cached rendering")
	}

	// A cached text rendering of a handle nobody had, checked during an
	// outage: the failed lookup looks the same as "still nobody", so the
	// failure itself must refuse it.
	d2 := openDB(t)
	s2 := &Service{DB: d2, Render: render.NewCache(1 << 20)}
	nobody := threadlog.Post{Ordinal: 1, Revision: 1, Source: "hello @nobody"}
	if _, err := s2.html(1, nobody, render.Context{}, s2.handleMemo(t.Context())); err != nil {
		t.Fatal(err)
	}
	d2.Close()
	if _, err := s2.html(1, nobody, render.Context{}, s2.handleMemo(t.Context())); err == nil {
		t.Fatal("a cached unresolved mention was served while its check failed")
	}

	fresh := &Service{DB: d, Render: render.NewCache(1 << 20)}
	if _, err := fresh.html(1, mentionPost, render.Context{}, fresh.handleMemo(t.Context())); err == nil {
		t.Fatal("a render whose lookup failed succeeded")
	}
	if _, ok := fresh.Render.Get(k); ok {
		t.Fatal("a render whose lookup failed was cached")
	}
}

// TestHandleTellsMissingFromFailed: no such handle is ok=false with no error;
// a database that cannot answer is an error, and the memo does not remember
// a failure as an answer.
func TestHandleTellsMissingFromFailed(t *testing.T) {
	d := openDB(t)
	s := &Service{DB: d}
	if _, _, ok, err := s.handle(t.Context(), "nobody"); ok || err != nil {
		t.Fatalf("a missing handle: ok=%v err=%v", ok, err)
	}
	m := s.handleMemo(t.Context())
	if l := m.lookup("nobody"); l.Found || m.err != nil {
		t.Fatalf("a missing handle through the memo: %+v, %v", l, m.err)
	}
	d.Close()
	if _, _, ok, err := s.handle(t.Context(), "nobody"); ok || err == nil {
		t.Fatalf("a closed database: ok=%v err=%v", ok, err)
	}
	m = s.handleMemo(t.Context())
	m.lookup("nobody")
	if m.err == nil {
		t.Fatal("the memo did not record the failure")
	}
	if _, remembered := m.seen["nobody"]; remembered {
		t.Fatal("the memo remembered a failure as an answer")
	}
}

// TestMentionOfAHandleThatStopsResolving: a cached link to a handle is
// rendered again once the handle no longer resolves as it did.
func TestMentionOfAHandleThatStopsResolving(t *testing.T) {
	d := openDB(t)
	addUser(t, d, "dana")
	s := &Service{DB: d, Render: render.NewCache(1 << 20)}
	if html, err := s.html(1, mentionPost, render.Context{}, s.handleMemo(t.Context())); err != nil || !strings.Contains(html, "/u/dana") {
		t.Fatalf("while dana exists: %q, %v", html, err)
	}
	if err := d.Write(t.Context(), func(q *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `UPDATE users SET handle = 'dana-renamed' WHERE handle = 'dana'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if html, err := s.html(1, mentionPost, render.Context{}, s.handleMemo(t.Context())); err != nil || strings.Contains(html, "/u/dana") {
		t.Fatalf("after the handle went away: %q, %v", html, err)
	}
}
