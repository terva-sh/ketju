package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/db"
	"github.com/terva-sh/ketju/internal/store/db/dbq"
	"github.com/terva-sh/ketju/internal/store/db/dbtest"
	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// frameEnds is the offset at which each whole frame of thread id's log under
// data ends, in order.
func frameEnds(t *testing.T, data string, id threadid.ID) []int64 {
	t.Helper()
	f, err := os.Open(id.LogPath(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	var ends []int64
	err = threadlog.ScanFramesFrom(f, threadlog.FileHeaderSize, fi.Size(), func(rec threadlog.ScanRecord) bool {
		if rec.Err != nil {
			t.Fatalf("thread %s: frame at %d: %v", id, rec.Offset, rec.Err)
		}
		ends = append(ends, rec.Offset+int64(rec.Size))
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	return ends
}

// projectedLengths is projection_state of the database under data, by
// thread, read with a query every schema since 0004 answers.
func projectedLengths(t *testing.T, data string) map[threadid.ID]int64 {
	t.Helper()
	d, err := db.OpenReadOnly(t.Context(), adminDBPath(data))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out := map[threadid.ID]int64{}
	if err := d.Read(t.Context(), func(_ *dbq.Queries, tx *sql.Tx) error {
		rows, err := tx.QueryContext(t.Context(), `SELECT thread_id, log_length FROM projection_state`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, n int64
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			out[threadid.ID(id)] = n
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// dumpWithout is the projection tables under data as dbtest.DumpProjections
// writes them, without the lines of the threads named.
func dumpWithout(t *testing.T, data string, ids ...threadid.ID) string {
	t.Helper()
	d, err := db.OpenReadOnly(t.Context(), adminDBPath(data))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var b strings.Builder
	for line := range strings.Lines(dbtest.DumpProjections(t, d)) {
		if !slices.ContainsFunc(ids, func(id threadid.ID) bool {
			return strings.Contains(line, fmt.Sprintf(" thread_id=%d ", int64(id))) ||
				strings.Contains(line, fmt.Sprintf(": id=%d ", int64(id)))
		}) && !strings.HasSuffix(strings.TrimSpace(line), " rows") {
			b.WriteString(line)
		}
	}
	return b.String()
}

// catchUpLine is serve's log line for a finished projection catch-up.
var catchUpLine = regexp.MustCompile(`"msg":"projection catch-up finished".*`)

// TestUpgradeKeepsTruncationDetection is TKT-01M41MTXWH's first two
// criteria (Terva review 2074). The fixture's database is at migration 0024,
// before quote backlinks. Two of its logs are cut before the upgrade: one at
// a frame boundary above its header, one to exactly its header. The first
// start after upgrading quarantines both, as it would on any other start,
// because the backfill no longer moves projection_state back. Every intact
// log is replayed from its header over the rows already there, which fills
// post_quotes, and ends on the rows a reindex of the uncut fixture makes. A
// second start replays nothing.
func TestUpgradeKeepsTruncationDetection(t *testing.T) {
	data := restore(t, upgradeFixture)
	pristine := restore(t, upgradeFixture)
	for _, dir := range []string{data, pristine} {
		if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	main, err := threadid.Parse(upgradeThread)
	if err != nil {
		t.Fatal(err)
	}

	// Two threads other than the seeded one whose projection reached the
	// end of a log of at least two frames.
	before := projectedLengths(t, data)
	var cut []threadid.ID
	for _, id := range slices.Sorted(func(yield func(threadid.ID) bool) {
		for id := range before {
			if !yield(id) {
				return
			}
		}
	}) {
		if id == main {
			continue
		}
		fi, err := os.Stat(id.LogPath(data))
		if err != nil {
			t.Fatal(err)
		}
		if ends := frameEnds(t, data, id); len(ends) >= 2 && fi.Size() == before[id] {
			cut = append(cut, id)
		}
		if len(cut) == 2 {
			break
		}
	}
	if len(cut) != 2 {
		t.Fatalf("the fixture has %d threads to cut, want 2", len(cut))
	}
	ends := frameEnds(t, data, cut[0])
	toFrame, toHeader := ends[len(ends)-2], int64(threadlog.FileHeaderSize)
	if toFrame <= threadlog.FileHeaderSize {
		t.Fatalf("thread %s: the cut at %d is not above the header", cut[0], toFrame)
	}
	for id, n := range map[threadid.ID]int64{cut[0]: toFrame, cut[1]: toHeader} {
		if err := os.Truncate(id.LogPath(data), n); err != nil {
			t.Fatal(err)
		}
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("listen = %q\ndata_dir = %q\n[storage]\nfsync = \"never\"\n",
		freePort(t), data)), 0o644); err != nil {
		t.Fatal(err)
	}

	// The first start after upgrading.
	r := start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatalf("serve: %v\n%s", err, r.log)
	}
	log := r.log.String()
	for _, id := range cut {
		if !regexp.MustCompile(`shorter than its projection.*"thread":"` + id.String() + `"`).MatchString(log) {
			t.Errorf("thread %s, cut before the upgrade, was not quarantined:\n%s", id, log)
		}
	}
	first := catchUpLine.FindString(log)
	if !strings.Contains(first, `"corrupt":2,`) || !strings.Contains(first, fmt.Sprintf(`"backfilled":%d,`, len(before)-2)) {
		t.Errorf("the first start's catch-up: %s", first)
	}
	after := projectedLengths(t, data)
	for _, id := range cut {
		if after[id] != before[id] {
			t.Errorf("thread %s: projection_state is %d after the start, want the %d it held", id, after[id], before[id])
		}
	}

	// Every intact thread: the rows a reindex of the uncut fixture makes,
	// post_quotes included, with nothing left to backfill. The seeded
	// thread's second post quotes its first.
	if _, errOut, err := reindexRun(t, reindexOpts{}, "--root", pristine); err != nil {
		t.Fatalf("reindex: %v\n%s", err, errOut)
	}
	got, want := dumpWithout(t, data, cut...), dumpWithout(t, pristine, cut...)
	if got != want {
		t.Errorf("after the first start the intact threads' rows differ from a reindex:\n%s\n---\n%s", got, want)
	}
	if line := fmt.Sprintf("post_quotes: thread_id=%d ordinal=2 quoted_ordinal=1", int64(main)); !strings.Contains(got, line) {
		t.Errorf("the seeded thread's quote was not backfilled; want %q in\n%s", line, got)
	}

	// The second start replays nothing, and the cut threads stay
	// quarantined.
	r = start(t, path, serveOpts{})
	if err := r.shutdown(t); err != nil {
		t.Fatalf("serve: %v\n%s", err, r.log)
	}
	second := catchUpLine.FindString(r.log.String())
	if !strings.Contains(second, `"closed":0,`) || !strings.Contains(second, `"backfilled":0,`) ||
		!strings.Contains(second, `"corrupt":2,`) {
		t.Errorf("the second start's catch-up: %s", second)
	}
}
