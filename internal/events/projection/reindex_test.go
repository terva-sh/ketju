package projection

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// TestReindexRebuildsFromEmptyAndStale: threads projected one by one, then
// reindexed twice, once over rows scribbled with state no record put there
// and once over emptied tables. Both come back to the rows the incremental
// projection made, and progress is reported before the first thread and
// after each.
func TestReindexRebuildsFromEmptyAndStale(t *testing.T) {
	f := newFixture(t)
	rng := rand.New(rand.NewPCG(7, 0xC5))
	var ids []threadid.ID
	for i := range 6 {
		id, end := f.thread(fmt.Sprintf("T%d", i), "meta")
		f.project(id, end)
		var posts uint32
		for range 10 + rng.IntN(20) {
			posts, end = randomRecord(f, rng, id, posts)
			f.project(id, end)
		}
		ids = append(ids, id)
	}
	incremental := f.dump()

	for _, setup := range []struct {
		name string
		run  func()
	}{
		{"stale", func() {
			f.exec(`UPDATE threads SET title = 'stale', post_count = 99, locked = 1 - locked`)
			f.exec(`UPDATE post_meta SET deleted = 1 - deleted, reply_to = 77`)
			f.exec(`INSERT INTO thread_tags (thread_id, tag) VALUES (1, 'orphan')`)
			f.exec(`INSERT INTO projection_state (thread_id, log_length, updated_at) VALUES (1, 999, 0)`)
			f.exec(`UPDATE forum_thread_counts SET threads = threads + 5, live_posts = live_posts + 7`)
			f.exec(`UPDATE threads SET live_post_count = live_post_count + 3`)
		}},
		{"empty", f.clear},
	} {
		t.Run(setup.name, func(t *testing.T) {
			setup.run()
			var seen []ReindexProgress
			rep, err := f.w.Reindex(t.Context(), func(p ReindexProgress) { seen = append(seen, p) })
			if err != nil {
				t.Fatal(err)
			}
			if got := f.dump(); got != incremental {
				t.Fatalf("reindexed rows differ from the incremental ones:\n%s\n---\n%s", incremental, got)
			}
			if rep.Threads != len(ids) || rep.Projected != len(ids) || len(rep.Failed)+len(rep.Torn)+len(rep.Unfinished) != 0 {
				t.Fatalf("report = %+v", rep)
			}
			if len(seen) != len(ids)+1 || seen[0].Done != 0 || seen[len(seen)-1].Done != len(ids) ||
				seen[0].TotalBytes == 0 || seen[len(seen)-1].Bytes != seen[0].TotalBytes {
				t.Fatalf("progress = %+v", seen)
			}
		})
	}
}

// TestReindexAroundBrokenLogs: one log of each kind reindex has to step
// around, beside a sound one. A torn tail is projected to its last whole
// frame; a header-only log has nothing to project; a checksum failure before
// the last frame and a file cut inside its header are failures with no rows; and none of them
// stops the sound thread. The logs are left byte for byte as they were.
func TestReindexAroundBrokenLogs(t *testing.T) {
	f := newFixture(t)
	sound, soundEnd := f.thread("Sound")
	soundEnd = max(soundEnd, f.post(sound, 1, 0))

	torn, tornEnd := f.thread("Torn")
	tornEnd = max(tornEnd, f.post(torn, 1, 0))

	// Damage before the last frame. In the last frame a checksum failure is
	// what a final write that reached the disk in pieces looks like, and it
	// is a torn tail to the engine and to reindex alike.
	damaged, _ := f.thread("Damaged")
	damagedAt := f.post(damaged, 1, 0)
	f.post(damaged, 1, 0)

	empty := threadid.ID(uint64(1_700_000_000_000)<<16 | 900)
	th, err := f.cache.Create(empty, 7, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	f.cache.Release(th)

	short := threadid.ID(uint64(1_700_000_000_000)<<16 | 901)
	th, err = f.cache.Create(short, 7, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	f.cache.Release(th)
	if err := f.cache.Close(); err != nil {
		t.Fatal(err)
	}

	// The incremental rows for what survives: the sound thread whole, and
	// the torn one up to its last whole frame.
	f.project(sound, soundEnd)
	f.project(torn, tornEnd)
	want := f.dump()

	tear(t, f.root, torn)
	b, err := os.ReadFile(damaged.LogPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	b[damagedAt-6] ^= 0xff
	if err := os.WriteFile(damaged.LogPath(f.root), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(short.LogPath(f.root), 20); err != nil {
		t.Fatal(err)
	}
	before := map[threadid.ID][]byte{}
	for _, id := range []threadid.ID{sound, torn, damaged, empty, short} {
		before[id], _ = os.ReadFile(id.LogPath(f.root))
	}

	rep, err := f.w.Reindex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.dump(); got != want {
		t.Fatalf("rows differ:\n%s\n---\n%s", want, got)
	}
	if rep.Threads != 5 || rep.Projected != 2 || !slices.Equal(rep.Torn, []threadid.ID{torn}) ||
		!slices.Equal(rep.Unfinished, []threadid.ID{empty}) || len(rep.Failed) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	failed := map[threadid.ID]error{}
	for _, fl := range rep.Failed {
		failed[fl.Thread] = fl.Err
	}
	if !errors.Is(failed[damaged], threadlog.ErrChecksum) {
		t.Errorf("damaged: %v, want ErrChecksum", failed[damaged])
	}
	if !errors.Is(failed[short], threadlog.ErrLogTooShort) {
		t.Errorf("short: %v, want ErrLogTooShort", failed[short])
	}
	for id, b := range before {
		if after, _ := os.ReadFile(id.LogPath(f.root)); string(after) != string(b) {
			t.Errorf("reindex changed the log of %s", id)
		}
	}
	if !strings.Contains(want, fmt.Sprintf("log_length=%d", tornEnd)) {
		t.Fatalf("the torn thread's projection does not stop at %d:\n%s", tornEnd, want)
	}
}

// TestReindexWalkFailureChangesNothing: a walk that cannot finish returns
// before the tables are emptied.
func TestReindexWalkFailureChangesNothing(t *testing.T) {
	f := newFixture(t)
	id, end := f.thread("Kept")
	f.project(id, end)
	want := f.dump()

	shard := id.Dir(f.root)
	if err := os.Chmod(shard, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(shard, 0o755) })
	if _, err := os.ReadDir(shard); err == nil {
		t.Skip("running as a user that can read an unreadable directory")
	}
	if rep, err := f.w.Reindex(t.Context(), nil); err == nil || rep.Cleared {
		t.Fatalf("reindex over a directory it could not read: cleared %v, err %v", rep.Cleared, err)
	}
	if got := f.dump(); got != want {
		t.Fatalf("a failed walk changed the projection:\n%s\n---\n%s", want, got)
	}
}

// TestReindexHeaderOnlyAndTornCreation (review 1137, findings 2 and 3): a
// log of exactly its file header is unfinished only when that header is
// sound and names the thread; and a log whose creation frame was torn is a
// torn write, not an unfinished one, with nothing projected.
func TestReindexHeaderOnlyAndTornCreation(t *testing.T) {
	f := newFixture(t)
	header := func(n uint64, names threadid.ID) threadid.ID {
		id := threadid.ID(uint64(1_700_000_000_000)<<16 | n)
		if names == 0 {
			names = id
		}
		if err := os.MkdirAll(id.Dir(f.root), 0o755); err != nil {
			t.Fatal(err)
		}
		b := threadlog.NewFileHeader(uint64(names), 1, 7).AppendTo(nil)
		if err := os.WriteFile(id.LogPath(f.root), b, 0o644); err != nil {
			t.Fatal(err)
		}
		return id
	}
	unfinished := header(1, 0)
	damaged := header(2, 0)
	b, err := os.ReadFile(damaged.LogPath(f.root))
	if err != nil {
		t.Fatal(err)
	}
	b[0] ^= 0xff // the magic
	if err := os.WriteFile(damaged.LogPath(f.root), b, 0o644); err != nil {
		t.Fatal(err)
	}
	other := header(3, unfinished)
	tornCreation := header(4, 0)
	tear(t, f.root, tornCreation)

	rep, err := f.w.Reindex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rep.Unfinished, []threadid.ID{unfinished}) {
		t.Errorf("unfinished = %v, want only %s", rep.Unfinished, unfinished)
	}
	if !slices.Equal(rep.Torn, []threadid.ID{tornCreation}) {
		t.Errorf("torn = %v, want only %s", rep.Torn, tornCreation)
	}
	if rep.Projected != 0 || rep.Bytes != 0 {
		t.Errorf("projected %d threads, %d bytes, from logs with no whole record", rep.Projected, rep.Bytes)
	}
	failed := map[threadid.ID]error{}
	for _, fl := range rep.Failed {
		failed[fl.Thread] = fl.Err
	}
	if len(failed) != 2 || failed[damaged] == nil || failed[other] == nil {
		t.Fatalf("failed = %v, want the damaged header and the one naming another thread", rep.Failed)
	}
	if !strings.Contains(failed[other].Error(), "names thread "+unfinished.String()) {
		t.Errorf("the misplaced log's failure does not say whose it is: %v", failed[other])
	}
	if d := f.dump(); !strings.Contains(d, "projection_state: 0 rows") {
		t.Errorf("rows for logs with nothing to project:\n%s", d)
	}
}

// TestReindexSeparatesLogFaultsFromTheDatabase (review 1137, finding 1): a
// log whose records contradict each other is a per-thread failure the run
// goes past, like damage; a database write that fails is not the log's
// fault, and stops the run with an error rather than reporting the thread
// as broken.
func TestReindexSeparatesLogFaultsFromTheDatabase(t *testing.T) {
	f := newFixture(t)
	created := rec{threadlog.TypeThreadCreated, &threadlog.ThreadCreatedBody{Title: "T", Slug: "t", ForumID: 7}}
	// An Edit of a post the log never wrote: sound bytes, and no projection.
	contradicts, _ := rawLog(t, f, created, rec{threadlog.TypeEdit, &threadlog.EditBody{Source: "x"}})
	sound, _ := rawLog(t, f, created, rec{threadlog.TypePost, &threadlog.PostBody{Source: "x"}})

	rep, err := f.w.Reindex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Failed) != 1 || rep.Failed[0].Thread != contradicts || !IsLogFault(rep.Failed[0].Err) ||
		rep.Projected != 1 {
		t.Fatalf("report = %+v", rep)
	}

	f.exec(`CREATE TRIGGER disk_full BEFORE INSERT ON post_meta BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	rep, err = f.w.Reindex(t.Context(), nil)
	if err == nil || IsLogFault(err) || !strings.Contains(err.Error(), "disk full") || !rep.Cleared {
		t.Fatalf("got %v, want the database's error, not a log fault", err)
	}
	for _, fl := range rep.Failed {
		if fl.Thread == sound {
			t.Errorf("the sound thread was reported as a broken log: %v", fl.Err)
		}
	}
}
