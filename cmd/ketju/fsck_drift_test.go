package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/terva-sh/ketju/internal/events"
	"github.com/terva-sh/ketju/internal/events/projection"
	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/threadcache"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// The drift and quarantine half of fsck (TKT-01M3A054TR).

// projectRows sets projection_state rows in the data directory's database,
// creating and migrating it if it is not there. The database is closed, so
// the rows are checkpointed into ketju.db.
func projectRows(t *testing.T, root string, rows map[threadid.ID]int64) {
	t.Helper()
	d := openDB(t, root)
	defer d.Close()
	for id, n := range rows {
		execDB(t, d, `INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (?, ?, 1)
			ON CONFLICT (thread_id) DO UPDATE SET log_length = excluded.log_length`, int64(id), n)
	}
}

func sizeOf(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// lastRecordAt is where a log's last record starts: a frame boundary short
// of the end.
func lastRecordAt(t *testing.T, path string) int64 {
	t.Helper()
	lines, err := dumpLines(t, path)
	if err != nil {
		t.Fatal(err)
	}
	return lines[len(lines)-1].Offset
}

// reportFor is the report naming id, or fails the test.
func reportFor(t *testing.T, sum fsckSummary, id threadid.ID) threadReport {
	t.Helper()
	for _, r := range sum.Reports {
		if r.ID == id.String() {
			return r
		}
	}
	t.Fatalf("no report names %s", id)
	return threadReport{}
}

// flagged is every thread fsck marks with a projection verdict, sorted.
func flagged(sum fsckSummary, verdict string) []string {
	var out []string
	for _, r := range sum.Reports {
		if r.Projection == verdict {
			out = append(out, r.ID)
		}
	}
	slices.Sort(out)
	return out
}

// TestFsckReportsDriftAndExitsThree is AC1: each thread whose log is longer
// than its projection_state row is named with how far, drift_checked is set,
// and the exit code is 3. A thread with frames and no row is drift of its
// whole log, and a header-only thread nobody projected is not drift at all,
// as catch-up treats each.
func TestFsckReportsDriftAndExitsThree(t *testing.T) {
	root := t.TempDir()
	current := threadid.ID(0x3f7a9c21e0000001)
	behind := threadid.ID(0x3f7a9c21e0000002)
	never := threadid.ID(0x3f7a9c21e0000003)
	unfinished := threadid.ID(0x3f7a9c21e0000004)
	threadAt(t, root, current, 3)
	behindLog := threadAt(t, root, behind, 5)
	neverLog := threadAt(t, root, never, 2)
	threadAt(t, root, unfinished, 0)

	at := lastRecordAt(t, behindLog)
	projectRows(t, root, map[threadid.ID]int64{
		current: sizeOf(t, current.LogPath(root)),
		behind:  at,
	})

	sum, _, err := fsckJSON(t, "--root", root)
	if got := exitCodeFor(err); got != exitDrift {
		t.Fatalf("exit %d (%v), want %d", got, err, exitDrift)
	}
	if !sum.DriftChecked || sum.DriftNote != "" {
		t.Errorf("drift_checked = %t, note %q; the database was there to read", sum.DriftChecked, sum.DriftNote)
	}
	wantBehind := sizeOf(t, behindLog) - at
	wantNever := sizeOf(t, neverLog) - threadlog.FileHeaderSize
	if sum.DriftThreads != 2 || sum.DriftBytes != wantBehind+wantNever {
		t.Errorf("drift_threads %d, drift_bytes %d; want 2, %d", sum.DriftThreads, sum.DriftBytes, wantBehind+wantNever)
	}
	if sum.Quarantined != 0 || sum.Damaged != 0 {
		t.Errorf("quarantined %d, damaged %d; drift is neither", sum.Quarantined, sum.Damaged)
	}
	for _, tc := range []struct {
		id    threadid.ID
		class string
		drift int64
	}{
		{current, "current", 0},
		{behind, "drift", wantBehind},
		{never, "unprojected", wantNever},
		{unfinished, "unfinished", 0},
	} {
		r := reportFor(t, sum, tc.id)
		if r.Projection != tc.class || r.DriftBytes != tc.drift {
			t.Errorf("%s: projection %q, drift_bytes %d; want %q, %d", tc.id, r.Projection, r.DriftBytes, tc.class, tc.drift)
		}
	}

	text, _ := fsckText(t, "--root", root)
	for _, want := range []string{
		"DRIFT    " + behind.String(), "DRIFT    " + never.String(),
		"no corruption found", "2 thread(s) with projection drift",
		"may be lag that catch-up is closing",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text output lacks %q:\n%s", want, text)
		}
	}
	for _, not := range []string{"DRIFT    " + current.String(), "DRIFT    " + unfinished.String()} {
		if strings.Contains(text, not) {
			t.Errorf("text output has %q:\n%s", not, text)
		}
	}
}

// TestFsckReportsAQuarantinedThread is AC2: a log shorter than its
// projection and a projection with no log are each named and counted, fsck
// exits 1, and serve's catch-up over the same directory quarantines exactly
// the same threads.
func TestFsckReportsAQuarantinedThread(t *testing.T) {
	root := t.TempDir()
	fine := threadid.ID(0x3f7a9c21e0000001)
	cut := threadid.ID(0x3f7a9c21e0000002)
	gone := threadid.ID(0x3f7a9c21e0000003)
	threadAt(t, root, fine, 3)
	cutLog := threadAt(t, root, cut, 4)
	goneLog := threadAt(t, root, gone, 2)

	projectRows(t, root, map[threadid.ID]int64{
		fine: sizeOf(t, fine.LogPath(root)),
		cut:  sizeOf(t, cutLog),
		gone: sizeOf(t, goneLog),
	})
	// Cut at a frame boundary, so the log is sound and only shorter.
	if err := os.Truncate(cutLog, lastRecordAt(t, cutLog)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{goneLog, gone.IndexPath(root)} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}

	sum, _, err := fsckJSON(t, "--root", root)
	if got := exitCodeFor(err); got != exitCorrupt {
		t.Fatalf("exit %d (%v), want %d", got, err, exitCorrupt)
	}
	if sum.Quarantined != 2 || sum.Damaged != 0 {
		t.Errorf("quarantined_threads %d, damaged %d; want 2, 0", sum.Quarantined, sum.Damaged)
	}
	fromFsck := flagged(sum, "corrupt")
	if want := []string{cut.String(), gone.String()}; !slices.Equal(fromFsck, want) {
		t.Errorf("fsck marked %v corrupt, want %v", fromFsck, want)
	}
	if r := reportFor(t, sum, gone); r.Path != "" || r.Projected == nil {
		t.Errorf("the thread with no log is reported with path %q, projected %v", r.Path, r.Projected)
	}
	if sum.Threads != 2 {
		t.Errorf("threads = %d; a projection with no log is not a log walked", sum.Threads)
	}

	text, _ := fsckText(t, "--root", root)
	for _, want := range []string{
		"QUARANT  " + cut.String(), "QUARANT  " + gone.String(), "2 thread(s) QUARANTINED",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "no corruption found") {
		t.Errorf("text output says no corruption beside a quarantined thread:\n%s", text)
	}

	// Serve's verdict on the same directory. Neither thread has a gap for
	// catch-up to open, so the Recover is never called.
	d := openDB(t, root)
	defer d.Close()
	w, err := projection.New(projection.Options{DB: d, Root: root, Sweep: func(context.Context) error { return nil },
		Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := w.CatchUp(t.Context(), func(_ context.Context, id threadid.ID) (int64, error) {
		t.Errorf("catch-up opened %s; no thread here has a gap", id)
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var fromServe []string
	for _, id := range rep.Corrupt {
		fromServe = append(fromServe, id.String())
		if !w.Quarantined(id) {
			t.Errorf("catch-up reported %s corrupt but did not quarantine it", id)
		}
	}
	if !slices.Equal(fromServe, fromFsck) {
		t.Errorf("catch-up quarantined %v, fsck reported %v", fromServe, fromFsck)
	}
}

// TestFsckDoesNotCallCorruptThreadsCurrent: with no drift, the text's last
// line claims every projection current only when no thread is quarantined
// and no log was left uncompared for damage (Terva review 1536).
func TestFsckDoesNotCallCorruptThreadsCurrent(t *testing.T) {
	for _, corruption := range []string{"quarantined thread", "damaged log", "unfinished thread", "none"} {
		t.Run(corruption, func(t *testing.T) {
			root := t.TempDir()
			fine := threadid.ID(0x3f7a9c21e0000001)
			bad := threadid.ID(0x3f7a9c21e0000002)
			threadAt(t, root, fine, 3)
			badLog := threadAt(t, root, bad, 3)
			rows := map[threadid.ID]int64{fine: sizeOf(t, fine.LogPath(root)), bad: sizeOf(t, badLog)}
			switch corruption {
			case "quarantined thread":
				rows[threadid.ID(0x3f7a9c21e0000003)] = 500 // a row with no log
			case "damaged log":
				raw, err := os.ReadFile(badLog)
				if err != nil {
					t.Fatal(err)
				}
				raw[threadlog.FileHeaderSize+threadlog.FrameFixedSize+2] ^= 0xff
				if err := os.WriteFile(badLog, raw, 0o644); err != nil {
					t.Fatal(err)
				}
			case "unfinished thread":
				threadAt(t, root, threadid.ID(0x3f7a9c21e0000003), 0) // header only, no row
			}
			projectRows(t, root, rows)

			text, err := fsckText(t, "--root", root)
			const all = "projection current for every thread"
			switch corruption {
			case "none":
				if err != nil || !strings.Contains(text, all) {
					t.Errorf("exit %d; a clean directory's text lacks %q:\n%s", exitCodeFor(err), all, text)
				}
				return
			case "unfinished thread":
				// Not an error, but not current either (Terva review 1537).
				if err != nil {
					t.Errorf("exit %d (%v); an unfinished thread is not an error", exitCodeFor(err), err)
				}
				sum, _, _ := fsckJSON(t, "--root", root)
				if sum.Unfinished != 1 {
					t.Errorf("unfinished_threads = %d, want 1", sum.Unfinished)
				}
				for _, want := range []string{"1 thread(s) unfinished", "no projection drift found"} {
					if !strings.Contains(text, want) {
						t.Errorf("text lacks %q:\n%s", want, text)
					}
				}
				if strings.Contains(text, all) {
					t.Errorf("the text calls every projection current beside an unfinished thread:\n%s", text)
				}
				return
			}
			if got := exitCodeFor(err); got != exitCorrupt {
				t.Errorf("exit %d, want %d", got, exitCorrupt)
			}
			if strings.Contains(text, all) {
				t.Errorf("the text calls every projection current beside a %s:\n%s", corruption, text)
			}
			if !strings.Contains(text, "no projection drift found") {
				t.Errorf("the text does not say no drift was found:\n%s", text)
			}
		})
	}
}

// TestFsckCorruptionOutranksDrift is AC3's first half: a directory with
// both corruption and drift exits 1, and still reports the drift.
func TestFsckCorruptionOutranksDrift(t *testing.T) {
	for _, corruption := range []string{"damaged log", "quarantined thread"} {
		t.Run(corruption, func(t *testing.T) {
			root := t.TempDir()
			bad := threadid.ID(0x3f7a9c21e0000001)
			behind := threadid.ID(0x3f7a9c21e0000002)
			badLog := threadAt(t, root, bad, 4)
			behindLog := threadAt(t, root, behind, 4)
			rows := map[threadid.ID]int64{
				bad:    sizeOf(t, badLog),
				behind: lastRecordAt(t, behindLog),
			}
			if corruption == "damaged log" {
				raw, err := os.ReadFile(badLog)
				if err != nil {
					t.Fatal(err)
				}
				raw[threadlog.FileHeaderSize+threadlog.FrameFixedSize+2] ^= 0xff
				if err := os.WriteFile(badLog, raw, 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				rows[bad] += 100
			}
			projectRows(t, root, rows)

			sum, _, err := fsckJSON(t, "--root", root)
			if got := exitCodeFor(err); got != exitCorrupt {
				t.Fatalf("exit %d (%v), want %d: corruption outranks drift", got, err, exitCorrupt)
			}
			if sum.DriftThreads != 1 || reportFor(t, sum, behind).Projection != "drift" {
				t.Errorf("drift_threads %d; the drift beside the corruption went unreported", sum.DriftThreads)
			}
			if sum.Damaged+sum.Quarantined != 1 {
				t.Errorf("damaged %d, quarantined %d; want one of them", sum.Damaged, sum.Quarantined)
			}
		})
	}
}

// TestFsckTornTailIsNotDrift is AC3's second half: bytes past the last whole
// frame are an interrupted write, which serve's open cuts off, so a
// projection that reaches the last whole frame is current. Catch-up agrees.
func TestFsckTornTailIsNotDrift(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	p := threadAt(t, root, id, 5)
	whole := sizeOf(t, p)
	projectRows(t, root, map[threadid.ID]int64{id: whole})

	// The first 9 bytes of the last frame again: a length that runs past
	// the end of the file.
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	last := lastRecordAt(t, p)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(raw[last : last+9]); err != nil {
		t.Fatal(err)
	}
	f.Close()

	sum, _, err := fsckJSON(t, "--root", root)
	if err != nil {
		t.Fatalf("exit %d (%v) over a torn tail on a current projection", exitCodeFor(err), err)
	}
	r := reportFor(t, sum, id)
	if r.Truncated != 9 || r.Projection != "current" || r.DriftBytes != 0 || sum.DriftThreads != 0 {
		t.Errorf("truncated %d, projection %q, drift %d/%d; want 9, current, 0/0",
			r.Truncated, r.Projection, r.DriftBytes, sum.DriftThreads)
	}

	// Catch-up opens the thread, which cuts the tail, and finds nothing to do.
	d := openDB(t, root)
	defer d.Close()
	cache, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	w, err := projection.New(projection.Options{DB: d, Root: root, Sweep: func(context.Context) error { return nil },
		Logger: slog.New(slog.DiscardHandler)})
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
	if rep.Closed != 0 || len(rep.Corrupt) != 0 || len(rep.Failed) != 0 {
		t.Errorf("catch-up over the same torn tail: %+v; want nothing closed, corrupt or failed", rep)
	}
}

// dirState is ketju.db and its WAL, which a read-only run must leave as it
// found them. The -shm file is shared memory, not data, and readers write it.
func dirState(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"ketju.db", "ketju.db-wal"} {
		p := filepath.Join(root, "db", name)
		fi, err := os.Stat(p)
		if err != nil {
			if name == "ketju.db" {
				t.Fatal(err)
			}
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = fmt.Sprintf("size %d, mtime %d, sha256 %x", fi.Size(), fi.ModTime().UnixNano(), sha256.Sum256(b))
	}
	return out
}

// TestFsckOpensTheDatabaseReadOnly is AC4's first half: ketju.db's size,
// mtime and SHA-256 are the same after fsck runs as before, in every mode it
// has, whether the rows are checkpointed into the file or sit in the WAL of a
// handle still open, as a live serve's are.
func TestFsckOpensTheDatabaseReadOnly(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := map[bool]string{false: "closed", true: "held open"}[live]
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			ids := []threadid.ID{0x3f7a9c21e0000001, 0x3f7a9c21e0000002, 0x3f7a9c21e0000003}
			rows := map[threadid.ID]int64{}
			for i, id := range ids {
				threadAt(t, root, id, 2+i)
				rows[id] = sizeOf(t, id.LogPath(root))
			}
			rows[ids[1]] = threadlog.FileHeaderSize // drift, so the report has something to say
			projectRows(t, root, rows)
			if live {
				d := openDB(t, root)
				t.Cleanup(func() { d.Close() })
				execDB(t, d, `UPDATE projection_state SET updated_at = 2`)
			}

			before := dirState(t, root)
			for _, args := range [][]string{{"--json"}, {}, {"--quiet"}} {
				fsckText(t, append(args, "--root", root)...)
			}
			if after := dirState(t, root); !maps.Equal(after, before) {
				t.Errorf("fsck changed the database:\nbefore %q\nafter  %q", before, after)
			}
		})
	}
}

// TestFsckWithoutADatabase is AC4's second half, and replaces the test that
// said the drift check never ran: with no database fsck still checks every
// log, says drift was not checked and why, and creates no db/ directory.
func TestFsckWithoutADatabase(t *testing.T) {
	root := t.TempDir()
	threadAt(t, root, threadid.ID(0x3f7a9c21e0000001), 2)

	sum, _, err := fsckJSON(t, "--root", root)
	if err != nil {
		t.Fatal(err)
	}
	if sum.DriftChecked {
		t.Error("fsck claims to have checked drift; there is no database to check against")
	}
	if !strings.Contains(sum.DriftNote, "no database at") {
		t.Errorf("drift_note = %q", sum.DriftNote)
	}
	if sum.Threads != 1 || sum.Reports[0].Projection != "" {
		t.Errorf("threads %d, projection %q; the logs are checked and none classified", sum.Threads, sum.Reports[0].Projection)
	}

	text, _ := fsckText(t, "--root", root)
	if !strings.Contains(text, "projection drift not checked: no database at "+filepath.Join(root, "db", "ketju.db")) {
		t.Errorf("text output does not say the drift check was skipped, and why:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(root, "db")); !os.IsNotExist(err) {
		t.Errorf("fsck created %s (stat err %v)", filepath.Join(root, "db"), err)
	}
}

// TestFsckWithAnUnreadableDatabase: a database fsck cannot open, or one
// without projection_state, leaves drift unchecked with the reason, and is
// not an error on its own: the logs are what fsck checks first.
func TestFsckWithAnUnreadableDatabase(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		make       func(t *testing.T, path string)
	}{
		{"not a database", "read-only", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(strings.Repeat("not a database ", 20)), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"no projection_state", "projection_state", func(t *testing.T, path string) {
			d, err := db.Open(t.Context(), db.Options{Path: path})
			if err != nil {
				t.Fatal(err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			threadAt(t, root, threadid.ID(0x3f7a9c21e0000001), 2)
			if err := os.MkdirAll(filepath.Join(root, "db"), 0o750); err != nil {
				t.Fatal(err)
			}
			tc.make(t, adminDBPath(root))
			before := dirState(t, root)

			sum, _, err := fsckJSON(t, "--root", root)
			if err != nil {
				t.Fatalf("exit %d (%v); an unreadable database is not corruption in the logs", exitCodeFor(err), err)
			}
			if sum.DriftChecked || !strings.Contains(sum.DriftNote, tc.want) {
				t.Errorf("drift_checked %t, note %q; want unchecked, naming %q", sum.DriftChecked, sum.DriftNote, tc.want)
			}
			if !maps.Equal(dirState(t, root), before) {
				t.Error("fsck changed a database it could not read")
			}
		})
	}
}

// TestFsckBesideALiveProjection is AC5. A goroutine appends to a thread and
// projects each append through the writer's Deliver, as serve's bus does,
// while fsck runs over and over with the database held open. fsck may
// find the projection behind the log, and exit 3, but never ahead of it: it
// never reports the thread quarantined. A write the projection has not been
// told of is drift, and the text says that beside a live serve it may be lag.
func TestFsckBesideALiveProjection(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(filepath.Join(root, threadid.ThreadsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	d := openDB(t, root) // held open, as serve holds it
	defer d.Close()
	cache, err := threadcache.New(root, threadcache.Options{Sync: threadlog.SyncNever})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	w, err := projection.New(projection.Options{DB: d, Root: root, Sweep: func(context.Context) error { return nil },
		Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}

	id := threadid.ID(0x3f7a9c21e0000001)
	clock := int64(1_700_000_000_000)
	th, err := cache.Create(id, 1, clock)
	if err != nil {
		t.Fatal(err)
	}
	cache.Release(th)
	// Only the appending goroutine calls it once the race starts.
	appendOne := func(a threadlog.Append) (int64, error) {
		clock += 1000
		a.TimeMS = clock
		var res threadlog.Result
		err := cache.With(id, func(th *threadcache.Thread) error {
			var err error
			res, err = th.Actor.Append(a)
			return err
		})
		return res.LogLength, err
	}
	project := func(n int64) error {
		return w.Deliver(t.Context(), events.Batch{Events: []events.Event{{Thread: id, LogLength: n}}})
	}
	post := threadlog.Append{Type: threadlog.TypePost, Author: 1, Body: &threadlog.PostBody{Source: "said while fsck looked"}}

	n, err := appendOne(threadlog.Append{Type: threadlog.TypeThreadCreated, Author: 1,
		Body: &threadlog.ThreadCreatedBody{Title: "Live", Slug: "live", ForumID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := project(n); err != nil {
		t.Fatal(err)
	}

	// A write the projection has not been told of yet: the log is ahead.
	n, err = appendOne(post)
	if err != nil {
		t.Fatal(err)
	}
	sum, _, err := fsckJSON(t, "--root", root)
	if got := exitCodeFor(err); got != exitDrift || reportFor(t, sum, id).Projection != "drift" {
		t.Errorf("exit %d, projection %q with an unprojected write; want %d, drift", got, reportFor(t, sum, id).Projection, exitDrift)
	}
	text, _ := fsckText(t, "--root", root)
	if !strings.Contains(text, "beside a running ketju serve may be lag") {
		t.Errorf("the drift report does not say it may be lag:\n%s", text)
	}
	if err := project(n); err != nil {
		t.Fatal(err)
	}

	// Appends and projection racing fsck. Each append is projected at once,
	// so the projection is rarely more than one record behind: a run that
	// sized the log before reading the projection would often find the
	// projection past the size it took, and call the thread quarantined.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	// Stops the writer before the deferred closes run, on a Fatalf too.
	halt := sync.OnceFunc(func() { close(stop); wg.Wait() })
	defer halt()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, err := appendOne(post)
			if err != nil {
				t.Errorf("appending: %v", err)
				return
			}
			if err := project(n); err != nil {
				t.Errorf("projecting to %d: %v", n, err)
				return
			}
		}
	}()

	const runs = 40
	sawDrift := 0
	for range runs {
		sum, _, err := fsckJSON(t, "--root", root)
		switch code := exitCodeFor(err); code {
		case exitDrift:
			sawDrift++
		case exitOK:
		default:
			t.Errorf("exit %d (%v) beside a live projection", code, err)
		}
		if sum.Quarantined != 0 || sum.Damaged != 0 || reportFor(t, sum, id).Projection == "corrupt" {
			t.Fatalf("fsck reported the live thread quarantined or damaged: %+v", sum)
		}
	}
	halt()
	t.Logf("fsck saw drift in %d of %d runs beside the live projection", sawDrift, runs)

	if _, _, err := fsckJSON(t, "--root", root); err != nil {
		t.Errorf("exit %d (%v) once the projection caught up", exitCodeFor(err), err)
	}
}

// TestFsckIgnoresARedactionMarkerAndJournal: hard redaction
// (TKT-01M29CXJZ9) appends a Redacted record, type 0x80, from the critical
// range, pads every blanked frame so the log never gets shorter, keeps its
// journal at the data root, and swaps each log through <log>.redact. None of
// that is corruption. The appended records are drift until serve projects
// them, and current once it has.
func TestFsckIgnoresARedactionMarkerAndJournal(t *testing.T) {
	root := t.TempDir()
	id := threadid.ID(0x3f7a9c21e0000001)
	p := threadAt(t, root, id, 3)
	before := sizeOf(t, p)

	body, err := threadlog.EncodeBody(threadlog.EncodingCBOR, map[string]any{
		"ordinal": 2, "revisions": []int{1}, "reason": "a takedown", "operator": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := threadlog.AppendFrame(nil, threadlog.FrameHeader{
		Type: threadlog.RecordType(0x80), Encoding: threadlog.EncodingCBOR,
		Ordinal: 2, TimestampMS: 1_700_000_100_000, Actor: 1,
	}, body)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(frame); err != nil {
		t.Fatal(err)
	}
	f.Close()
	for path, content := range map[string]string{
		filepath.Join(root, "redaction.journal"): `{"targets":[{"thread":"` + id.String() + `","ordinal":2}]}`,
		p + ".redact":                            "a swap the redaction had not finished",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		projected int64
		want      string
		code      int
	}{
		{before, "drift", exitDrift},
		{sizeOf(t, p), "current", exitOK},
	} {
		projectRows(t, root, map[threadid.ID]int64{id: tc.projected})
		sum, _, err := fsckJSON(t, "--root", root)
		if got := exitCodeFor(err); got != tc.code {
			t.Errorf("projected to %d: exit %d (%v), want %d", tc.projected, got, err, tc.code)
		}
		if sum.Threads != 1 || sum.Damaged != 0 || sum.Quarantined != 0 {
			t.Errorf("projected to %d: threads %d, damaged %d, quarantined %d; want 1, 0, 0",
				tc.projected, sum.Threads, sum.Damaged, sum.Quarantined)
		}
		if r := reportFor(t, sum, id); r.Projection != tc.want || r.Records != 4 {
			t.Errorf("projected to %d: projection %q, %d records; want %q, 4", tc.projected, r.Projection, r.Records, tc.want)
		}
	}
}
