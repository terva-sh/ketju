package search

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/db/views"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// syncBuffer is a log destination tests read while the indexer writes it.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fixture is a migrated database, a thread cache writing real logs, and an
// Indexer over both.
type fixture struct {
	t     testing.TB
	db    *db.DB
	root  string
	cache *threadcache.Cache
	x     *Indexer
	log   *syncBuffer
	next  atomic.Uint64

	// begins counts the indexer's write transactions. The statement trace
	// cannot: the driver's BEGIN never reaches it.
	begins atomic.Int64
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	return newFixtureAt(t, t.TempDir())
}

// newFixtureAt opens a fixture over the data directory root, with a fresh
// database.
func newFixtureAt(t testing.TB, root string) *fixture {
	t.Helper()
	f := &fixture{t: t, root: root, log: &syncBuffer{}}
	f.db = dbtest.Open(t, db.Options{Path: filepath.Join(t.TempDir(), "db", "ketju.db")})
	f.openCache(threadcache.Options{Sync: threadlog.SyncNever})
	f.x = f.indexer()
	return f
}

func (f *fixture) openCache(o threadcache.Options) {
	f.t.Helper()
	c, err := threadcache.New(f.root, o)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { c.Close() })
	f.cache = c
	if f.x != nil {
		f.x.threads = c
	}
}

// indexer returns a new Indexer over the fixture's database and threads, as
// a restarted serve would build.
func (f *fixture) indexer() *Indexer {
	f.t.Helper()
	x, err := New(Options{DB: f.db, Root: f.root, Threads: f.cache,
		Logger: slog.New(slog.NewTextHandler(f.log, nil)),
		Now:    func() time.Time { return time.UnixMilli(1_790_000_000_000) }})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := x.Prepare(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	x.beforeTx = func(context.Context, int) { f.begins.Add(1) }
	return x
}

// thread creates a thread and returns its id and the event its creation
// published.
func (f *fixture) thread(title string) (threadid.ID, events.Event) {
	f.t.Helper()
	id := threadid.ID(uint64(1_700_000_000_000)<<16 | f.next.Add(1))
	th, err := f.cache.Create(id, 7, 1_700_000_000_000)
	if err != nil {
		f.t.Fatal(err)
	}
	f.cache.Release(th)
	return id, f.append(id, threadlog.Append{
		Type: threadlog.TypeThreadCreated, Author: 3, TimeMS: 1_700_000_000_000,
		Body: &threadlog.ThreadCreatedBody{Title: title, Slug: "s", ForumID: 7},
	})
}

// append writes one record and returns the event it publishes.
func (f *fixture) append(id threadid.ID, a threadlog.Append) events.Event {
	f.t.Helper()
	if a.TimeMS == 0 {
		a.TimeMS = 1_700_000_100_000 + int64(f.next.Add(1))*1000
	}
	var res threadlog.Result
	err := f.cache.With(id, func(th *threadcache.Thread) error {
		var err error
		res, err = th.Actor.Append(a)
		return err
	})
	if err != nil {
		f.t.Fatalf("appending %s: %v", a.Type, err)
	}
	return events.Event{Thread: id, Ordinal: res.Ordinal, LogLength: res.LogLength}
}

func (f *fixture) post(id threadid.ID, author uint64, source string) events.Event {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypePost, Author: author,
		Body: &threadlog.PostBody{Source: source}})
}

func (f *fixture) edit(id threadid.ID, ordinal uint32, source string) events.Event {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypeEdit, Author: 5, Ordinal: ordinal,
		Body: &threadlog.EditBody{Source: source}})
}

func (f *fixture) del(id threadid.ID, ordinal uint32, body threadlog.DeleteBody) events.Event {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypeDelete, Author: 5, Ordinal: ordinal, Body: &body})
}

func (f *fixture) restore(id threadid.ID, ordinal uint32) events.Event {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypeRestore, Author: 1, Ordinal: ordinal,
		Body: &threadlog.RestoreBody{}})
}

func (f *fixture) rename(id threadid.ID, title string) events.Event {
	f.t.Helper()
	return f.append(id, threadlog.Append{Type: threadlog.TypeThreadRenamed, Author: 1,
		Body: &threadlog.ThreadRenamedBody{Title: title, Slug: "s"}})
}

// deliver hands the indexer one batch of events.
func (f *fixture) deliver(evs ...events.Event) {
	f.t.Helper()
	if err := f.x.Deliver(f.t.Context(), events.Batch{Events: evs}); err != nil {
		f.t.Fatal(err)
	}
}

var fixtureRedact = threadlog.RedactOptions{TimeMS: 1_790_000_000_000, Operator: 1, Reason: "takedown",
	Journal: "01K6DHZ3Q8N6T2V4X9B7M5C1RE"}

// rewriteLog does to thread id's log what ketju redact does, with every
// thread closed as the command requires, and reopens the cache. It returns
// the new log length.
func (f *fixture) rewriteLog(id threadid.ID, rw func(src *bytes.Reader, size int64, dst *bytes.Buffer) error) int64 {
	f.t.Helper()
	if err := f.cache.Close(); err != nil {
		f.t.Fatal(err)
	}
	path := id.LogPath(f.root)
	old, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var out bytes.Buffer
	if err := rw(bytes.NewReader(old), int64(len(old)), &out); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, out.Bytes(), 0o640); err != nil {
		f.t.Fatal(err)
	}
	// As ketju redact's reindex does: bring the sidecar index current with
	// the rewritten log and write it whole, so the marker's entry change is
	// on disk before anything trusts the index (cmd/ketju/redact.go).
	idx, err := os.ReadFile(id.IndexPath(f.root))
	if err != nil {
		f.t.Fatal(err)
	}
	rec, err := threadlog.Open(bytes.NewReader(out.Bytes()), int64(out.Len()), idx)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(id.IndexPath(f.root), rec.Index.AppendTo(nil), 0o640); err != nil {
		f.t.Fatal(err)
	}
	f.openCache(threadcache.Options{Sync: threadlog.SyncNever})
	return int64(out.Len())
}

func (f *fixture) redact(id threadid.ID, ordinals ...uint32) events.Event {
	f.t.Helper()
	n := f.rewriteLog(id, func(src *bytes.Reader, size int64, dst *bytes.Buffer) error {
		_, err := threadlog.RedactPosts(src, size, dst, ordinals, fixtureRedact)
		return err
	})
	return events.Event{Thread: id, LogLength: n}
}

func (f *fixture) redactTitle(id threadid.ID, replacement string) events.Event {
	f.t.Helper()
	n := f.rewriteLog(id, func(src *bytes.Reader, size int64, dst *bytes.Buffer) error {
		_, err := threadlog.RedactTitle(src, size, dst, threadlog.ThreadRenamedBody{Title: replacement, Slug: "r"}, fixtureRedact)
		return err
	})
	return events.Event{Thread: id, LogLength: n}
}

// hits counts the rows a phrase matches, in every forum, whatever the
// projection says: the index alone.
func (f *fixture) hits(phrase string) int {
	f.t.Helper()
	var n int
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		return tx.QueryRowContext(f.t.Context(), `SELECT count(*) FROM search WHERE search MATCH ?`,
			views.MatchAll([]string{phrase})).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// row returns the search_docs row for (id, ordinal), and false if there is
// none.
func (f *fixture) row(id threadid.ID, ordinal int64) (dbq.GetSearchDocRow, bool) {
	f.t.Helper()
	var r dbq.GetSearchDocRow
	found := false
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		r, err = q.GetSearchDoc(f.t.Context(), dbq.GetSearchDocParams{ThreadID: int64(id), Ordinal: ordinal})
		if err == sql.ErrNoRows {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return r, found
}

// state is the thread's search_state length, or 0 for none.
func (f *fixture) state(id threadid.ID) int64 {
	f.t.Helper()
	var n int64
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		var err error
		n, err = q.GetSearchState(f.t.Context(), int64(id))
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) write(fn func(q *dbq.Queries, tx *sql.Tx) error) {
	f.t.Helper()
	if err := f.db.Write(f.t.Context(), fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) check() {
	f.t.Helper()
	f.write(func(_ *dbq.Queries, tx *sql.Tx) error { return db.CheckSearchIndex(f.t.Context(), tx) })
}

// dump writes the search tables as canonical text: search_docs by (thread,
// ordinal) without its rowid, which depends on the order rows were written
// in, and search_state without its wall clock.
func (f *fixture) dump() string {
	f.t.Helper()
	var b strings.Builder
	err := f.db.Read(f.t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, q := range []string{
			`SELECT 'doc', thread_id, ordinal, revision, author_id, created_at, title, source_plain FROM search_docs ORDER BY thread_id, ordinal`,
			`SELECT 'state', thread_id, log_length, '', '', '', '', '' FROM search_state ORDER BY thread_id`,
			`SELECT 'meta', id, version, '', '', '', '', '' FROM search_meta`,
		} {
			rows, err := tx.QueryContext(f.t.Context(), q)
			if err != nil {
				return err
			}
			for rows.Next() {
				v := make([]any, 8)
				p := make([]any, 8)
				for i := range v {
					p[i] = &v[i]
				}
				if err := rows.Scan(p...); err != nil {
					rows.Close()
					return err
				}
				fmt.Fprintf(&b, "%v %v %v %v %v %v %q %q\n", v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7])
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return b.String()
}

// fromScratch indexes every thread under the fixture's data directory into
// a fresh database, from each thread's first frame, and returns that dump
// after an integrity-check.
func (f *fixture) fromScratch() string {
	f.t.Helper()
	g := &fixture{t: f.t, root: f.root, cache: f.cache, log: &syncBuffer{}}
	g.db = dbtest.Open(f.t, db.Options{Path: filepath.Join(f.t.TempDir(), "db", "ketju.db")})
	g.x = g.indexer()
	if err := g.x.CatchUp(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	g.check()
	return g.dump()
}

// gate blocks every indexing transaction from number n on until it is
// opened, and reports when the first one is waiting.
type gate struct {
	n       int
	waiting chan struct{}
	open    chan struct{}
	once    sync.Once
}

func newGate(n int) *gate {
	return &gate{n: n, waiting: make(chan struct{}), open: make(chan struct{})}
}

func (g *gate) hook(ctx context.Context, n int) {
	if n < g.n {
		return
	}
	g.once.Do(func() { close(g.waiting) })
	select {
	case <-g.open:
	case <-ctx.Done():
	}
}

func (g *gate) wait(t testing.TB) {
	t.Helper()
	select {
	case <-g.waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("the indexer never reached the gate")
	}
}
