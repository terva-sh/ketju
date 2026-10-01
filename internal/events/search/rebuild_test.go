package search

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

func (f *fixture) rebuild() RebuildReport {
	f.t.Helper()
	rep, err := f.x.Rebuild(f.t.Context(), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

// version is search_meta's version, or -1 for none.
func (f *fixture) version() int64 {
	f.t.Helper()
	v := int64(-1)
	err := f.db.Read(f.t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
		got, err := q.GetSearchVersion(f.t.Context())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		v = got
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

var historyWords = []string{"apple", "birch", "cedar", "delta", "ember", "fjord", "grove"}

// TestRebuildEqualsIncremental: over random histories, Rebuild gives the rows
// the incremental path wrote, and the same hits, from the index as it is,
// from an emptied one, and from one whose FTS5 segments are damaged; and
// integrity-check passes after each.
func TestRebuildEqualsIncremental(t *testing.T) {
	for seed := range uint64(4) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			f := newFixture(t)
			randomHistory(f, seed)
			f.check()
			want := f.dump()
			hits := map[string]int{}
			for _, w := range historyWords {
				hits[w] = f.hits(w)
			}
			for _, c := range []struct {
				name string
				lose func()
			}{
				{"as it is", func() {}},
				{"emptied", func() {
					f.write(func(_ *dbq.Queries, tx *sql.Tx) error { return db.ClearSearch(t.Context(), tx) })
				}},
				{"damaged segments", func() {
					f.write(func(_ *dbq.Queries, tx *sql.Tx) error {
						_, err := tx.ExecContext(t.Context(), `UPDATE search_data SET block = zeroblob(length(block)) WHERE id > 10`)
						return err
					})
					if err := f.db.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
						return db.CheckSearchIndex(t.Context(), tx)
					}); err == nil {
						t.Fatal("the damage did not fail integrity-check")
					}
				}},
			} {
				c.lose()
				rep := f.rebuild()
				if len(rep.Failed) != 0 || rep.Indexed != rep.Threads || !rep.HadVersion && c.name != "emptied" {
					t.Fatalf("%s: report %+v", c.name, rep)
				}
				f.check()
				if got := f.dump(); got != want {
					t.Fatalf("%s: rebuilt and incremental differ:\n--- incremental\n%s\n--- rebuilt\n%s", c.name, want, got)
				}
				for _, w := range historyWords {
					if got := f.hits(w); got != hits[w] {
						t.Errorf("%s: %q finds %d rows, %d incrementally", c.name, w, got, hits[w])
					}
				}
			}
		})
	}
}

// TestRebuildRollsBackAFailedThread is Terva review 1711's case: a thread
// whose log fails after one of its posts was already put rolls back to its
// savepoint, keeps no row and no search_state, and is reported, while the
// threads beside it are indexed and the run commits.
func TestRebuildRollsBackAFailedThread(t *testing.T) {
	f := newFixture(t)
	a, ac := f.thread("Healthy one")
	bad, bc := f.thread("Faulty")
	c, cc := f.thread("Healthy two")
	first := f.post(bad, 5, "first rolled")
	second := f.post(bad, 5, "second unread")
	f.deliver(ac, bc, cc, first, second, f.post(a, 5, "alpha stays"), f.post(c, 5, "gamma stays"))
	if f.hits("rolled") != 1 {
		t.Fatal("the setup did not index the faulty thread")
	}
	// The second post's frame starts where the first post's append ended.
	at := first.LogLength
	path := bad.LogPath(f.root)
	reads, failAt := 0, 0
	f.x.open = func(p string) (logFile, error) {
		l, err := os.OpenFile(p, os.O_RDONLY, 0)
		if err != nil || p != path {
			return l, err
		}
		return faultyLog{File: l, at: at, reads: &reads, failAt: &failAt}, nil
	}

	// A first run counts the reads of that frame. The last is the one that
	// reads the second post's text, which comes after the first post's row
	// was put.
	f.rebuild()
	if reads < 3 {
		t.Fatalf("the frame was read %d times; want the replay, the scan and the text", reads)
	}
	failAt, reads = reads, 0

	rep := f.rebuild()
	if len(rep.Failed) != 1 || rep.Failed[0].Thread != bad || !IsLogFault(rep.Failed[0].Err) ||
		!strings.Contains(rep.Failed[0].Err.Error(), "post 2") {
		t.Fatalf("failed = %+v", rep.Failed)
	}
	if rep.Indexed != 2 || rep.Threads != 3 {
		t.Errorf("report %+v", rep)
	}
	for _, o := range []int64{0, 1, 2} {
		if r, ok := f.row(bad, o); ok {
			t.Errorf("the failed thread kept row %d: %+v", o, r)
		}
	}
	if f.state(bad) != 0 || f.hits("rolled") != 0 {
		t.Error("the failed thread kept its search_state or its first post's row")
	}
	if f.hits("alpha") != 1 || f.hits("gamma") != 1 {
		t.Error("a healthy thread was not indexed beside the failed one")
	}
	f.check()
}

// faultyLog returns the failAt-th read at offset at with its last byte
// flipped, as a damaged sector would read: the frame then fails its
// checksum, which is the log's fault.
type faultyLog struct {
	*os.File
	at            int64
	reads, failAt *int
}

func (l faultyLog) ReadAt(p []byte, off int64) (int, error) {
	n, err := l.File.ReadAt(p, off)
	if off == l.at {
		*l.reads++
		if *l.reads == *l.failAt && n > 0 {
			p[n-1] ^= 0xff
		}
	}
	return n, err
}

// failingLog fails every read from offset at on with an I/O error.
type failingLog struct {
	*os.File
	at int64
}

func (l failingLog) ReadAt(p []byte, off int64) (int, error) {
	if off >= l.at {
		return 0, &os.PathError{Op: "read", Path: l.Name(), Err: errors.New("input/output error")}
	}
	return l.File.ReadAt(p, off)
}

// TestRebuildStopsOnAReadError is Terva review 1766: a read the filesystem
// fails, at any point in a thread, is not the log's fault. The rebuild stops
// and leaves the old index as it was, rather than committing one without the
// thread and reporting its log damaged.
func TestRebuildStopsOnAReadError(t *testing.T) {
	f := newFixture(t)
	a, ac := f.thread("Healthy")
	bad, bc := f.thread("Unreadable")
	first := f.post(bad, 5, "first readable")
	f.deliver(ac, bc, f.post(a, 5, "alpha stays"), first, f.post(bad, 5, "second unreadable"))
	before := f.dump()
	path := bad.LogPath(f.root)
	for _, c := range []struct {
		name string
		at   int64
	}{
		{"the file header", 0},
		{"the replay", threadlog.FileHeaderSize},
		{"a later frame", first.LogLength},
	} {
		t.Run(c.name, func(t *testing.T) {
			f.x.open = func(p string) (logFile, error) {
				l, err := os.OpenFile(p, os.O_RDONLY, 0)
				if err != nil || p != path {
					return l, err
				}
				return failingLog{File: l, at: c.at}, nil
			}
			_, err := f.x.Rebuild(t.Context(), nil)
			if err == nil || IsLogFault(err) || !strings.Contains(err.Error(), "input/output error") {
				t.Fatalf("want the read error, not a log fault: %v", err)
			}
			if got := f.dump(); got != before {
				t.Errorf("a rebuild stopped by a read error changed the index:\n%s\n---\n%s", before, got)
			}
		})
	}
}

// TestRebuildKeepsTheOldIndexOnFailure is Terva review 1731's requirement: a
// run cancelled after the clear, one stopped by a database error inside a
// thread, and one whose version write fails each leave every row, every
// search_state and the old version exactly as they were, the old version
// being one this build does not write.
func TestRebuildKeepsTheOldIndexOnFailure(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(f *fixture) error
	}{
		{"cancelled after the clear", func(f *fixture) error {
			ctx, cancel := context.WithCancel(f.t.Context())
			defer cancel()
			_, err := f.x.Rebuild(ctx, func(p RebuildProgress) {
				if p.Done == 1 {
					cancel()
				}
			})
			if !errors.Is(err, context.Canceled) {
				f.t.Fatalf("want context.Canceled, got %v", err)
			}
			return err
		}},
		{"a database error in a thread", func(f *fixture) error {
			f.write(func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(f.t.Context(), `CREATE TRIGGER disk_full BEFORE INSERT ON search_docs
					WHEN NEW.source_plain LIKE '%beta%' BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
				return err
			})
			_, err := f.x.Rebuild(f.t.Context(), nil)
			if err == nil || IsLogFault(err) || !strings.Contains(err.Error(), "disk full") {
				f.t.Fatalf("want the database's error, got %v", err)
			}
			return err
		}},
		{"the version write", func(f *fixture) error {
			f.write(func(_ *dbq.Queries, tx *sql.Tx) error {
				_, err := tx.ExecContext(f.t.Context(), `CREATE TRIGGER disk_full BEFORE INSERT ON search_meta
					BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
				return err
			})
			_, err := f.x.Rebuild(f.t.Context(), nil)
			if err == nil || !strings.Contains(err.Error(), "disk full") {
				f.t.Fatalf("want the database's error, got %v", err)
			}
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			for _, w := range []string{"alpha", "beta", "gamma"} {
				id, ev := f.thread(w + " thread")
				f.deliver(ev, f.post(id, 5, w+" words"))
			}
			f.write(func(q *dbq.Queries, _ *sql.Tx) error { return q.SetSearchVersion(t.Context(), Version-1) })
			before := f.dump()
			if c.run(f) == nil {
				t.Fatal("the run did not fail")
			}
			if got := f.dump(); got != before {
				t.Fatalf("a failed rebuild changed the index:\n--- before\n%s\n--- after\n%s", before, got)
			}
			if v := f.version(); v != Version-1 {
				t.Errorf("version %d, want the old %d", v, Version-1)
			}
			f.check()
		})
	}
}

// TestRebuildReportsATornTail: a log whose last frame was cut short is
// indexed to its last whole frame and reported torn, and nothing is written
// to it or its .idx.
func TestRebuildReportsATornTail(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Thread")
	f.deliver(created, f.post(id, 5, "whole post"))
	cut := f.post(id, 5, "severed post")
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	f.x.threads = nil
	path := id.LogPath(f.root)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = b[:cut.LogLength-5]
	if err := os.WriteFile(path, b, 0o640); err != nil {
		t.Fatal(err)
	}
	idx, err := os.ReadFile(id.IndexPath(f.root))
	if err != nil {
		t.Fatal(err)
	}

	rep := f.rebuild()
	if len(rep.Torn) != 1 || rep.Torn[0] != id || rep.Indexed != 1 || rep.Posts != 1 {
		t.Fatalf("report %+v", rep)
	}
	if f.hits("whole") != 1 || f.hits("severed") != 0 {
		t.Error("the whole post was not indexed, or the torn one was")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, b) {
		t.Errorf("the log was changed (%v)", err)
	}
	if got, err := os.ReadFile(id.IndexPath(f.root)); err != nil || !bytes.Equal(got, idx) {
		t.Errorf("the .idx was changed (%v)", err)
	}
	f.check()
}

// TestRebuildReadsTheLogNotTheIdx: a sidecar index that disagrees with its
// log, while recording the log's length so an ordinary open would trust it,
// does not reach the rebuilt index, which comes from the log alone.
func TestRebuildReadsTheLogNotTheIdx(t *testing.T) {
	f := newFixture(t)
	id, created := f.thread("Thread")
	f.deliver(created, f.post(id, 5, "hidden words"), f.del(id, 1, threadlog.DeleteBody{ByModerator: true}))
	if f.hits("hidden") != 0 {
		t.Fatal("the hidden post was indexed")
	}
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}
	f.x.threads = nil
	// Clear post 1's flags (docs/03 "Index entry": 32-byte header, 40-byte
	// entries, flags at 34), as an .idx written before the hide holds them.
	idx, err := os.ReadFile(id.IndexPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	idx[32+34], idx[32+35] = 0, 0
	if err := os.WriteFile(id.IndexPath(f.root), idx, 0o640); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(id.LogPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := threadlog.Open(bytes.NewReader(b), int64(len(b)), idx)
	if err != nil {
		t.Fatal(err)
	}
	if e, err := rec.Index.Entry(1); err != nil || rec.Outcome != threadlog.Trusted || !e.Flags.Visible() {
		t.Fatalf("the doctored .idx is not trusted as visible: %v, %v, %#x", err, rec.Outcome, e.Flags)
	}

	f.rebuild()
	if f.hits("hidden") != 0 {
		t.Error("the rebuild trusted a stale .idx and indexed a hidden post")
	}
	f.check()
}
