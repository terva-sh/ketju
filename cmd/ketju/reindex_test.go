package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/lock"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// openDB opens and migrates the data directory's database.
func openDB(t *testing.T, data string) *db.DB {
	t.Helper()
	d, err := db.Open(t.Context(), db.Options{Path: filepath.Join(data, "db", "ketju.db")})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(t.Context()); err != nil {
		d.Close()
		t.Fatal(err)
	}
	return d
}

func execDB(t *testing.T, d *db.DB, query string, args ...any) {
	t.Helper()
	err := d.Write(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// dumpAt is the projection tables of the data directory's database, and
// what is left of its authoritative rows that populate wrote.
func dumpAt(t *testing.T, data string) (projections, authoritative string) {
	t.Helper()
	d := openDB(t, data)
	defer d.Close()
	var b strings.Builder
	err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		for _, q := range []string{
			`SELECT id || ' ' || handle FROM users ORDER BY id`,
			`SELECT user_id || ' ' || thread_id || ' ' || last_read_ordinal FROM read_positions ORDER BY 1`,
		} {
			rows, err := tx.QueryContext(t.Context(), q)
			if err != nil {
				return err
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					rows.Close()
					return err
				}
				b.WriteString(s + "\n")
			}
			if err := rows.Close(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dbtest.DumpProjections(t, d), b.String()
}

// everyType is the record types populate writes. TestPopulateWritesEveryType
// holds it to the catalogue, so a record type added later is reindexed here
// before anyone relies on it.
var everyType = []threadlog.RecordType{
	threadlog.TypePost, threadlog.TypeEdit, threadlog.TypeDelete, threadlog.TypeRestore,
	threadlog.TypeThreadRenamed, threadlog.TypeThreadMoved, threadlog.TypeThreadLocked,
	threadlog.TypeThreadUnlocked, threadlog.TypeThreadPinned, threadlog.TypeThreadUnpinned,
	threadlog.TypeThreadTagged, threadlog.TypeThreadArchived,
}

func TestPopulateWritesEveryType(t *testing.T) {
	covered := map[threadlog.RecordType]bool{threadlog.TypeThreadCreated: true}
	for _, typ := range everyType {
		covered[typ] = true
	}
	for n := 1; n <= 255; n++ {
		if typ := threadlog.RecordType(n); typ.Known() && !covered[typ] {
			t.Errorf("%s is in the catalogue and populate never writes it", typ)
		}
	}
}

// randomAppend is one record of a random type for a thread with posts
// ordinals 1..posts, and the post count after it.
func randomAppend(rng *rand.Rand, posts uint32) (threadlog.Append, uint32) {
	typ := everyType[rng.IntN(len(everyType))]
	if posts == 0 {
		typ = threadlog.TypePost
	}
	target := 1 + rng.Uint32N(max(posts, 1))
	author := uint64(1 + rng.IntN(4))
	a := threadlog.Append{Type: typ, Author: author}
	switch typ {
	case threadlog.TypePost:
		var reply uint32
		if posts > 0 && rng.IntN(2) == 0 {
			reply = target
		}
		a.Body = &threadlog.PostBody{Source: "a post", ReplyTo: reply, Mentions: []uint64{uint64(rng.IntN(5))}}
		return a, posts + 1
	case threadlog.TypeEdit:
		body := &threadlog.EditBody{Source: "edited", ReplyTo: rng.Uint32N(target)}
		if rng.IntN(2) == 0 {
			m := []uint64{uint64(rng.IntN(5))}
			body.Mentions = &m
		}
		a.Ordinal, a.Body = target, body
	case threadlog.TypeDelete:
		a.Ordinal, a.Body = target, &threadlog.DeleteBody{}
	case threadlog.TypeRestore:
		a.Ordinal, a.Body = target, &threadlog.RestoreBody{}
	case threadlog.TypeThreadRenamed:
		n := rng.IntN(100)
		a.Body = &threadlog.ThreadRenamedBody{Title: fmt.Sprintf("Renamed %d", n), Slug: fmt.Sprintf("renamed-%d", n)}
	case threadlog.TypeThreadMoved:
		a.Body = &threadlog.ThreadMovedBody{ForumID: uint64(1 + rng.IntN(3))}
	case threadlog.TypeThreadLocked:
		a.Body = &threadlog.ThreadLockedBody{}
	case threadlog.TypeThreadUnlocked:
		a.Body = &threadlog.ThreadUnlockedBody{}
	case threadlog.TypeThreadPinned:
		a.Body = &threadlog.ThreadPinnedBody{Scope: []string{"forum", "global"}[rng.IntN(2)],
			Until: int64(rng.IntN(2)) * 1_800_000_000_000}
	case threadlog.TypeThreadUnpinned:
		a.Body = &threadlog.ThreadUnpinnedBody{Scope: []string{"forum", "global"}[rng.IntN(2)]}
	case threadlog.TypeThreadTagged:
		a.Body = &threadlog.ThreadTaggedBody{Tags: []string{"a", "b", "c"}[:rng.IntN(4)]}
	case threadlog.TypeThreadArchived:
		a.Body = &threadlog.ThreadArchivedBody{}
	}
	return a, posts
}

// writeThreads appends n new threads of random records to the data
// directory, published through a real bus to a real projection writer, the
// way serve projects them. It also runs extra records against the threads
// given, and returns every thread it touched.
func writeThreads(t *testing.T, data string, seed uint64, n int, more []threadid.ID) []threadid.ID {
	t.Helper()
	d := openDB(t, data)
	defer d.Close()
	cache, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	w, err := projection.New(projection.Options{DB: d, Root: data, Sweep: func(context.Context) error {
		t.Error("the bus dropped events; the queue is too small for the test")
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New(events.Options{Queue: 1 << 16, Logger: slog.New(slog.DiscardHandler)})
	if err := bus.Subscribe(w, 0); err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	rng := rand.New(rand.NewPCG(seed, 0xC5))
	clock := int64(1_700_000_000_000) + int64(seed)*1_000_000
	appendOne := func(id threadid.ID, a threadlog.Append) {
		clock += 1000
		a.TimeMS = clock
		var res threadlog.Result
		err := cache.With(id, func(th *threadcache.Thread) error {
			var err error
			res, err = th.Actor.Append(a)
			return err
		})
		if err != nil {
			t.Fatalf("appending %s: %v", a.Type, err)
		}
		bus.Publish(events.Event{Thread: id, LogLength: res.LogLength})
	}
	posts := map[threadid.ID]uint32{}
	var ids []threadid.ID
	for i := range n {
		id := threadid.ID(uint64(clock)<<16 | uint64(i+1))
		th, err := cache.Create(id, 1, clock)
		if err != nil {
			t.Fatal(err)
		}
		cache.Release(th)
		appendOne(id, threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 3,
			Body: &threadlog.ThreadCreatedBody{Title: fmt.Sprintf("T%d", i), Slug: fmt.Sprintf("t%d", i),
				ForumID: 1, Tags: []string{"meta"}}})
		ids = append(ids, id)
	}
	for _, id := range more {
		posts[id] = 1 // every earlier thread has a first post; see below
	}
	for _, id := range append(ids, more...) {
		// A first post, so every record after it has a post to name.
		if posts[id] == 0 {
			appendOne(id, threadlog.Append{Type: threadlog.TypePost, Author: 1, Body: &threadlog.PostBody{Source: "first"}})
			posts[id] = 1
		}
		for range 5 + rng.IntN(25) {
			a, p := randomAppend(rng, posts[id])
			appendOne(id, a)
			posts[id] = p
		}
	}
	if err := bus.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if l := w.Lag(); l.Threads != 0 {
		t.Fatalf("the projection is behind after the bus drained: %+v", l)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	return append(ids, more...)
}

// populate is a data directory with n threads projected incrementally, a
// member and a read position, and returns their ids.
func populate(t *testing.T, n int) (string, []threadid.ID) {
	t.Helper()
	data := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(data, threadid.ThreadsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	ids := writeThreads(t, data, 1, n, nil)
	d := openDB(t, data)
	execDB(t, d, `INSERT INTO users (id, handle, display_name, created_at, updated_at) VALUES (3, 'alice', 'Alice', 1, 1)`)
	execDB(t, d, `INSERT INTO read_positions (user_id, thread_id, last_read_ordinal, last_read_at) VALUES (3, ?, 1, 1)`, int64(ids[0]))
	d.Close()
	return data, ids
}

func reindexRun(t *testing.T, o reindexOpts, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := reindexMain(t.Context(), &out, &errOut, args, o)
	return out.String(), errOut.String(), err
}

// TestReindexEqualsIncremental is AC1, and TKT-01M29BYYJ8's AC3: a populated
// instance whose projection was written incrementally, through a bus, loses
// its projection three ways, and reindex brings back exactly the rows the
// incremental path wrote. The authoritative rows are not touched, where there
// are any left to touch.
func TestReindexEqualsIncremental(t *testing.T) {
	data, _ := populate(t, 12)
	incremental, members := dumpAt(t, data)
	if !strings.Contains(incremental, "threads: 12 rows") || !strings.Contains(members, "3 alice") {
		t.Fatalf("the setup did not populate:\n%s\n%s", incremental, members)
	}

	for _, c := range []struct {
		name string
		lose func(t *testing.T)
		// members is what is left of the authoritative rows afterwards.
		members string
	}{
		{"emptied", func(t *testing.T) {
			d := openDB(t, data)
			defer d.Close()
			if err := d.Write(t.Context(), func(q *dbq.Queries, _ *sql.Tx) error {
				return db.ClearProjections(t.Context(), q)
			}); err != nil {
				t.Fatal(err)
			}
		}, members},
		{"stale", func(t *testing.T) {
			d := openDB(t, data)
			defer d.Close()
			execDB(t, d, `UPDATE threads SET title = 'stale', archived = 1 - archived, pinned_scope = NULL, pinned_until = NULL`)
			execDB(t, d, `UPDATE post_meta SET deleted = 1 - deleted, current_revision = 9`)
			execDB(t, d, `DELETE FROM mentions`)
			execDB(t, d, `INSERT INTO thread_tags (thread_id, tag) SELECT id, 'stale' FROM threads`)
			execDB(t, d, `UPDATE projection_state SET log_length = 64`)
			execDB(t, d, `UPDATE forum_thread_counts SET threads = 999`)
		}, members},
		{"deleted", func(t *testing.T) {
			for _, suffix := range []string{"", "-wal", "-shm"} {
				if err := os.Remove(filepath.Join(data, "db", "ketju.db"+suffix)); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			}
		}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.lose(t)
			if lost, _ := dumpAt(t, data); lost == incremental {
				t.Fatal("the projection was not lost")
			}
			out, errOut, err := reindexRun(t, reindexOpts{}, "--root", data, "--json")
			if err != nil {
				t.Fatalf("reindex: %v\n%s", err, errOut)
			}
			var sum reindexSummary
			if err := json.Unmarshal([]byte(out), &sum); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, out)
			}
			if sum.Threads != 12 || sum.Projected != 12 || len(sum.Failed)+len(sum.Torn)+len(sum.Unfinished) != 0 {
				t.Fatalf("summary = %+v", sum)
			}
			got, left := dumpAt(t, data)
			if got != incremental {
				t.Fatalf("reindexed rows differ from the incremental ones:\n%s\n---\n%s", incremental, got)
			}
			if left != c.members {
				t.Fatalf("authoritative rows = %q, want %q", left, c.members)
			}
			if warned := strings.Contains(errOut, "no members"); warned != (c.members == "") {
				t.Errorf("warned about members = %v, with members %q:\n%s", warned, c.members, errOut)
			}
		})
	}
}

// fakeClock is a clock that moves on a second each time it is read.
func fakeClock() func() time.Time {
	now := time.Unix(1_800_000_000, 0)
	return func() time.Time { now = now.Add(time.Second); return now }
}

// TestReindexReportsProgress is AC2: a run long enough to need it reports
// where it is, on stderr, before its summary on stdout.
func TestReindexReportsProgress(t *testing.T) {
	data, _ := populate(t, 30)
	out, errOut, err := reindexRun(t, reindexOpts{every: 2 * time.Second, now: fakeClock()}, "--root", data)
	if err != nil {
		t.Fatalf("reindex: %v\n%s", err, errOut)
	}
	lines := strings.Split(strings.TrimSpace(errOut), "\n")
	if !strings.HasPrefix(lines[0], "reindex: 30 thread(s), ") {
		t.Errorf("no opening line naming the work:\n%s", errOut)
	}
	progress := regexp.MustCompile(`^reindex: (\d+)/30 threads, [\d.]+ [KMG]?i?B of [\d.]+ [KMG]?i?B`)
	var seen int
	for _, l := range lines[1:] {
		if progress.MatchString(l) {
			seen++
		}
	}
	// A second a read and a line every two: one line every other thread.
	if seen < 10 || seen > 16 {
		t.Errorf("%d progress lines for 30 threads read a second apart, every 2 s:\n%s", seen, errOut)
	}
	if !strings.Contains(out, "reindexed 30 of 30 thread(s)") || strings.Contains(out, "/30 threads") {
		t.Errorf("stdout has no summary, or has progress in it:\n%s", out)
	}
}

// TestReindexRefusesWhileLocked is AC3: while serve holds the data
// directory, reindex refuses, names the holder and says what to do, and
// changes nothing. Once the lock is free it runs.
func TestReindexRefusesWhileLocked(t *testing.T) {
	data, _ := populate(t, 3)
	d := openDB(t, data)
	execDB(t, d, `UPDATE threads SET title = 'stale'`)
	d.Close()
	before, _ := dumpAt(t, data)

	lk, err := lock.Acquire(data, "serve")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = reindexRun(t, reindexOpts{}, "--root", data)
	if err == nil {
		lk.Release()
		t.Fatal("reindex ran while serve held the data directory")
	}
	for _, want := range []string{"ketju serve (pid", "stop ketju serve, then run reindex"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if code := exitCodeFor(err); code != exitInternal {
		t.Errorf("exit %d, want %d", code, exitInternal)
	}
	if after, _ := dumpAt(t, data); after != before {
		t.Errorf("a refused reindex changed the projection:\n%s\n---\n%s", before, after)
	}

	if err := lk.Release(); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := reindexRun(t, reindexOpts{}, "--root", data); err != nil {
		t.Fatalf("reindex after the lock was released: %v\n%s", err, errOut)
	}
	if after, _ := dumpAt(t, data); strings.Contains(after, "stale") {
		t.Error("reindex did not rebuild once it could")
	}
}

// TestReindexHoldsTheLock: serve cannot start against a directory reindex
// is working on. Stood in for by taking the lock from inside a progress
// line, which is while the tables are half rebuilt.
func TestReindexHoldsTheLock(t *testing.T) {
	data, _ := populate(t, 2)
	var tried bool
	var errOut bytes.Buffer
	o := reindexOpts{every: time.Nanosecond, now: func() time.Time {
		if !tried {
			tried = true
			if lk, err := lock.Acquire(data, "serve"); err == nil {
				lk.Release()
				t.Error("serve could take the lock while reindex ran")
			}
		}
		return time.Now()
	}}
	if err := reindexMain(t.Context(), &bytes.Buffer{}, &errOut, []string{"--root", data}, o); err != nil {
		t.Fatalf("%v\n%s", err, errOut.String())
	}
	if !tried {
		t.Fatal("the clock was never read")
	}
}

// TestReindexExitCodes: the refusals before anything is touched, and a
// damaged log, each with fsck's exit code for the same thing.
func TestReindexExitCodes(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		data, _ := populate(t, 1)
		for _, args := range [][]string{
			{"--root", data, "--search"},
			{"--root", data, "--projections=false"},
			{"--root", data, "extra"},
			{"--root", data, "--no-such-flag"},
		} {
			if _, _, err := reindexRun(t, reindexOpts{}, args...); exitCodeFor(err) != exitUsage {
				t.Errorf("%q: exit %d (%v), want %d", args, exitCodeFor(err), err, exitUsage)
			}
		}
	})
	t.Run("not a data directory", func(t *testing.T) {
		dir := t.TempDir()
		_, _, err := reindexRun(t, reindexOpts{}, "--root", dir)
		if exitCodeFor(err) != exitUsage || !strings.Contains(err.Error(), "is --root the data directory") {
			t.Errorf("exit %d: %v", exitCodeFor(err), err)
		}
		if _, err := os.Stat(filepath.Join(dir, "db")); !os.IsNotExist(err) {
			t.Error("a refused reindex created a database directory")
		}
	})
	t.Run("damaged", func(t *testing.T) {
		data, ids := populate(t, 2)
		path := ids[0].LogPath(data)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b[threadlog.FileHeaderSize+40] ^= 0xff // inside the first frame
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := reindexRun(t, reindexOpts{}, "--root", data)
		if exitCodeFor(err) != exitCorrupt {
			t.Fatalf("exit %d (%v), want %d", exitCodeFor(err), err, exitCorrupt)
		}
		if !strings.Contains(out, "FAILED   "+ids[0].String()) || !strings.Contains(out, "reindexed 1 of 2 thread(s)") {
			t.Errorf("the report does not name the damaged thread:\n%s", out)
		}
		if got, _ := dumpAt(t, data); !strings.Contains(got, "threads: 1 rows") {
			t.Errorf("the sound thread was not projected beside the damaged one:\n%s", got)
		}
	})
	t.Run("database", func(t *testing.T) {
		// A write the database refuses is not the log's fault, so it is not
		// exit 1 and fsck will not explain it (review 1137).
		data, _ := populate(t, 2)
		d := openDB(t, data)
		execDB(t, d, `CREATE TRIGGER disk_full BEFORE INSERT ON post_meta BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
		d.Close()
		_, _, err := reindexRun(t, reindexOpts{}, "--root", data)
		if exitCodeFor(err) != exitInternal || !strings.Contains(err.Error(), "disk full") ||
			!strings.Contains(err.Error(), "run reindex again") {
			t.Fatalf("exit %d (%v), want %d naming the database's error", exitCodeFor(err), err, exitInternal)
		}
	})
}

// readmeRecovery is the shell block of the README's recovery section.
func readmeRecovery(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	const heading = "## Recovering a lost or damaged database\n"
	_, section, ok := strings.Cut(string(b), heading)
	if !ok {
		t.Fatalf("the README has no %q section", strings.TrimSpace(heading))
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	_, block, ok := strings.Cut(section, "```sh\n")
	if !ok {
		t.Fatal("the recovery section has no sh block")
	}
	block, _, ok = strings.Cut(block, "```")
	if !ok {
		t.Fatal("the recovery section's sh block is not closed")
	}
	for _, want := range []string{"ketju reindex", "ketju fsck", "$KETJU_BACKUP"} {
		if !strings.Contains(block, want) {
			t.Errorf("the recovery procedure does not run %q:\n%s", want, block)
		}
	}
	return block
}

// TestREADMERecoveryProcedure is AC4. The README's recovery commands run as
// written, with `ketju` being this test binary run as the real main, against
// a data directory whose database was lost after its last backup. Afterwards
// the members are the backup's, the content is all of the logs', posts made
// after the backup included, and serve's catch-up finds nothing left to do.
func TestREADMERecoveryProcedure(t *testing.T) {
	script := readmeRecovery(t)
	data, ids := populate(t, 5)

	// The backup. VACUUM INTO cannot run inside the transaction every access
	// here goes through, and this package may not open a driver of its own,
	// so it is a copy of the file of a database closed cleanly, which is as
	// whole: closing checkpoints the WAL into it, and the test checks that.
	_, backedUp := dumpAt(t, data)
	dbFile := filepath.Join(data, "db", "ketju.db")
	if fi, err := os.Stat(dbFile + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("the WAL holds %d bytes after a clean close; a copy of the file alone would not be a backup", fi.Size())
	}
	b, err := os.ReadFile(dbFile)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "ketju-backup.db")
	if err := os.WriteFile(backup, b, 0o600); err != nil {
		t.Fatal(err)
	}

	// Written after the backup: more on an existing thread, and new threads.
	writeThreads(t, data, 2, 2, ids[:1])
	d := openDB(t, data)
	execDB(t, d, `INSERT INTO users (id, handle, display_name, created_at, updated_at) VALUES (4, 'bob', 'Bob', 2, 2)`)
	d.Close()
	want, _ := dumpAt(t, data)
	if !strings.Contains(want, "threads: 7 rows") {
		t.Fatalf("the setup did not write after the backup:\n%s", want)
	}

	// The database is lost: damaged past opening.
	if err := os.WriteFile(filepath.Join(data, "db", "ketju.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(bin, "ketju")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-eu", "-c", script)
	cmd.Env = append(os.Environ(),
		"KETJU_AS_MAIN=1", "KETJU_DATA="+data, "KETJU_BACKUP="+backup,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the README procedure failed: %v\n%s", err, out)
	}
	for _, want := range []string{"reindexed 7 of 7 thread(s)", "no corruption found"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the procedure's output lacks %q:\n%s", want, out)
		}
	}

	got, members := dumpAt(t, data)
	if got != want {
		t.Fatalf("the recovered projection differs from the one lost:\n%s\n---\n%s", want, got)
	}
	if members != backedUp {
		t.Errorf("members = %q, want the backup's %q", members, backedUp)
	}

	// What serve does next: its catch-up finds every thread projected.
	d = openDB(t, data)
	defer d.Close()
	cache, err := threadcache.New(data, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	w, err := projection.New(projection.Options{DB: d, Root: data, Sweep: func(context.Context) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := w.CatchUp(t.Context(), func(_ context.Context, id threadid.ID) (int64, error) {
		var n int64
		err := cache.With(id, func(th *threadcache.Thread) error { n = th.Actor.LogLength(); return nil })
		return n, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Scanned != 7 || rep.Closed != 0 || len(rep.Corrupt)+len(rep.Failed)+len(rep.Unfinished) != 0 {
		t.Errorf("catch-up after recovery = %+v", rep)
	}
}
