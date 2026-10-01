package main

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events/search"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// searchWorld is a data directory with a public forum and n seeded threads,
// each with one post saying "hello", that one serve has already projected
// and indexed.
func searchWorld(t *testing.T, n int) (path, data string, ids []threadid.ID) {
	t.Helper()
	path, data = writeConfig(t, "")
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	withDB(t, data, func(q *dbq.Queries, _ *sql.Tx) error {
		_, err := q.CreateForum(t.Context(), dbq.CreateForumParams{Slug: "general", Name: "General",
			Visibility: "public", CreatedAt: 1, UpdatedAt: 1})
		return err
	})
	ids = seedThreads(t, data, n)
	r = start(t, path, serveOpts{})
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(r.log.String(), "search catch-up finished") {
		if time.Now().After(deadline) {
			t.Fatalf("the search catch-up never finished:\n%s", r.log)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
	return path, data, ids
}

// withDB runs fn in a write transaction on the data directory's database.
func withDB(t *testing.T, data string, fn func(q *dbq.Queries, tx *sql.Tx) error) {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Write(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}

// searchFor runs one search over forum 1, the seeded threads' forum.
func searchFor(t *testing.T, data, word string) []views.SearchRow {
	t.Helper()
	var rows []views.SearchRow
	withDB(t, data, func(_ *dbq.Queries, tx *sql.Tx) error {
		var err error
		rows, err = views.Search(t.Context(), tx, views.SearchParams{
			Match: views.MatchAll([]string{word}), Forums: []int64{1}, Limit: 25})
		return err
	})
	return rows
}

// held is a searchHold that blocks the search catch-up until released, and
// says when it has.
type held struct {
	reached chan struct{}
	release chan struct{}
}

func newHeld() *held { return &held{reached: make(chan struct{}, 1), release: make(chan struct{})} }

func (h *held) hold(ctx context.Context) {
	h.reached <- struct{}{}
	select {
	case <-h.release:
	case <-ctx.Done():
	}
}

// TestServePreparesSearchBeforeListening is criterion 12's third test: a
// data directory whose index another version built, served with its
// catch-up held back. When the listener opens, search_meta already records
// this version and the first search finds none of the old rows; the held
// pass then rebuilds them.
func TestServePreparesSearchBeforeListening(t *testing.T) {
	path, data, ids := searchWorld(t, 3)
	if n := len(searchFor(t, data, "hello")); n != 3 {
		t.Fatalf("the first serve indexed %d posts, want 3", n)
	}
	withDB(t, data, func(q *dbq.Queries, tx *sql.Tx) error {
		if err := db.PutSearchDoc(t.Context(), tx, db.SearchDoc{Thread: int64(ids[0]), Ordinal: 1, Revision: 1,
			AuthorID: 1, CreatedAt: 1_700_000_001_000, SourcePlain: "hello from an old extraction stalewords"}); err != nil {
			return err
		}
		return q.SetSearchVersion(t.Context(), search.Version-1)
	})

	h := newHeld()
	r := start(t, path, serveOpts{searchHold: h.hold})
	// ready has fired: this is what the first request would search.
	var version int64
	withDB(t, data, func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		version, err = q.GetSearchVersion(t.Context())
		return err
	})
	if version != search.Version {
		t.Errorf("when the listener opened, search_meta was %d, want %d", version, search.Version)
	}
	if rows := searchFor(t, data, "stalewords"); len(rows) != 0 {
		t.Errorf("the first search found %d rows of the old version", len(rows))
	}
	if rows := searchFor(t, data, "hello"); len(rows) != 0 {
		t.Errorf("the first search found %d rows the held pass had not rebuilt", len(rows))
	}
	steps := r.steps()
	if i, j := slices.Index(steps, "search"), slices.Index(steps, "listener"); i < 0 || j < i {
		t.Errorf("the search step did not come before the listener: %v", steps)
	}
	if !strings.Contains(r.log.String(), "search_indexing=true") && !strings.Contains(r.log.String(), `"search_indexing":true`) {
		t.Errorf("the startup line does not say search is indexing:\n%s", r.log)
	}

	close(h.release)
	deadline := time.Now().Add(20 * time.Second)
	for len(searchFor(t, data, "hello")) != 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the released pass did not rebuild the index:\n%s", r.log)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := r.shutdown(t); err != nil {
		t.Fatal(err)
	}
}

// TestServeAnswersWhileSearchIndexes is criterion 6's first test: with the
// search catch-up held back, serve has opened its listener, and /healthz and
// a thread page answer.
func TestServeAnswersWhileSearchIndexes(t *testing.T) {
	path, data, ids := searchWorld(t, 2)
	withDB(t, data, func(q *dbq.Queries, tx *sql.Tx) error {
		return q.SetSearchVersion(t.Context(), search.Version-1) // so every thread trails
	})
	h := newHeld()
	r := start(t, path, serveOpts{searchHold: h.hold})
	select {
	case <-h.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the search catch-up never started")
	}
	if code, body := get(t, "http://"+r.addr+"/healthz"); code != http.StatusOK {
		t.Errorf("GET /healthz while search indexes = %d: %s", code, body)
	}
	if code, body := get(t, "http://"+r.addr+"/api/v1/threads/"+ids[0].String()); code != http.StatusOK {
		t.Errorf("a thread page while search indexes = %d: %s", code, body)
	}
	if strings.Contains(r.log.String(), "search catch-up finished") {
		t.Error("the catch-up finished although it was held")
	}
	close(h.release)
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown: %v\n%s", err, r.log)
	}
}

// TestShutdownStopsAHeldSearchCatchUp: a catch-up still running at shutdown
// is stopped before the store closes, and shutdown reports no error.
func TestShutdownStopsAHeldSearchCatchUp(t *testing.T) {
	path, _ := writeConfig(t, "")
	h := newHeld()
	r := start(t, path, serveOpts{searchHold: h.hold})
	<-h.reached
	if err := r.shutdown(t); err != nil {
		t.Fatalf("shutdown with a held search catch-up: %v\n%s", err, r.log)
	}
	steps := r.steps()
	if i, j := slices.Index(steps, "stop search"), slices.Index(steps, "close threads"); i < 0 || j < i {
		t.Errorf("the search indexer was not stopped before the threads closed: %v", steps)
	}
}
